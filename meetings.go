package main

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"
)

// MeetingOrigin records how a Meeting came about, from the initiator's
// perspective (match is symmetric; mission is not).
type MeetingOrigin string

// The meeting origins.
const (
	// OriginMatch: the pair are each other's current Match.
	OriginMatch MeetingOrigin = "match"
	// OriginMission: the meeting was initiated from a mission card.
	OriginMission MeetingOrigin = "mission"
	// OriginOther: anyone else at the event.
	OriginOther MeetingOrigin = "other"
)

// Meeting is one canonical row per pair of Participants who met:
// participant_a is the lexicographically lower id (the matcher's pair-key
// ordering), so there is one row per pair regardless of who confirms first.
// Each side's confirmation is tracked separately; completed_at is set when
// both sides have confirmed.
type Meeting struct {
	A            string        `json:"participant_a"`
	B            string        `json:"participant_b"`
	AConfirmedAt *time.Time    `json:"a_confirmed_at"`
	BConfirmedAt *time.Time    `json:"b_confirmed_at"`
	CompletedAt  *time.Time    `json:"completed_at"`
	Origin       MeetingOrigin `json:"origin"`
	CreatedAt    time.Time     `json:"created_at"`
}

// MeetingStatus is how a Meeting stands from one Participant's perspective.
type MeetingStatus string

// The meeting statuses, from one Participant's perspective.
const (
	// MeetingCompleted: both sides confirmed.
	MeetingCompleted MeetingStatus = "completed"
	// MeetingPending: you confirmed, the other side has not yet.
	MeetingPending MeetingStatus = "pending"
	// MeetingAwaiting: the other side confirmed, you have not yet.
	MeetingAwaiting MeetingStatus = "awaiting"
)

// canonicalPair orders two Participant ids like the matcher's pair key: the
// lexicographically lower id first.
func canonicalPair(a, b string) (string, string) {
	if a < b {
		return a, b
	}
	return b, a
}

// Completed reports whether both sides have confirmed.
func (m *Meeting) Completed() bool { return m.CompletedAt != nil }

// ConfirmedBy reports whether the given Participant's side is confirmed.
func (m *Meeting) ConfirmedBy(id string) bool {
	if id == m.A {
		return m.AConfirmedAt != nil
	}
	return m.BConfirmedAt != nil
}

// Other returns the pair's other Participant id.
func (m *Meeting) Other(id string) string {
	if id == m.A {
		return m.B
	}
	return m.A
}

// StatusFor reports the Meeting's status from the given Participant's
// perspective.
func (m *Meeting) StatusFor(id string) MeetingStatus {
	switch {
	case m.Completed():
		return MeetingCompleted
	case m.ConfirmedBy(id):
		return MeetingPending
	default:
		return MeetingAwaiting
	}
}

// ErrSelfMeeting is returned when a Participant tries to meet themselves.
var ErrSelfMeeting = errors.New("cannot meet yourself")

// ErrMeetingsClosed is returned when confirmation is attempted outside the
// social and show & tell phases.
var ErrMeetingsClosed = errors.New("meeting confirmation is not open")

const selectMeeting = `
	SELECT participant_a, participant_b, a_confirmed_at, b_confirmed_at, completed_at, origin, created_at
	FROM meetings`

// scanMeeting scans one Meeting row. The confirmation timestamps are
// NULL-able, so they are scanned through sql.NullTime.
func scanMeeting(row interface{ Scan(...any) error }) (*Meeting, error) {
	m := &Meeting{}
	var aConfirmed, bConfirmed, completed sql.NullTime
	if err := row.Scan(&m.A, &m.B, &aConfirmed, &bConfirmed, &completed, &m.Origin, &m.CreatedAt); err != nil {
		return nil, err
	}
	if aConfirmed.Valid {
		m.AConfirmedAt = &aConfirmed.Time
	}
	if bConfirmed.Valid {
		m.BConfirmedAt = &bConfirmed.Time
	}
	if completed.Valid {
		m.CompletedAt = &completed.Time
	}
	return m, nil
}

// meetingRow reads the pair's canonical row from row, mapping "no row" to nil.
func meetingRow(row *sql.Row) (*Meeting, error) {
	m, err := scanMeeting(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return m, nil
}

// Meetings owns the meetings table: the two-sided confirm handshake. Each
// confirm is a read-then-write on the pair's single canonical row, so mu
// serializes them — without it, two phones confirming at the same moment
// could race the row's creation or read it one-sided.
type Meetings struct {
	db     *DB
	events *EventStore
	mu     sync.Mutex
}

// NewMeetings creates the Meetings module.
func NewMeetings(db *DB, events *EventStore) *Meetings {
	return &Meetings{db: db, events: events}
}

// Confirm records confirmer's side of their meeting with target, creating the
// pair's canonical row on the first confirm. The origin is tagged
// automatically from the initiator's perspective: a pair that are each
// other's current Match meets as match; otherwise the given origin stands.
// Confirm is idempotent per side: a second confirm from the same side is a
// friendly no-op (already=true). When both sides have confirmed, the meeting
// is completed and the ticker announces it.
func (ms *Meetings) Confirm(confirmer, target *Participant, origin MeetingOrigin) (meeting *Meeting, already bool, err error) {
	if confirmer.ID == target.ID {
		return nil, false, ErrSelfMeeting
	}
	if phase := ms.events.State(); !phase.AllowsMeetings() {
		return nil, false, fmt.Errorf("%w: the event is in the %s phase", ErrMeetingsClosed, phase)
	}

	ms.mu.Lock()
	defer ms.mu.Unlock()

	a, b := canonicalPair(confirmer.ID, target.ID)
	now := time.Now()
	completed := false
	err = ms.db.inTx(func(tx *sql.Tx) error {
		m, err := meetingRow(tx.QueryRow(selectMeeting+` WHERE participant_a = ? AND participant_b = ?`, a, b))
		if err != nil {
			return err
		}
		if m == nil {
			// The first side: create the canonical row, tagging the origin
			// from the initiator's perspective — a current Match meets as match.
			if confirmer.MatchedWith == target.ID {
				origin = OriginMatch
			}
			m = &Meeting{A: a, B: b, Origin: origin}
			if confirmer.ID == a {
				m.AConfirmedAt = &now
			} else {
				m.BConfirmedAt = &now
			}
			_, err := tx.Exec(`INSERT INTO meetings (participant_a, participant_b, a_confirmed_at, b_confirmed_at, origin) VALUES (?, ?, ?, ?, ?)`,
				a, b, m.AConfirmedAt, m.BConfirmedAt, m.Origin)
			if err != nil {
				return err
			}
			meeting = m
			return nil
		}
		// The same side confirming again is a friendly no-op.
		if m.ConfirmedBy(confirmer.ID) {
			already = true
			meeting = m
			return nil
		}
		if confirmer.ID == a {
			m.AConfirmedAt = &now
			_, err = tx.Exec(`UPDATE meetings SET a_confirmed_at = ? WHERE participant_a = ? AND participant_b = ?`, now, a, b)
		} else {
			m.BConfirmedAt = &now
			_, err = tx.Exec(`UPDATE meetings SET b_confirmed_at = ? WHERE participant_a = ? AND participant_b = ?`, now, a, b)
		}
		if err != nil {
			return err
		}
		if m.AConfirmedAt != nil && m.BConfirmedAt != nil {
			m.CompletedAt = &now
			if _, err := tx.Exec(`UPDATE meetings SET completed_at = ? WHERE participant_a = ? AND participant_b = ?`, now, a, b); err != nil {
				return err
			}
			completed = true
		}
		meeting = m
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	if completed {
		// The announcement names the pair in canonical order.
		x, y := confirmer, target
		if meeting.B == confirmer.ID {
			x, y = target, confirmer
		}
		ms.db.LogActivity(fmt.Sprintf("🤝 %s and %s met!", x.PersonaName, y.PersonaName))
	}
	return meeting, already, nil
}

// Between returns the Meeting between two Participants, or nil when they
// haven't met.
func (ms *Meetings) Between(a, b string) (*Meeting, error) {
	a, b = canonicalPair(a, b)
	return meetingRow(ms.db.db.QueryRow(selectMeeting+` WHERE participant_a = ? AND participant_b = ?`, a, b))
}

// All returns every Meeting, oldest first — the debug endpoint's view.
func (ms *Meetings) All() ([]Meeting, error) {
	return ms.queryMeetings(`ORDER BY created_at`)
}

// ForParticipant returns the Meetings the Participant is part of, oldest first.
func (ms *Meetings) ForParticipant(id string) ([]Meeting, error) {
	return ms.queryMeetings(`WHERE participant_a = ? OR participant_b = ? ORDER BY created_at`, id, id)
}

// queryMeetings selects the Meetings matching the given clause.
func (ms *Meetings) queryMeetings(clause string, args ...any) ([]Meeting, error) {
	rows, err := ms.db.db.Query(selectMeeting+" "+clause, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Meeting
	for rows.Next() {
		m, err := scanMeeting(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// Connections derives a Participant's connection count from their completed
// Meetings: one connection per completed meeting (derived, never stored).
func (ms *Meetings) Connections(id string) int {
	var n int
	ms.db.db.QueryRow(`SELECT COUNT(*) FROM meetings WHERE completed_at IS NOT NULL AND (participant_a = ? OR participant_b = ?)`, id, id).Scan(&n)
	return n
}

// RemoveParticipant deletes a Participant's Meetings when they leave the event.
func (ms *Meetings) RemoveParticipant(id string) error {
	_, err := ms.db.db.Exec(`DELETE FROM meetings WHERE participant_a = ? OR participant_b = ?`, id, id)
	if err == nil {
		ms.db.changed()
	}
	return err
}
