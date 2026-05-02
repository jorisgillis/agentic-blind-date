package main

import (
	"database/sql"
	"log"
	"math/rand"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var tailwindColors = []string{
	"bg-teal-400", "bg-red-400", "bg-purple-400", "bg-amber-400", "bg-blue-400",
}

var personaSymbols = []string{"🦊", "🦁", "🐯", "🐺", "🦝", "🦔", "🐙", "🦈", "🦅", "🐸"}

type Participant struct {
	ID              string
	GitHubHandle    string
	Name            string
	PersonaName     string
	PersonaColor    string
	PersonaSymbol   string
	PersonaTagline  string
	ProfileJSON     string
	CustomQuestions string
	AnswersJSON     string
	PipelineStep    string
	MatchedWith     string
	CompatScore     int
	CompatReason    string
	RedFlags        string
	GreenFlags      string
	Icebreakers     string
	CreatedAt       time.Time
}

type DB struct {
	db *sql.DB
}

func initDB(path string) (*DB, error) {
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
			github_handle    TEXT UNIQUE NOT NULL,
			name             TEXT NOT NULL DEFAULT '',
			persona_name     TEXT NOT NULL DEFAULT '',
			persona_color    TEXT NOT NULL DEFAULT 'bg-gray-400',
			persona_symbol   TEXT NOT NULL DEFAULT '🎭',
			persona_tagline  TEXT NOT NULL DEFAULT '',
			profile_json     TEXT NOT NULL DEFAULT '{}',
			custom_questions TEXT NOT NULL DEFAULT '[]',
			answers_json     TEXT NOT NULL DEFAULT '{}',
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
	} {
		sqlDB.Exec(m)
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

func (db *DB) UnmatchAll() error {
	_, err := db.db.Exec(`UPDATE participants SET matched_with='', compat_score=0, compat_reason='', pipeline_step='ready' WHERE pipeline_step='matched'`)
	return err
}

func (db *DB) DeleteParticipant(id string) error {
	_, err := db.db.Exec(`DELETE FROM participants WHERE id=?`, id)
	return err
}

func scanParticipant(row interface{ Scan(...any) error }) (*Participant, error) {
	p := &Participant{}
	err := row.Scan(
		&p.ID, &p.GitHubHandle, &p.Name,
		&p.PersonaName, &p.PersonaColor, &p.PersonaSymbol, &p.PersonaTagline,
		&p.ProfileJSON, &p.CustomQuestions, &p.AnswersJSON, &p.PipelineStep,
		&p.MatchedWith, &p.CompatScore, &p.CompatReason,
		&p.RedFlags, &p.GreenFlags, &p.Icebreakers, &p.CreatedAt,
	)
	return p, err
}

const selectParticipant = `
	SELECT id, github_handle, name,
	       persona_name, persona_color, persona_symbol, persona_tagline,
	       profile_json, custom_questions, answers_json, pipeline_step,
	       COALESCE(matched_with, ''), compat_score, compat_reason,
	       red_flags, green_flags, icebreakers, created_at
	FROM participants`

func (db *DB) CreateParticipant(id, handle, name string) error {
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
		`INSERT INTO participants (id, github_handle, name, persona_color, persona_symbol) VALUES (?, ?, ?, ?, ?)`,
		id, handle, name, color, symbol,
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

func (db *DB) UpdateProfile(id, profileJSON, personaName, personaTagline, customQuestions string) error {
	_, err := db.db.Exec(`
		UPDATE participants SET profile_json = ?, persona_name = ?, persona_tagline = ?, custom_questions = ?
		WHERE id = ?`, profileJSON, personaName, personaTagline, customQuestions, id)
	return err
}

func (db *DB) UpdateAnswers(id, answersJSON string) error {
	_, err := db.db.Exec(`UPDATE participants SET answers_json = ? WHERE id = ?`, answersJSON, id)
	return err
}

func (db *DB) SetMatched(id, matchedWith string, score int, reason, redFlags, greenFlags, icebreakers string) error {
	_, err := db.db.Exec(`
		UPDATE participants SET matched_with = ?, compat_score = ?, compat_reason = ?,
		    red_flags = ?, green_flags = ?, icebreakers = ?, pipeline_step = 'matched'
		WHERE id = ?`, matchedWith, score, reason, redFlags, greenFlags, icebreakers, id)
	return err
}

func (db *DB) GetAllParticipants() ([]*Participant, error) {
	rows, err := db.db.Query(selectParticipant + ` ORDER BY created_at`)
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

func (db *DB) GetAllByStep(step string) ([]*Participant, error) {
	rows, err := db.db.Query(selectParticipant+` WHERE pipeline_step = ? ORDER BY created_at`, step)
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

func (db *DB) GetReadyUnmatched() ([]*Participant, error) {
	// Get participants who are ready and not yet matched
	rows, err := db.db.Query(selectParticipant+` WHERE pipeline_step = 'ready' AND (matched_with = '' OR matched_with IS NULL) ORDER BY created_at`)
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

func (db *DB) UnmatchParticipant(id string) error {
	_, err := db.db.Exec(`UPDATE participants SET matched_with='', compat_score=0, compat_reason='', pipeline_step='ready' WHERE id=?`, id)
	return err
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
	db.db.QueryRow(`SELECT COUNT(*) FROM participants WHERE pipeline_step IN ('ready', 'matched')`).Scan(&n)
	return n
}
