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

// DefaultRevealPairDuration is how long each pair stays on stage before the
// Reveal advances to the next one on its own (#83).
const DefaultRevealPairDuration = 5 * time.Second

// ShowState is the Game Show's current position, stored with the Event
// State so a restart resumes it (#78). Round, RoundOpen and RoundDeadline
// are meaningful only when Segment is SegmentRound: which Fixed Question
// (0-based) is the current Round, whether it still accepts answers, and
// when it closes on its own (#79). RevealIndex and RevealDeadline are
// meaningful only when Segment is SegmentReveal: which pair (0-based, in
// Show.revealPairs order) is currently on stage, and when it advances to
// the next one on its own (#83).
type ShowState struct {
	Segment        ShowSegment `json:"segment"`
	Round          int         `json:"round"`
	RoundOpen      bool        `json:"round_open"`
	RoundDeadline  time.Time   `json:"round_deadline"`
	RevealIndex    int         `json:"reveal_index"`
	RevealDeadline time.Time   `json:"reveal_deadline"`
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

// ErrRevealStillPlaying is returned by Advance while the Reveal is playing:
// it advances on its own, or with SkipToNextPair, never with Advance (#83).
var ErrRevealStillPlaying = errors.New("the Reveal advances on its own, or with SkipToNextPair")

// ErrRevealNotPlaying is returned by SkipToNextPair outside the Reveal (#83).
var ErrRevealNotPlaying = errors.New("the Reveal is not currently playing")

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
// has passed (#79), or advancing the Reveal to whichever pair its clock has
// reached — including all the way to the Afterparty once every pair has had
// its turn (#83). Either way, a read that observes this persists it, so it
// sticks and the change feed notifies open streams.
func (s *Show) State() ShowState {
	state := s.db.ShowState()
	switch {
	case state.Segment == SegmentRound && state.RoundOpen && !state.RoundDeadline.After(s.now()):
		state.RoundOpen = false
		s.db.setShowState(state)
	case state.Segment == SegmentReveal:
		if next, changed := s.advanceRevealIfDue(state); changed {
			state = next
			s.db.setShowState(state)
		}
	}
	return state
}

// advanceRevealIfDue moves the Reveal forward one pair at a time for every
// interval its deadline has fully passed, catching up in one read rather
// than drifting — landing on the Afterparty, dropping every Reveal field,
// once the last pair has had its turn (#83).
func (s *Show) advanceRevealIfDue(state ShowState) (ShowState, bool) {
	total := len(s.revealPairs())
	changed := false
	for !state.RevealDeadline.After(s.now()) {
		changed = true
		state.RevealIndex++
		if state.RevealIndex >= total {
			return ShowState{Segment: SegmentAfterparty}, true
		}
		state.RevealDeadline = state.RevealDeadline.Add(DefaultRevealPairDuration)
	}
	return state, changed
}

// nextState returns what follows state in the fixed segment order, for
// Advance — which never sees SegmentReveal (advance rejects it, State
// having already moved a finished Reveal on to the Afterparty) or
// SegmentAfterparty (nothing follows it).
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
	if current.Segment == SegmentReveal {
		return ErrRevealStillPlaying
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
		next.RevealDeadline = s.now().Add(DefaultRevealPairDuration)
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

// RevealPair is one pair the Reveal shows in turn: a genuine Match (A and B),
// or the wildcard alone (A only, Wildcard true) (#83).
type RevealPair struct {
	A, B     *Participant
	Wildcard bool
}

// revealPairs lists every Match, once each, plus the wildcard if there is
// one, in the order Participants registered — computed fresh from current
// Relationship State each time rather than stored, since nothing pairs
// anyone during the Reveal itself (#83).
func (s *Show) revealPairs() []RevealPair {
	all, _ := s.store.All()
	byID := make(map[string]*Participant, len(all))
	for _, p := range all {
		byID[p.ID] = p
	}
	var pairs []RevealPair
	seen := map[string]bool{}
	for _, p := range all {
		switch {
		case p.Wildcard:
			pairs = append(pairs, RevealPair{A: p, Wildcard: true})
		case p.MatchedWith != "":
			// byID[p.MatchedWith] always exists: the Relationship module keeps
			// both sides of a Match consistent in one transaction (#50).
			partner := byID[p.MatchedWith]
			if seen[pairKey(p, partner)] {
				continue
			}
			seen[pairKey(p, partner)] = true
			pairs = append(pairs, RevealPair{A: p, B: partner})
		}
	}
	return pairs
}

// SkipToNextPair moves the Reveal straight to the next pair, ahead of its
// own countdown — the Host's way to keep a large room moving (#83).
func (s *Show) SkipToNextPair() error {
	if !s.Active() {
		return ErrShowInactive
	}
	state := s.State()
	if state.Segment != SegmentReveal {
		return ErrRevealNotPlaying
	}
	state.RevealIndex++
	if state.RevealIndex >= len(s.revealPairs()) {
		return s.db.setShowState(ShowState{Segment: SegmentAfterparty})
	}
	state.RevealDeadline = s.now().Add(DefaultRevealPairDuration)
	return s.db.setShowState(state)
}

// CurrentRevealPair returns the pair currently on stage, and whether there
// is one — false outside the Reveal (#83).
func (s *Show) CurrentRevealPair() (RevealPair, bool) {
	state := s.State()
	if state.Segment != SegmentReveal {
		return RevealPair{}, false
	}
	pairs := s.revealPairs()
	if state.RevealIndex >= len(pairs) {
		return RevealPair{}, false
	}
	return pairs[state.RevealIndex], true
}

// RevealProgress reports the Reveal's position for the host console: the
// 1-based pair currently on stage, and how many there are in total. Both
// are 0 outside the Reveal (#83).
func (s *Show) RevealProgress() (current, total int) {
	state := s.State()
	if state.Segment != SegmentReveal {
		return 0, 0
	}
	total = len(s.revealPairs())
	return state.RevealIndex + 1, total
}

// PairRevealed reports whether a Participant's own pair (or wildcard moment)
// has had its turn yet: always true once the Reveal segment is over, since
// then every pair has (#83). Callers only need this once EventState.IsRevealed
// is already true — before that, nobody's pair has been revealed regardless.
func (s *Show) PairRevealed(participantID string) bool {
	state := s.State()
	if state.Segment != SegmentReveal {
		return true
	}
	for i, pair := range s.revealPairs() {
		if i > state.RevealIndex {
			break
		}
		if pair.A.ID == participantID || (pair.B != nil && pair.B.ID == participantID) {
			return true
		}
	}
	return false
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

// RoundCallout is a closed, multiple-choice Round's majority answer and a
// minority "hot take" for the stage view (#80).
type RoundCallout struct {
	MajorityAnswer string `json:"majority_answer"`
	MajorityCount  int    `json:"majority_count"`
	MinorityAnswer string `json:"minority_answer"`
	MinorityCount  int    `json:"minority_count"`
}

// RoundCallout tallies the current Round's answers once it has closed: nil
// while the Round is still open, outside a Round, for a free-text Question
// (no Options), or when everyone answered the same way — there's no
// minority to call out (#80).
func (s *Show) RoundCallout() *RoundCallout {
	state := s.State()
	if state.Segment != SegmentRound || state.RoundOpen {
		return nil
	}
	question := FixedQuestions[state.Round]
	if question.Options == nil {
		return nil
	}
	all, _ := s.store.All()
	counts := map[string]int{}
	for _, p := range all {
		if answer, ok := p.Answers[question.ID]; ok {
			counts[answer]++
		}
	}
	if len(counts) < 2 {
		return nil
	}
	callout := &RoundCallout{MinorityCount: -1}
	for _, option := range question.Options { // fixed order: ties resolve consistently
		n, ok := counts[option]
		if !ok {
			continue
		}
		if n > callout.MajorityCount {
			callout.MajorityAnswer, callout.MajorityCount = option, n
		}
	}
	for _, option := range question.Options {
		n, ok := counts[option]
		if !ok || option == callout.MajorityAnswer {
			continue
		}
		if callout.MinorityCount == -1 || n < callout.MinorityCount {
			callout.MinorityAnswer, callout.MinorityCount = option, n
		}
	}
	return callout
}

// NextActionLabel names, for the host console, what Advance would do from
// state — "" once there is nothing left (the Afterparty), "" while a Round
// is still open, since Advance is rejected then (the valid action is
// closing the Round instead, #79), and "" during the Reveal, since it plays
// on its own or with SkipToNextPair, never Advance (#83).
func NextActionLabel(state ShowState) string {
	if state.Segment == SegmentRound && state.RoundOpen {
		return ""
	}
	if state.Segment == SegmentReveal {
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
	default: // SegmentMatchmaking: "Start matchmaking"; SegmentReveal: "Start the Reveal" — the only Segments nextState can still produce here
		if next.Segment == SegmentMatchmaking {
			return "Start matchmaking"
		}
		return "Start the Reveal"
	}
}
