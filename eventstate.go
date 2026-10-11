package main

import (
	"database/sql"
	"errors"
	"fmt"
)

// Phase is the event's phase: how far the evening has progressed. The host
// drives it forward from the host console; only a Reset returns it to
// onboarding.
type Phase string

// The phases, in the order the event moves through them.
const (
	// PhaseOnboarding is the default after a Reset: Participants register,
	// the onboarding pipeline and Continuous Matching run, and Matches stay
	// hidden. Matched Participants wait and see a teaser.
	PhaseOnboarding Phase = "onboarding"
	// PhaseSocial is the Big Reveal at the break: Matches are revealed and
	// every Match page opens.
	PhaseSocial Phase = "social"
	// PhaseShowTell keeps everything in the Social; show & tell sign-up and
	// the question queue go live.
	PhaseShowTell Phase = "showtell"
	// PhaseClosed disables meeting confirmation and shows the closing view;
	// personal recap pages persist.
	PhaseClosed Phase = "closed"
)

// phases lists the phases in order; the event only moves forward through them.
var phases = []Phase{PhaseOnboarding, PhaseSocial, PhaseShowTell, PhaseClosed}

// ErrIllegalPhaseTransition is returned when the event cannot advance to a
// phase: an unknown phase, or one earlier than the current one.
var ErrIllegalPhaseTransition = errors.New("illegal phase transition")

// rank is the phase's position in the event's order; an unknown phase ranks
// below onboarding, so it never counts as reached.
func (p Phase) rank() int {
	for i, q := range phases {
		if q == p {
			return i
		}
	}
	return -1
}

// Valid reports whether p is one of the four phases.
func (p Phase) Valid() bool { return p.rank() >= 0 }

// AtLeast reports whether the event has reached the given phase or a later
// one: from the Social on, the Matches are revealed.
func (p Phase) AtLeast(q Phase) bool { return p.rank() >= q.rank() }

// Next lists the phases the event can still advance to, in order.
func (p Phase) Next() []Phase {
	var out []Phase
	for _, q := range phases {
		if q.rank() > p.rank() {
			out = append(out, q)
		}
	}
	return out
}

// Label describes the Phase for the Big Screen and the host console.
func (p Phase) Label() string {
	switch p {
	case PhaseSocial:
		return "Matches revealed! 🎉"
	case PhaseShowTell:
		return "Show & tell time! 🎤"
	case PhaseClosed:
		return "The event is closed. See you next time! 👋"
	}
	return "Waiting for participants"
}

// Activity is the line logged when the event advances to this phase.
func (p Phase) Activity() string {
	switch p {
	case PhaseSocial:
		return "🎉 The matches are revealed!"
	case PhaseShowTell:
		return "🎤 Show & tell time!"
	case PhaseClosed:
		return "👋 The event is closed. Thanks for playing!"
	}
	return ""
}

// AdvanceLabel is the host console's button label for advancing to this phase.
func (p Phase) AdvanceLabel() string {
	switch p {
	case PhaseSocial:
		return "🎉 Start the social — reveal every match"
	case PhaseShowTell:
		return "🎤 Start show & tell"
	case PhaseClosed:
		return "🔒 Close the event"
	}
	return ""
}

// AdvanceConfirm is the browser confirm shown before advancing to this phase.
func (p Phase) AdvanceConfirm() string {
	switch p {
	case PhaseSocial:
		return "Advance to the social? Every match becomes visible to everyone."
	case PhaseShowTell:
		return "Advance to show & tell? Sign-up and the question queue go live."
	case PhaseClosed:
		return "Close the event? The evening ends — recap pages persist."
	}
	return ""
}

// ButtonClass is the host console's button colour for advancing to this phase.
func (p Phase) ButtonClass() string {
	switch p {
	case PhaseSocial:
		return "bg-pink-600 hover:bg-pink-500"
	case PhaseShowTell:
		return "bg-purple-600 hover:bg-purple-500"
	}
	return "bg-gray-700 hover:bg-gray-600"
}

// Phase predicates, for templates (which cannot use the constants).
func (p Phase) IsOnboarding() bool { return p == PhaseOnboarding }
func (p Phase) IsSocial() bool     { return p == PhaseSocial }
func (p Phase) IsShowTell() bool   { return p == PhaseShowTell }
func (p Phase) IsClosed() bool     { return p == PhaseClosed }

// AllowsMeetings reports whether meeting confirmation is open: during the
// social and show & tell phases, and not before them or once closed.
func (p Phase) AllowsMeetings() bool { return p == PhaseSocial || p == PhaseShowTell }

// MatchingFrozen reports whether matching has frozen: from the social onward
// pairs are final — the admin's Rematch is disabled and Continuous Matching
// never takes over an existing Match.
func (p Phase) MatchingFrozen() bool { return p.AtLeast(PhaseSocial) }

// MissionCardsLive reports whether Mission cards are live: from the social
// (the Big Reveal) through show & tell — never during onboarding, and not
// once the event is closed.
func (p Phase) MissionCardsLive() bool { return p == PhaseSocial || p == PhaseShowTell }

// EventStore owns the event_state table: the Phase is read by every page
// that hides or shows Matches, and changed only by the host advancing the
// event and by a Reset. It announces every phase change on DB's change feed
// so streams refresh.
type EventStore struct {
	db *DB
}

// NewEventStore creates the event-state store.
func NewEventStore(db *DB) *EventStore {
	return &EventStore{db: db}
}

// State returns the current Phase; anything unreadable counts as onboarding.
func (s *EventStore) State() Phase {
	var state Phase
	if err := s.db.db.QueryRow(`SELECT value FROM event_state WHERE key = 'phase'`).Scan(&state); err != nil || !state.Valid() {
		return PhaseOnboarding
	}
	return state
}

// Advance moves the event to the given phase. Phases only move forward: the
// host may jump ahead (social → closed for a short evening), advancing to the
// current phase is a no-op, and going back — or naming an unknown phase — is
// rejected with ErrIllegalPhaseTransition.
func (s *EventStore) Advance(to Phase) error {
	if !to.Valid() {
		return fmt.Errorf("%w: %q is not a phase", ErrIllegalPhaseTransition, to)
	}
	current := s.State()
	if to == current {
		return nil
	}
	if to.rank() < current.rank() {
		return fmt.Errorf("%w: %s → %s goes back", ErrIllegalPhaseTransition, current, to)
	}
	return s.set(to)
}

// Reveal advances the event to the Social: the Big Reveal.
func (s *EventStore) Reveal() error {
	return s.Advance(PhaseSocial)
}

func (s *EventStore) set(phase Phase) error {
	_, err := s.db.db.Exec(`INSERT OR REPLACE INTO event_state (key, value) VALUES ('phase', ?)`, phase)
	if err == nil {
		s.db.changed()
	}
	return err
}

// Reset starts the event over: no Participants, no meetings, no mission
// cards, no contact channels, no recap tokens, no activity, and back to
// onboarding — one transaction, so a failure leaves everything as it was.
// Meetings expire silently: no message, no nag.
func (s *EventStore) Reset() error {
	return s.db.inTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM meetings`); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM mission_cards`); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM contact_channels`); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM recaps`); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM participants`); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM activity_log`); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT OR REPLACE INTO event_state (key, value) VALUES ('phase', ?)`, PhaseOnboarding)
		return err
	})
}
