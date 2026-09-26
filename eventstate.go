package main

// EventState is the state of the event: before the Reveal, Matches and
// identities stay hidden; once the admin reveals, they are shown. Only the
// Reveal and a Reset change it.
type EventState string

const (
	BeforeReveal EventState = "onboarding"
	Revealed     EventState = "revealed"
)

// IsRevealed reports whether the admin has revealed the Matches.
func (e EventState) IsRevealed() bool { return e == Revealed }

// Label describes the Event State for the Big Screen and admin pages.
func (e EventState) Label() string {
	if e.IsRevealed() {
		return "Matches revealed! 🎉"
	}
	return "Waiting for participants"
}

// EventState returns the current Event State; anything unreadable counts as before the Reveal.
func (db *DB) EventState() EventState {
	var state EventState
	if err := db.db.QueryRow(`SELECT value FROM event_state WHERE key = 'phase'`).Scan(&state); err != nil || state != Revealed {
		return BeforeReveal
	}
	return Revealed
}

// Reveal switches the event to revealed.
func (db *DB) Reveal() error {
	return db.setEventState(Revealed)
}

func (db *DB) setEventState(state EventState) error {
	if err := db.checkFault("reveal"); err != nil {
		return err
	}
	_, err := db.db.ExecContext(db.execCtx(), `INSERT OR REPLACE INTO event_state (key, value) VALUES ('phase', ?)`, state)
	if err == nil {
		db.changed()
	}
	return err
}

// bigScreenThemeKey is a second key in the same event_state table as the
// phase: Reset only ever replaces 'phase', so the admin's Big Screen Theme
// choice survives Reset and Rematch untouched, and a restart (#42).
const bigScreenThemeKey = "big_screen_theme"

// BigScreenTheme returns the admin's chosen Theme for the Big Screen, or the
// default Theme when none is set or the stored key is unknown (#42).
func (db *DB) BigScreenTheme() Theme {
	var key string
	if err := db.db.QueryRow(`SELECT value FROM event_state WHERE key = ?`, bigScreenThemeKey).Scan(&key); err != nil {
		key = ""
	}
	return ThemeByKey(key)
}

// SetBigScreenTheme stores the admin's Big Screen Theme choice.
func (db *DB) SetBigScreenTheme(key string) error {
	_, err := db.db.Exec(`INSERT OR REPLACE INTO event_state (key, value) VALUES (?, ?)`, bigScreenThemeKey, key)
	if err == nil {
		db.changed()
	}
	return err
}
