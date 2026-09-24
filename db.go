package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Participant represents a person attending the meetup event.
// It contains their profile data, answers, persona, and matching information.
type Participant struct {
	ID             string
	GitHubHandle   string
	HasGitHub      bool // recorded at registration; Non-GitHub Users get a generated handle
	Name           string
	PersonaName    string
	PersonaColor   string
	PersonaSymbol  string
	PersonaTagline string
	Profile        *GitHubProfile
	Questions      []Question
	Answers        map[string]string
	Interests      map[string]interface{}
	PipelineStep   Step
	MatchedWith    string
	CompatScore    int
	CompatReason   string
	RedFlags       string
	GreenFlags     string
	Icebreakers    string
	CreatedAt      time.Time
}

// DisplayHandle is how the Participant is identified on screen: "@handle" for
// GitHub users, their name for Non-GitHub Users (whose handle is generated).
func (p *Participant) DisplayHandle() string {
	if p.HasGitHub {
		return "@" + p.GitHubHandle
	}
	return p.Name
}

// IsMatched reports the Participant's Relationship State: whether they have a partner.
func (p *Participant) IsMatched() bool {
	return p.MatchedWith != ""
}

// DB wraps the SQLite database connection and provides participant management operations.
type DB struct {
	db      *sql.DB
	changes *changeFeed
}

// NewDB creates and initializes a new database connection.
// It opens a SQLite connection, pings to verify it works, and creates the necessary tables.
func NewDB(path string) (*DB, error) {
	// sql.Open only fails for an unregistered driver; Ping reports real problems.
	sqlDB, _ := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err := sqlDB.Ping(); err != nil {
		return nil, err
	}

	_, err := sqlDB.Exec(`
		CREATE TABLE IF NOT EXISTS participants (
			id               TEXT PRIMARY KEY,
			github_handle    TEXT UNIQUE,
			has_github       INTEGER NOT NULL DEFAULT 1,
			name             TEXT NOT NULL DEFAULT '',
			persona_name     TEXT NOT NULL DEFAULT '',
			persona_color    TEXT NOT NULL DEFAULT 'bg-gray-400',
			persona_symbol   TEXT NOT NULL DEFAULT '🎭',
			persona_tagline  TEXT NOT NULL DEFAULT '',
			profile_json     TEXT NOT NULL DEFAULT '{}',
			questions       TEXT NOT NULL DEFAULT '[]',
			answers_json     TEXT NOT NULL DEFAULT '{}',
			interests       TEXT NOT NULL DEFAULT '{}',
			pipeline_step    TEXT NOT NULL DEFAULT 'fetching_github',
			matched_with     TEXT REFERENCES participants(id),
			compat_score     INTEGER NOT NULL DEFAULT 0,
			compat_reason    TEXT NOT NULL DEFAULT '',
			red_flags        TEXT NOT NULL DEFAULT '[]',
			green_flags      TEXT NOT NULL DEFAULT '[]',
			icebreakers      TEXT NOT NULL DEFAULT '[]',
			created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS event_state (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS activity_log (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			message    TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS llm_cache (
			pair_key   TEXT PRIMARY KEY,
			score      INTEGER NOT NULL,
			reason     TEXT NOT NULL,
			red_flags  TEXT NOT NULL,
			green_flags TEXT NOT NULL,
			icebreakers TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

	`)
	if err != nil {
		return nil, err
	}

	// Migrations for existing databases — ignore errors (columns may already exist)
	for _, m := range []string{
		`ALTER TABLE participants ADD COLUMN name TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE participants ADD COLUMN persona_symbol TEXT NOT NULL DEFAULT '🎭'`,
		`ALTER TABLE participants ADD COLUMN persona_tagline TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE participants MODIFY github_handle TEXT UNIQUE`,
		`ALTER TABLE participants ADD COLUMN interests TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE participants ADD COLUMN questions TEXT NOT NULL DEFAULT '[]'`,
		`UPDATE participants SET questions = custom_questions WHERE custom_questions IS NOT NULL`,
		`ALTER TABLE participants DROP COLUMN extra_answers`,                              // ExtraAnswers live in profile_json
		`UPDATE participants SET pipeline_step = 'ready' WHERE pipeline_step = 'matched'`, // Relationship State is not a Pipeline Step
	} {
		sqlDB.Exec(m)
	}
	// One-time backfill when has_github is introduced: until then, Non-GitHub
	// Users could only be recognised by their generated handle.
	if _, err := sqlDB.Exec(`ALTER TABLE participants ADD COLUMN has_github INTEGER NOT NULL DEFAULT 1`); err == nil {
		sqlDB.Exec(`UPDATE participants SET has_github = 0 WHERE github_handle LIKE 'no-github-%'`)
	}

	return &DB{db: sqlDB, changes: newChangeFeed()}, nil
}

func (db *DB) Close() error {
	return db.db.Close()
}

func (db *DB) SetMaxOpenConns(n int) {
	db.db.SetMaxOpenConns(n)
}

// Reset starts the event over: no Participants, no activity, and back to before the Reveal.
func (db *DB) Reset() error {
	if _, err := db.db.Exec(`DELETE FROM participants`); err != nil {
		return err
	}
	if _, err := db.db.Exec(`DELETE FROM activity_log`); err != nil {
		return err
	}
	return db.setEventState(BeforeReveal)
}

func scanParticipant(row interface{ Scan(...any) error }) (*Participant, error) {
	p := &Participant{}
	var profileJSON, questionsJSON, answersJSON, interestsJSON string
	err := row.Scan(
		&p.ID, &p.GitHubHandle, &p.HasGitHub, &p.Name,
		&p.PersonaName, &p.PersonaColor, &p.PersonaSymbol, &p.PersonaTagline,
		&profileJSON, &questionsJSON, &answersJSON, &interestsJSON,
		&p.PipelineStep,
		&p.MatchedWith, &p.CompatScore, &p.CompatReason,
		&p.RedFlags, &p.GreenFlags, &p.Icebreakers, &p.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	if profileJSON != "" {
		if err := json.Unmarshal([]byte(profileJSON), &p.Profile); err != nil {
			return nil, err
		}
	}
	if questionsJSON != "" {
		if err := json.Unmarshal([]byte(questionsJSON), &p.Questions); err != nil {
			return nil, err
		}
	}
	if answersJSON != "" {
		if err := json.Unmarshal([]byte(answersJSON), &p.Answers); err != nil {
			return nil, err
		}
	}
	if interestsJSON != "" {
		if err := json.Unmarshal([]byte(interestsJSON), &p.Interests); err != nil {
			return nil, err
		}
	}

	return p, nil
}

const selectParticipant = `
	SELECT id, github_handle, has_github, name,
	       persona_name, persona_color, persona_symbol, persona_tagline,
	       profile_json, questions, answers_json, interests, pipeline_step,
	       COALESCE(matched_with, ''), compat_score, compat_reason,
	       red_flags, green_flags, icebreakers, created_at
	FROM participants`

func (db *DB) CreateParticipant(id, handle, name string, hasGitHub bool) error {
	return db.inTx(func(tx *sql.Tx) error {
		// Persona colour and symbol come from the Persona module, given current use.
		rows, err := tx.Query(`SELECT persona_color, persona_symbol, COUNT(*) FROM participants GROUP BY persona_color, persona_symbol`)
		if err != nil {
			return err
		}
		uses := make(map[string]int)
		for rows.Next() {
			var c, s string
			var n int
			if rows.Scan(&c, &s, &n) == nil {
				uses[c+"|"+s] = n
			}
		}
		rows.Close()
		color, symbol := nextLook(uses)

		_, err = tx.Exec(
			`INSERT INTO participants (id, github_handle, has_github, name, persona_color, persona_symbol, questions, interests) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			id, handle, hasGitHub, name, color, symbol, "[]", "{}",
		)
		return err
	})
}

// inTx runs fn in a transaction, commits when it succeeds, and announces the change.
func (db *DB) inTx(fn func(tx *sql.Tx) error) error {
	tx, err := db.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	db.changed()
	return nil
}

// rowsAffected reads how many rows a statement changed. The SQLite driver
// always knows, so there is no error to handle.
func rowsAffected(res sql.Result) int64 {
	n, _ := res.RowsAffected()
	return n
}

func (db *DB) GetParticipant(id string) (*Participant, error) {
	row := db.db.QueryRow(selectParticipant+` WHERE id = ?`, id)
	return scanParticipant(row)
}

func (db *DB) GetParticipantByHandle(handle string) (*Participant, error) {
	row := db.db.QueryRow(selectParticipant+` WHERE github_handle = ?`, handle)
	return scanParticipant(row)
}

func (db *DB) SetProfile(id string, profile *GitHubProfile) error {
	encoded, _ := json.Marshal(profile) // plain data: cannot fail
	return db.write(`UPDATE participants SET profile_json = ? WHERE id = ?`, string(encoded), id)
}

// SetQuestions saves the Participant's interview question set.
func (db *DB) SetQuestions(id string, questions []Question) error {
	encoded, _ := json.Marshal(questions) // plain data: cannot fail
	return db.write(`UPDATE participants SET questions = ? WHERE id = ?`, string(encoded), id)
}

// SetPersona saves the Participant's Persona name and tagline.
func (db *DB) SetPersona(id, name, tagline string) error {
	return db.write(`UPDATE participants SET persona_name = ?, persona_tagline = ? WHERE id = ?`, name, tagline, id)
}

// write runs one statement and announces the change when it succeeds.
func (db *DB) write(query string, args ...any) error {
	_, err := db.db.Exec(query, args...)
	if err == nil {
		db.changed()
	}
	return err
}

func (db *DB) UpdateInterests(id string, interests map[string]interface{}) error {
	encoded, _ := json.Marshal(interests) // plain data: cannot fail
	return db.write(`UPDATE participants SET interests = ? WHERE id = ?`, string(encoded), id)
}

func (db *DB) UpdateAnswers(id string, answers map[string]string) error {
	encoded, _ := json.Marshal(answers) // plain data: cannot fail
	return db.write(`UPDATE participants SET answers_json = ? WHERE id = ?`, string(encoded), id)
}

func (db *DB) GetAllParticipants() ([]*Participant, error) {
	return db.queryParticipants(`ORDER BY created_at`)
}

func (db *DB) GetAllByStep(step Step) ([]*Participant, error) {
	return db.queryParticipants(`WHERE pipeline_step = ? ORDER BY created_at`, step)
}

// queryParticipants selects the Participants matching the given clause.
func (db *DB) queryParticipants(clause string, args ...any) ([]*Participant, error) {
	rows, err := db.db.Query(selectParticipant+" "+clause, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Participant
	for rows.Next() {
		p, err := scanParticipant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (db *DB) LogActivity(message string) {
	if _, err := db.db.Exec(`INSERT INTO activity_log (message) VALUES (?)`, message); err != nil {
		log.Printf("LogActivity: %v", err)
		return
	}
	db.changed()
}

func (db *DB) GetRecentActivity(limit int) ([]string, error) {
	rows, err := db.db.Query(`SELECT message FROM activity_log ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []string
	for rows.Next() {
		var m string
		if rows.Scan(&m) == nil { // a TEXT column always scans into a string
			msgs = append(msgs, m)
		}
	}
	return msgs, rows.Err()
}

func (db *DB) ParticipantCount() int {
	var n int
	db.db.QueryRow(`SELECT COUNT(*) FROM participants`).Scan(&n)
	return n
}

func (db *DB) ReadyCount() int {
	var n int
	db.db.QueryRow(`SELECT COUNT(*) FROM participants WHERE pipeline_step = ?`, StepReady).Scan(&n)
	return n
}

// LLM Cache methods

// GetLLMCache returns the cached assessment for a pair key. Rows that cannot
// be decoded (such as the old comma-joined format) count as a miss, using
// the same codec (and the same rule for undecodable data) as the
// Participant store's Match assessment.
func (db *DB) GetLLMCache(pairKey string) (*matchResult, bool) {
	var score int
	var reason, red, green, ice string
	err := db.db.QueryRow(
		`SELECT score, reason, red_flags, green_flags, icebreakers FROM llm_cache WHERE pair_key = ?`,
		pairKey,
	).Scan(&score, &reason, &red, &green, &ice)
	if err != nil {
		if err != sql.ErrNoRows {
			log.Printf("LLM cache read error for %s: %v", pairKey, err)
		}
		return nil, false
	}
	r, err := decodeMatchResult(score, reason, red, green, ice)
	if err != nil {
		return nil, false
	}
	return r, true
}

// SetLLMCache stores an assessment, with its lists JSON-encoded.
func (db *DB) SetLLMCache(pairKey string, r *matchResult) {
	red, green, ice := encodeMatchResult(r)
	_, err := db.db.Exec(
		`INSERT OR REPLACE INTO llm_cache (pair_key, score, reason, red_flags, green_flags, icebreakers) VALUES (?, ?, ?, ?, ?, ?)`,
		pairKey, r.Score, r.Reason, red, green, ice,
	)
	if err != nil {
		log.Printf("LLM cache write error for %s: %v", pairKey, err)
	}
}

func (db *DB) ClearLLMCache() {
	_, err := db.db.Exec(`DELETE FROM llm_cache`)
	if err != nil {
		log.Printf("LLM cache clear error: %v", err)
	}
}
