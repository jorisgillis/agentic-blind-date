package main

import "errors"

// EventFormat is how the event runs (spec #75): the admin picks it before
// the event starts. It can change only while there are no Participants —
// before the first registers, or after a Reset (#77).
type EventFormat string

const (
	ClassicFlow EventFormat = "classic"
	GameShow    EventFormat = "game_show"
)

const eventFormatKey = "event_format"

// ErrEventFormatLocked is returned when the Event Format is changed with
// Participants present.
var ErrEventFormatLocked = errors.New("the Event Format can only change before the first Participant registers, or after a Reset")

// EventFormat returns the current Event Format, defaulting to the Classic
// flow when unset or unrecognised.
func (db *DB) EventFormat() EventFormat {
	var v string
	if err := db.db.QueryRow(`SELECT value FROM event_state WHERE key = ?`, eventFormatKey).Scan(&v); err != nil {
		return ClassicFlow
	}
	if v != string(GameShow) {
		return ClassicFlow
	}
	return GameShow
}

// SetEventFormat stores the Event Format, refusing to change it while any
// Participant exists (#77).
func (db *DB) SetEventFormat(f EventFormat) error {
	if db.ParticipantCount() > 0 {
		return ErrEventFormatLocked
	}
	_, err := db.db.Exec(`INSERT OR REPLACE INTO event_state (key, value) VALUES (?, ?)`, eventFormatKey, f)
	if err == nil {
		db.changed()
	}
	return err
}
