package main

import "database/sql"

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

// EventStore owns the event_state table: the Event State is read by every
// page that hides or shows Matches, and changed only by the Reveal and a
// Reset. It announces the Reveal on DB's change feed so streams refresh.
type EventStore struct {
	db *DB
}

// NewEventStore creates the event-state store.
func NewEventStore(db *DB) *EventStore {
	return &EventStore{db: db}
}

// State returns the current Event State; anything unreadable counts as before the Reveal.
func (s *EventStore) State() EventState {
	var state EventState
	if err := s.db.db.QueryRow(`SELECT value FROM event_state WHERE key = 'phase'`).Scan(&state); err != nil || state != Revealed {
		return BeforeReveal
	}
	return Revealed
}

// Reveal switches the event to revealed.
func (s *EventStore) Reveal() error {
	return s.set(Revealed)
}

func (s *EventStore) set(state EventState) error {
	_, err := s.db.db.Exec(`INSERT OR REPLACE INTO event_state (key, value) VALUES ('phase', ?)`, state)
	if err == nil {
		s.db.changed()
	}
	return err
}

// Reset starts the event over: no Participants, no activity, and back to
// before the Reveal — one transaction, so a failure leaves everything as it was.
func (s *EventStore) Reset() error {
	return s.db.inTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM participants`); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM activity_log`); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT OR REPLACE INTO event_state (key, value) VALUES ('phase', ?)`, BeforeReveal)
		return err
	})
}
