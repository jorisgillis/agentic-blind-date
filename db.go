package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"math/rand"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var tailwindColors = []string{
	"bg-teal-400", "bg-red-400", "bg-purple-400", "bg-amber-400", "bg-blue-400",
}

var personaSymbols = []string{"🦊", "🦁", "🐯", "🐺", "🦝", "🦔", "🐙", "🦈", "🦅", "🐸"}

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
	PipelineStep   string
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
	db *sql.DB
}

// NewDB creates and initializes a new database connection.
// It opens a SQLite connection, pings to verify it works, and creates the necessary tables.
func NewDB(path string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	if err := sqlDB.Ping(); err != nil {
		return nil, err
	}

	_, err = sqlDB.Exec(`
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

		INSERT OR IGNORE INTO event_state (key, value) VALUES ('phase', 'onboarding');
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
		`ALTER TABLE participants DROP COLUMN extra_answers`, // ExtraAnswers live in profile_json
		`UPDATE participants SET pipeline_step = 'ready' WHERE pipeline_step = 'matched'`, // Relationship State is not a Pipeline Step
	} {
		sqlDB.Exec(m)
	}
	// One-time backfill when has_github is introduced: until then, Non-GitHub
	// Users could only be recognised by their generated handle.
	if _, err := sqlDB.Exec(`ALTER TABLE participants ADD COLUMN has_github INTEGER NOT NULL DEFAULT 1`); err == nil {
		sqlDB.Exec(`UPDATE participants SET has_github = 0 WHERE github_handle LIKE 'no-github-%'`)
	}

	return &DB{sqlDB}, nil
}

func (db *DB) Close() error {
	return db.db.Close()
}

func (db *DB) SetMaxOpenConns(n int) {
	db.db.SetMaxOpenConns(n)
}

func (db *DB) Reset() error {
	if _, err := db.db.Exec(`DELETE FROM participants`); err != nil {
		return err
	}
	_, err := db.db.Exec(`DELETE FROM activity_log`)
	return err
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
	tx, err := db.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Get all used color+symbol combinations
	rows, err := tx.Query(`SELECT persona_color, persona_symbol FROM participants`)
	if err != nil {
		return err
	}
	used := make(map[string]bool)
	for rows.Next() {
		var c, s string
		if err := rows.Scan(&c, &s); err != nil {
			rows.Close()
			return err
		}
		used[c+"|"+s] = true
	}
	rows.Close()

	// Find all available combinations
	var available [][2]string
	for _, c := range tailwindColors {
		for _, s := range personaSymbols {
			if !used[c+"|"+s] {
				available = append(available, [2]string{c, s})
			}
		}
	}

	var color, symbol string
	if len(available) > 0 {
		pick := available[rand.Intn(len(available))]
		color, symbol = pick[0], pick[1]
	} else {
		// Fallback: all combinations used, pick sequentially
		n := len(used)
		symbol = personaSymbols[n%len(personaSymbols)]
		color = tailwindColors[(n/len(personaSymbols))%len(tailwindColors)]
	}

	_, err = tx.Exec(
		`INSERT INTO participants (id, github_handle, has_github, name, persona_color, persona_symbol, questions, interests) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, handle, hasGitHub, name, color, symbol, "[]", "{}",
	)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) GetParticipant(id string) (*Participant, error) {
	row := db.db.QueryRow(selectParticipant+` WHERE id = ?`, id)
	return scanParticipant(row)
}

func (db *DB) GetParticipantByHandle(handle string) (*Participant, error) {
	row := db.db.QueryRow(selectParticipant+` WHERE github_handle = ?`, handle)
	return scanParticipant(row)
}

func (db *DB) UpdatePipelineStep(id, step string) error {
	_, err := db.db.Exec(`UPDATE participants SET pipeline_step = ? WHERE id = ?`, step, id)
	return err
}

// SetProfile saves the Participant's profile (GitHub data and ExtraAnswers).
func (db *DB) SetProfile(id string, profile *GitHubProfile) error {
	profileJSON, err := json.Marshal(profile)
	if err != nil {
		return err
	}
	_, err = db.db.Exec(`UPDATE participants SET profile_json = ? WHERE id = ?`, string(profileJSON), id)
	return err
}

// SetQuestions saves the Participant's interview question set.
func (db *DB) SetQuestions(id string, questions []Question) error {
	questionsJSON, err := json.Marshal(questions)
	if err != nil {
		return err
	}
	_, err = db.db.Exec(`UPDATE participants SET questions = ? WHERE id = ?`, string(questionsJSON), id)
	return err
}

// SetPersona saves the Participant's Persona name and tagline.
func (db *DB) SetPersona(id, name, tagline string) error {
	_, err := db.db.Exec(`UPDATE participants SET persona_name = ?, persona_tagline = ? WHERE id = ?`, name, tagline, id)
	return err
}

func (db *DB) UpdateInterests(id string, interests map[string]interface{}) error {
	interestsJSON, err := json.Marshal(interests)
	if err != nil {
		return err
	}
	_, err = db.db.Exec(`UPDATE participants SET interests = ? WHERE id = ?`, string(interestsJSON), id)
	return err
}

func (db *DB) UpdateAnswers(id string, answers map[string]string) error {
	answersJSON, err := json.Marshal(answers)
	if err != nil {
		return err
	}
	_, err = db.db.Exec(`UPDATE participants SET answers_json = ? WHERE id = ?`, string(answersJSON), id)
	return err
}

func (db *DB) GetAllParticipants() ([]*Participant, error) {
	return db.queryParticipants(`ORDER BY created_at`)
}

func (db *DB) GetAllByStep(step string) ([]*Participant, error) {
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

func (db *DB) GetPhase() (string, error) {
	var phase string
	err := db.db.QueryRow(`SELECT value FROM event_state WHERE key = 'phase'`).Scan(&phase)
	return phase, err
}

func (db *DB) SetPhase(phase string) error {
	_, err := db.db.Exec(`INSERT OR REPLACE INTO event_state (key, value) VALUES ('phase', ?)`, phase)
	return err
}

func (db *DB) LogActivity(message string) {
	if _, err := db.db.Exec(`INSERT INTO activity_log (message) VALUES (?)`, message); err != nil {
		log.Printf("LogActivity: %v", err)
	}
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
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
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
	db.db.QueryRow(`SELECT COUNT(*) FROM participants WHERE pipeline_step = 'ready'`).Scan(&n)
	return n
}

// LLM Cache methods

// GetLLMCache returns the cached assessment for a pair key. Rows that cannot be
// decoded (such as the old comma-joined format) count as a miss.
func (db *DB) GetLLMCache(pairKey string) (*matchResult, bool) {
	var r matchResult
	var red, green, ice string
	err := db.db.QueryRow(
		`SELECT score, reason, red_flags, green_flags, icebreakers FROM llm_cache WHERE pair_key = ?`,
		pairKey,
	).Scan(&r.Score, &r.Reason, &red, &green, &ice)
	if err != nil {
		if err != sql.ErrNoRows {
			log.Printf("LLM cache read error for %s: %v", pairKey, err)
		}
		return nil, false
	}
	for _, f := range []struct {
		raw string
		dst *[]string
	}{{red, &r.RedFlags}, {green, &r.GreenFlags}, {ice, &r.Icebreakers}} {
		if err := json.Unmarshal([]byte(f.raw), f.dst); err != nil {
			return nil, false
		}
		if *f.dst == nil {
			*f.dst = []string{}
		}
	}
	return &r, true
}

// SetLLMCache stores an assessment, with its lists JSON-encoded.
func (db *DB) SetLLMCache(pairKey string, r *matchResult) {
	red, _ := json.Marshal(nonNil(r.RedFlags))
	green, _ := json.Marshal(nonNil(r.GreenFlags))
	ice, _ := json.Marshal(nonNil(r.Icebreakers))
	_, err := db.db.Exec(
		`INSERT OR REPLACE INTO llm_cache (pair_key, score, reason, red_flags, green_flags, icebreakers) VALUES (?, ?, ?, ?, ?, ?)`,
		pairKey, r.Score, r.Reason, string(red), string(green), string(ice),
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
