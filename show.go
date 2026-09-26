package main

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ShowSegment is a stage of the Game Show, in order (CONTEXT.md).
type ShowSegment string

const (
	SegmentLobby       ShowSegment = "lobby"
	SegmentRound       ShowSegment = "round"
	SegmentMatchmaking ShowSegment = "matchmaking"
	SegmentReveal      ShowSegment = "reveal"
	SegmentAfterparty  ShowSegment = "afterparty"
)

// ShowState is the Game Show's current position, stored with the Event
// State so a restart resumes it (#78). Round is meaningful only when
// Segment is SegmentRound: which Fixed Question (0-based) is the current Round.
type ShowState struct {
	Segment ShowSegment `json:"segment"`
	Round   int         `json:"round"`
}

const showStateKey = "show_state"

// ShowState returns the Game Show's current position, defaulting to the
// Lobby when unset or unreadable.
func (db *DB) ShowState() ShowState {
	var raw string
	if err := db.db.QueryRow(`SELECT value FROM event_state WHERE key = ?`, showStateKey).Scan(&raw); err != nil {
		return ShowState{Segment: SegmentLobby}
	}
	var s ShowState
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return ShowState{Segment: SegmentLobby}
	}
	return s
}

func (db *DB) setShowState(s ShowState) error {
	data, _ := json.Marshal(s) // ShowState is plain data: this cannot fail
	_, err := db.db.Exec(`INSERT OR REPLACE INTO event_state (key, value) VALUES (?, ?)`, showStateKey, string(data))
	if err == nil {
		db.changed()
	}
	return err
}

// ErrShowInactive is returned by every Show operation when the Event Format
// is not the Game Show.
var ErrShowInactive = errors.New("the Show module is only active in a Game Show")

// ErrShowSegmentOrder is returned when a Show operation would move out of
// order — including advancing past the last segment.
var ErrShowSegmentOrder = errors.New("segments only advance in order")

// Show owns the Game Show's segments: the Lobby, the six Rounds, the
// matchmaking segment, the Reveal and the Afterparty. It is inactive in the
// Classic flow (#78).
type Show struct {
	db *DB
}

// NewShow creates the Show module.
func NewShow(db *DB) *Show {
	return &Show{db: db}
}

// Active reports whether the Event Format is the Game Show.
func (s *Show) Active() bool {
	return s.db.EventFormat() == GameShow
}

// State returns the current segment.
func (s *Show) State() ShowState {
	return s.db.ShowState()
}

// nextState returns what follows state in the fixed segment order, or false
// once there is nothing left (the Afterparty never advances further).
func nextState(state ShowState) (ShowState, bool) {
	switch state.Segment {
	case SegmentLobby:
		return ShowState{Segment: SegmentRound, Round: 0}, true
	case SegmentRound:
		if state.Round+1 < TotalFixedQuestions {
			return ShowState{Segment: SegmentRound, Round: state.Round + 1}, true
		}
		return ShowState{Segment: SegmentMatchmaking}, true
	case SegmentMatchmaking:
		return ShowState{Segment: SegmentReveal}, true
	case SegmentReveal:
		return ShowState{Segment: SegmentAfterparty}, true
	default:
		return ShowState{}, false
	}
}

// Advance moves to the next segment: the Host's one action for "start the
// show", "open the next Round" (including past the last Round, into the
// matchmaking segment) and "start the Reveal". Later tickets add the Round's
// own countdown and the Reveal's pair-by-pair choreography on top of this
// same state. Entering the Reveal also reveals the Matches (#78), so every
// existing Event-State-driven rule (identity hiding, the Match page) applies
// without a parallel Game Show special case.
func (s *Show) Advance() error {
	if !s.Active() {
		return ErrShowInactive
	}
	next, ok := nextState(s.State())
	if !ok {
		return ErrShowSegmentOrder
	}
	if next.Segment == SegmentReveal {
		if err := s.db.Reveal(); err != nil {
			return err
		}
	}
	return s.db.setShowState(next)
}

// NextActionLabel names, for the host console, what Advance would do from
// state — "" once there is nothing left (the Afterparty).
func NextActionLabel(state ShowState) string {
	next, ok := nextState(state)
	if !ok {
		return ""
	}
	switch next.Segment {
	case SegmentRound:
		if state.Segment == SegmentLobby {
			return "Start the show"
		}
		return fmt.Sprintf("Open Round %d", next.Round+1)
	case SegmentMatchmaking:
		return "Start matchmaking"
	case SegmentReveal:
		return "Start the Reveal"
	default: // SegmentAfterparty: the only Segment nextState can still produce here
		return "Move to the Afterparty"
	}
}
