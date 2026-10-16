package main

import (
	"database/sql"
	"encoding/json"
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
	Interests      Interests
	PipelineStep   Step
	MatchedWith    string
	// Assessment is the Participant's own side of their current Match, decoded
	// from the five compat_* columns; nil when the Participant is unmatched.
	// The columns are written by the Participant store's Change and read by
	// the same codec as the LLM cache (decodeMatchResult).
	Assessment *matchResult
	CreatedAt  time.Time
	// MeetCode is the Participant's 6-character meet code (see meetcode.go);
	// assigned at registration and backfilled for existing Participants.
	MeetCode string
	// Theme is the Participant's theme choice's registry name (see theme.go);
	// midnight until they choose one in Settings.
	Theme string
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
			created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			meet_code        TEXT,
			theme            TEXT NOT NULL DEFAULT 'midnight'
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

		CREATE TABLE IF NOT EXISTS meetings (
			participant_a  TEXT NOT NULL,
			participant_b  TEXT NOT NULL,
			a_confirmed_at DATETIME,
			b_confirmed_at DATETIME,
			completed_at   DATETIME,
			origin         TEXT NOT NULL DEFAULT 'other',
			created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (participant_a, participant_b)
		);

		CREATE TABLE IF NOT EXISTS mission_cards (
			participant_id TEXT PRIMARY KEY REFERENCES participants(id),
			tiles          TEXT NOT NULL DEFAULT '[]',
			created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS contact_channels (
			participant_id TEXT NOT NULL REFERENCES participants(id),
			channel        TEXT NOT NULL,
			value          TEXT NOT NULL DEFAULT '',
			enabled        INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (participant_id, channel)
		);

		CREATE TABLE IF NOT EXISTS recaps (
			participant_id TEXT PRIMARY KEY REFERENCES participants(id),
			token          TEXT NOT NULL UNIQUE,
			created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS showtell_signups (
			id                INTEGER PRIMARY KEY AUTOINCREMENT,
			participant_id    TEXT NOT NULL UNIQUE REFERENCES participants(id),
			title             TEXT NOT NULL,
			description       TEXT NOT NULL DEFAULT '',
			repo_url          TEXT NOT NULL DEFAULT '',
			status            TEXT NOT NULL DEFAULT 'queued',
			identity_revealed INTEGER NOT NULL DEFAULT 0,
			hype              TEXT NOT NULL DEFAULT '',
			position          INTEGER NOT NULL DEFAULT 0,
			created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS showtell_questions (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			participant_id TEXT NOT NULL REFERENCES participants(id),
			signup_id      INTEGER NOT NULL REFERENCES showtell_signups(id),
			text           TEXT NOT NULL,
			status         TEXT NOT NULL DEFAULT 'queued',
			created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS slide_decks (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			title       TEXT NOT NULL,
			speaker     TEXT NOT NULL,
			filename    TEXT NOT NULL,
			stored_name TEXT NOT NULL UNIQUE,
			size        INTEGER NOT NULL,
			created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS board_posts (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			participant_id TEXT NOT NULL REFERENCES participants(id),
			text           TEXT NOT NULL,
			status         TEXT NOT NULL DEFAULT 'pending',
			created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
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
		`UPDATE event_state SET value = 'social' WHERE value = 'revealed'`,                // the Reveal became the Social phase
		`ALTER TABLE participants ADD COLUMN meet_code TEXT`,                              // meet codes, backfilled below
		`ALTER TABLE participants ADD COLUMN theme TEXT NOT NULL DEFAULT 'midnight'`,      // theme choices, default backfilled
	} {
		sqlDB.Exec(m)
	}

	// Meet codes are unique. The index is created after the migrations, so
	// databases that gained the column through them are covered too.
	if _, err := sqlDB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_participants_meet_code ON participants (meet_code)`); err != nil {
		return nil, err
	}

	// One-time backfill when has_github is introduced: until then, Non-GitHub
	// Users could only be recognised by their generated handle.
	if _, err := sqlDB.Exec(`ALTER TABLE participants ADD COLUMN has_github INTEGER NOT NULL DEFAULT 1`); err == nil {
		sqlDB.Exec(`UPDATE participants SET has_github = 0 WHERE github_handle LIKE 'no-github-%'`)
	}

	db := &DB{db: sqlDB, changes: newChangeFeed()}
	// Participants who registered before meet codes existed get them backfilled.
	if err := backfillMeetCodes(db); err != nil {
		logger("db").Error("backfilling meet codes", "err", err)
	}
	return db, nil
}

func (db *DB) Close() error {
	return db.db.Close()
}

func (db *DB) SetMaxOpenConns(n int) {
	db.db.SetMaxOpenConns(n)
}

func scanParticipant(row interface{ Scan(...any) error }) (*Participant, error) {
	p := &Participant{}
	var profileJSON, questionsJSON, answersJSON, interestsJSON string
	var compatScore int
	var compatReason, redFlags, greenFlags, icebreakers string
	err := row.Scan(
		&p.ID, &p.GitHubHandle, &p.HasGitHub, &p.Name,
		&p.PersonaName, &p.PersonaColor, &p.PersonaSymbol, &p.PersonaTagline,
		&profileJSON, &questionsJSON, &answersJSON, &interestsJSON,
		&p.PipelineStep,
		&p.MatchedWith, &compatScore, &compatReason,
		&redFlags, &greenFlags, &icebreakers, &p.CreatedAt, &p.MeetCode, &p.Theme,
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

	// The Match assessment is decoded by the same codec — and under the same
	// rule for undecodable data — as the LLM cache, so a corrupt row is
	// reported here rather than half-served. It is kept only while matched.
	assessment, err := decodeMatchResult(compatScore, compatReason, redFlags, greenFlags, icebreakers)
	if err != nil {
		return nil, err
	}
	if p.MatchedWith != "" {
		p.Assessment = assessment
	}

	return p, nil
}

const selectParticipant = `
	SELECT id, github_handle, has_github, name,
	       persona_name, persona_color, persona_symbol, persona_tagline,
	       profile_json, questions, answers_json, interests, pipeline_step,
	       COALESCE(matched_with, ''), compat_score, compat_reason,
	       red_flags, green_flags, icebreakers, created_at, COALESCE(meet_code, ''), theme
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
			rows.Scan(&c, &s, &n) // rows.Err(), checked below, reports a row that fails to scan
			uses[c+"|"+s] = n
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		color, symbol := nextLook(uses)

		_, err = tx.Exec(
			`INSERT INTO participants (id, github_handle, has_github, name, persona_color, persona_symbol, questions, interests) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			id, handle, hasGitHub, name, color, symbol, "[]", "{}",
		)
		if err != nil {
			return err
		}
		// Every Participant gets a meet code at registration.
		_, err = ensureMeetCode(tx, id)
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

// GetParticipantByMeetCode reads one Participant by their meet code.
func (db *DB) GetParticipantByMeetCode(code string) (*Participant, error) {
	row := db.db.QueryRow(selectParticipant+` WHERE meet_code = ?`, code)
	return scanParticipant(row)
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
		logger("db").Warn("activity log", "err", err)
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
		rows.Scan(&m) // rows.Err(), returned below, reports a row that fails to scan
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
	db.db.QueryRow(`SELECT COUNT(*) FROM participants WHERE pipeline_step = ?`, StepReady).Scan(&n)
	return n
}

// MatchedCount counts the Participants with a partner.
func (db *DB) MatchedCount() int {
	var n int
	db.db.QueryRow(`SELECT COUNT(*) FROM participants WHERE COALESCE(matched_with, '') <> ''`).Scan(&n)
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
			logger("db").Warn("LLM cache read", "pair", pairKey, "err", err)
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
		logger("db").Warn("LLM cache write", "pair", pairKey, "err", err)
	}
}

func (db *DB) ClearLLMCache() {
	_, err := db.db.Exec(`DELETE FROM llm_cache`)
	if err != nil {
		logger("db").Warn("LLM cache clear", "err", err)
	}
}
