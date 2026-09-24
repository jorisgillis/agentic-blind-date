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
	_, err := db.db.Exec(`INSERT OR REPLACE INTO event_state (key, value) VALUES ('phase', ?)`, state)
	if err == nil {
		db.changed()
	}
	return err
}
