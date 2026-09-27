package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
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

// DefaultRoundDuration is a Round's countdown unless the Host sets another (#79).
const DefaultRoundDuration = 15 * time.Second

// ShowState is the Game Show's current position, stored with the Event
// State so a restart resumes it (#78). Round, RoundOpen and RoundDeadline
// are meaningful only when Segment is SegmentRound: which Fixed Question
// (0-based) is the current Round, whether it still accepts answers, and
// when it closes on its own (#79).
type ShowState struct {
	Segment       ShowSegment `json:"segment"`
	Round         int         `json:"round"`
	RoundOpen     bool        `json:"round_open"`
	RoundDeadline time.Time   `json:"round_deadline"`
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

// ErrRoundStillOpen is returned by Advance/OpenRound when the current Round
// hasn't closed yet: the Host must close it (or let its countdown end)
// before moving on (#79).
var ErrRoundStillOpen = errors.New("the current Round is still open")

// ErrRoundNotOpen is returned by CloseRound and SubmitRoundAnswer when no
// Round is currently open (#79).
var ErrRoundNotOpen = errors.New("no Round is open")

// Show owns the Game Show's segments: the Lobby, the six Rounds, the
// matchmaking segment, the Reveal and the Afterparty. It is inactive in the
// Classic flow (#78).
type Show struct {
	db    *DB
	store *ParticipantStore
	now   func() time.Time // an injected clock, so tests never wait on real time (#79)
}

// NewShow creates the Show module.
func NewShow(db *DB, now func() time.Time) *Show {
	return &Show{db: db, store: NewParticipantStore(db), now: now}
}

// Active reports whether the Event Format is the Game Show.
func (s *Show) Active() bool {
	return s.db.EventFormat() == GameShow
}

// State returns the current segment, closing an open Round whose countdown
// has passed (#79) — a read that, when it observes this, persists the
// closure so it sticks and the change feed notifies open streams.
func (s *Show) State() ShowState {
	state := s.db.ShowState()
	if state.Segment == SegmentRound && state.RoundOpen && !state.RoundDeadline.After(s.now()) {
		state.RoundOpen = false
		s.db.setShowState(state)
	}
	return state
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

// Advance moves to the next segment using the default Round countdown: the
// Host's one action for "start the show", "open the next Round" (including
// past the last Round, into the matchmaking segment) and "start the
// Reveal". Rejected while the current Round is still open (#79): the Host
// must close it, or let its countdown end, first. Entering the Reveal also
// reveals the Matches (#78), so every existing Event-State-driven rule
// (identity hiding, the Match page) applies without a parallel Game Show
// special case.
func (s *Show) Advance() error {
	return s.advance(DefaultRoundDuration)
}

// OpenRound advances like Advance, but gives the Round it opens a countdown
// of duration instead of the default 15 seconds (#79).
func (s *Show) OpenRound(duration time.Duration) error {
	return s.advance(duration)
}

func (s *Show) advance(roundDuration time.Duration) error {
	if !s.Active() {
		return ErrShowInactive
	}
	current := s.State()
	if current.Segment == SegmentRound && current.RoundOpen {
		return ErrRoundStillOpen
	}
	next, ok := nextState(current)
	if !ok {
		return ErrShowSegmentOrder
	}
	if next.Segment == SegmentRound {
		next.RoundOpen = true
		next.RoundDeadline = s.now().Add(roundDuration)
	}
	if next.Segment == SegmentReveal {
		if err := s.db.Reveal(); err != nil {
			return err
		}
	}
	return s.db.setShowState(next)
}

// CloseRound closes the current Round early: from then on it no longer
// accepts answers (#79).
func (s *Show) CloseRound() error {
	if !s.Active() {
		return ErrShowInactive
	}
	state := s.State()
	if state.Segment != SegmentRound || !state.RoundOpen {
		return ErrRoundNotOpen
	}
	state.RoundOpen = false
	return s.db.setShowState(state)
}

// SubmitRoundAnswer records a Participant's answer to the current Round's
// Fixed Question. Accepted, and changeable, only while the Round is open;
// stored as the Participant's answer to that Fixed Question, so the
// heuristic score, the Persona prompt and the data page read it unchanged (#79).
func (s *Show) SubmitRoundAnswer(participantID, answer string) error {
	if !s.Active() {
		return ErrShowInactive
	}
	state := s.State()
	if state.Segment != SegmentRound || !state.RoundOpen {
		return ErrRoundNotOpen
	}
	question := FixedQuestions[state.Round]
	p, err := s.store.Get(participantID)
	if err != nil {
		return err
	}
	answers := make(map[string]string, len(p.Answers)+1)
	for k, v := range p.Answers {
		answers[k] = v
	}
	answers[question.ID] = answer
	return s.store.Change(ParticipantChange{ID: participantID, Answers: answers})
}

// RoundAnswerCount reports how many Participants have answered the current
// Round's Fixed Question, for the host console (#79). 0 outside a Round.
func (s *Show) RoundAnswerCount() int {
	state := s.State()
	if state.Segment != SegmentRound {
		return 0
	}
	all, _ := s.store.All()
	question := FixedQuestions[state.Round]
	n := 0
	for _, p := range all {
		if _, ok := p.Answers[question.ID]; ok {
			n++
		}
	}
	return n
}

// RoundSecondsLeft reports how many seconds remain in the current Round's
// countdown, rounded up. 0 outside an open Round.
func (s *Show) RoundSecondsLeft() int {
	state := s.State()
	if state.Segment != SegmentRound || !state.RoundOpen {
		return 0
	}
	remaining := state.RoundDeadline.Sub(s.now())
	if remaining <= 0 {
		return 0
	}
	return int((remaining + time.Second - 1) / time.Second)
}

// NextActionLabel names, for the host console, what Advance would do from
// state — "" once there is nothing left (the Afterparty), and "" while a
// Round is still open, since Advance is rejected then: the valid action is
// closing the Round instead (#79).
func NextActionLabel(state ShowState) string {
	if state.Segment == SegmentRound && state.RoundOpen {
		return ""
	}
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
