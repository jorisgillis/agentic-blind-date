package main

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestShow_InactiveInTheClassicFlow(t *testing.T) {
	db := newTestDB(t)
	show := NewShow(db, time.Now)

	if show.Active() {
		t.Error("want the Show inactive in the Classic flow")
	}
	if err := show.Advance(); !errors.Is(err, ErrShowInactive) {
		t.Errorf("want ErrShowInactive, got %v", err)
	}
}

func TestShow_StartsInTheLobby(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	show := NewShow(db, time.Now)

	if got := show.State(); got.Segment != SegmentLobby {
		t.Errorf("want the Lobby, got %+v", got)
	}
}

// TestShow_SegmentsAdvanceInOrder covers #78: one call to Advance moves the
// show exactly one segment forward, through every Round in turn, into
// matchmaking, the Reveal and the Afterparty — where it stops. Each Round it
// enters is immediately open, with a deadline in the future (#79); Advance
// is rejected while it stays open, so the test closes each one first.
func TestShow_SegmentsAdvanceInOrder(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	clock := newFakeNow()
	show := NewShow(db, clock.Now)

	want := []struct {
		segment ShowSegment
		round   int
	}{
		{SegmentRound, 0}, {SegmentRound, 1}, {SegmentRound, 2},
		{SegmentRound, 3}, {SegmentRound, 4}, {SegmentRound, 5},
		{SegmentMatchmaking, 0}, {SegmentReveal, 0}, {SegmentAfterparty, 0},
	}
	for i, w := range want {
		advance := show.Advance
		if show.State().Segment == SegmentReveal { // #83: plays on its own, or skipped, never Advance
			advance = show.SkipToNextPair
		}
		if err := advance(); err != nil {
			t.Fatalf("advance %d: %v", i, err)
		}
		got := show.State()
		if got.Segment != w.segment || got.Round != w.round {
			t.Fatalf("advance %d: want segment %q round %d, got %+v", i, w.segment, w.round, got)
		}
		if got.Segment == SegmentRound {
			if !got.RoundOpen {
				t.Fatalf("advance %d: a newly opened Round should be open", i)
			}
			if err := show.CloseRound(); err != nil {
				t.Fatalf("advance %d: closing the Round: %v", i, err)
			}
		}
	}

	if err := show.Advance(); !errors.Is(err, ErrShowSegmentOrder) {
		t.Errorf("want ErrShowSegmentOrder past the Afterparty, got %v", err)
	}
}

// advanceThroughRounds moves show from the Lobby to the matchmaking segment,
// closing each Round it opens along the way so Advance is never rejected (#79).
func advanceThroughRounds(t *testing.T, show *Show) {
	t.Helper()
	for i := 0; i < TotalFixedQuestions; i++ { // Lobby -> Round 0..5
		if err := show.Advance(); err != nil {
			t.Fatalf("advancing into Round %d: %v", i, err)
		}
		if err := show.CloseRound(); err != nil {
			t.Fatalf("closing Round %d: %v", i, err)
		}
	}
	if err := show.Advance(); err != nil { // -> Matchmaking
		t.Fatal(err)
	}
}

func TestShow_EnteringTheRevealRevealsTheMatches(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	show := NewShow(db, time.Now)
	advanceThroughRounds(t, show)
	if db.EventState().IsRevealed() {
		t.Fatal("must not be revealed before the Reveal segment")
	}

	if err := show.Advance(); err != nil { // -> Reveal
		t.Fatal(err)
	}

	if !db.EventState().IsRevealed() {
		t.Error("entering the Reveal segment should reveal the Matches")
	}
}

func TestShow_AdvanceReportsAGenuineRevealFailure(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	show := NewShow(db, time.Now)
	advanceThroughRounds(t, show)
	breakOnFault(t, db, "reveal")

	if err := show.Advance(); err == nil { // -> Reveal
		t.Error("want a genuine failure revealing the Matches")
	}
	if got := show.State().Segment; got != SegmentMatchmaking {
		t.Errorf("a failed Reveal must not have advanced the segment, got %q", got)
	}
}

// TestShow_SetShowStateReportsAFailure and
// TestShow_SetShowStateReportsAGenuineFailure cover setShowState's own
// guard and its Exec, the write behind every Show transition (Advance,
// CloseRound, SkipToNextPair).
func TestShow_SetShowStateReportsAFailure(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	show := NewShow(db, time.Now)
	failWrites(t, db, "show_state")

	if err := show.Advance(); err == nil {
		t.Error("want the failure reported")
	}
}

func TestShow_SetShowStateReportsAGenuineFailure(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	show := NewShow(db, time.Now)
	breakOnFault(t, db, "show_state")

	if err := show.Advance(); err == nil {
		t.Error("want a genuine failure reported")
	}
}

// TestShow_SubmitRoundAnswerReportsAFailure covers the write behind
// SubmitRoundAnswer: the Participant store's own "answers" tag.
func TestShow_SubmitRoundAnswerReportsAFailure(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	db.CreateParticipant("p", "p", "P", true)
	show := NewShow(db, time.Now)
	if err := show.Advance(); err != nil { // -> Round 0
		t.Fatal(err)
	}
	failWrites(t, db, "answers")

	if err := show.SubmitRoundAnswer("p", "Tabs"); err == nil {
		t.Error("want the failure reported")
	}
}

func TestShow_StateSurvivesReopeningTheDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := NewDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	show := NewShow(db, time.Now)
	if err := show.Advance(); err != nil { // -> Round 0
		t.Fatal(err)
	}
	if err := show.CloseRound(); err != nil {
		t.Fatal(err)
	}
	if err := show.Advance(); err != nil { // -> Round 1
		t.Fatal(err)
	}
	db.Close()

	reopened, err := NewDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got := reopened.ShowState(); got.Segment != SegmentRound || got.Round != 1 {
		t.Errorf("want to resume at Round 1, got %+v", got)
	}
}

func TestReset_ReturnsTheShowToTheLobby(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	show := NewShow(db, time.Now)
	if err := show.Advance(); err != nil { // -> Round 0
		t.Fatal(err)
	}

	if err := db.Reset(); err != nil {
		t.Fatal(err)
	}

	if got := show.State(); got.Segment != SegmentLobby {
		t.Errorf("want the Lobby after Reset, got %+v", got)
	}
}

func TestNextActionLabel(t *testing.T) {
	cases := []struct {
		state ShowState
		want  string
	}{
		{ShowState{Segment: SegmentLobby}, "Start the show"},
		{ShowState{Segment: SegmentRound, Round: 0}, "Open Round 2"},
		{ShowState{Segment: SegmentRound, Round: TotalFixedQuestions - 1}, "Start matchmaking"},
		{ShowState{Segment: SegmentMatchmaking}, "Start the Reveal"},
		{ShowState{Segment: SegmentReveal}, ""}, // #83: plays on its own, or with SkipToNextPair, never Advance
		{ShowState{Segment: SegmentAfterparty}, ""},
	}
	for _, c := range cases {
		t.Run(string(c.state.Segment), func(t *testing.T) {
			if got := NextActionLabel(c.state); got != c.want {
				t.Errorf("NextActionLabel(%+v): want %q, got %q", c.state, c.want, got)
			}
		})
	}
}

// TestShow_SubmitRoundAnswer covers #79: an answer is only accepted while
// the Round is open, is stored against the Fixed Question, is changeable,
// and is reflected in RoundAnswerCount.
func TestShow_SubmitRoundAnswer(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateParticipant("p", "p", "P", true); err != nil {
		t.Fatal(err)
	}
	show := NewShow(db, time.Now)

	if err := show.SubmitRoundAnswer("p", "Tabs"); !errors.Is(err, ErrRoundNotOpen) {
		t.Errorf("want ErrRoundNotOpen in the Lobby, got %v", err)
	}
	if err := show.Advance(); err != nil { // -> Round 0
		t.Fatal(err)
	}
	if got := show.RoundAnswerCount(); got != 0 {
		t.Errorf("want 0 answers before anyone answers, got %d", got)
	}

	if err := show.SubmitRoundAnswer("p", "Tabs"); err != nil {
		t.Fatal(err)
	}
	if got := show.RoundAnswerCount(); got != 1 {
		t.Errorf("want 1 answer, got %d", got)
	}
	p, err := NewParticipantStore(db).Get("p")
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Answers[FixedQuestions[0].ID]; got != "Tabs" {
		t.Errorf("want the answer recorded against the Fixed Question, got %q", got)
	}

	if err := show.SubmitRoundAnswer("p", "Spaces"); err != nil {
		t.Fatal(err)
	}
	if p, err = NewParticipantStore(db).Get("p"); err != nil {
		t.Fatal(err)
	}
	if got := p.Answers[FixedQuestions[0].ID]; got != "Spaces" {
		t.Errorf("want the answer changeable while the Round is open, got %q", got)
	}

	if err := show.CloseRound(); err != nil {
		t.Fatal(err)
	}
	if err := show.SubmitRoundAnswer("p", "Whatever the linter says"); !errors.Is(err, ErrRoundNotOpen) {
		t.Errorf("want ErrRoundNotOpen once the Round has closed, got %v", err)
	}
}

func TestShow_SubmitRoundAnswerInactiveInTheClassicFlow(t *testing.T) {
	db := newTestDB(t)
	show := NewShow(db, time.Now)

	if err := show.SubmitRoundAnswer("p", "Tabs"); !errors.Is(err, ErrShowInactive) {
		t.Errorf("want ErrShowInactive, got %v", err)
	}
}

func TestShow_SubmitRoundAnswerReportsAnUnknownParticipant(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	show := NewShow(db, time.Now)
	if err := show.Advance(); err != nil { // -> Round 0
		t.Fatal(err)
	}

	if err := show.SubmitRoundAnswer("nobody", "Tabs"); err == nil {
		t.Error("want an error for an unknown Participant")
	}
}

func TestShow_RoundAnswerCountIsZeroOutsideARound(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	show := NewShow(db, time.Now)

	if got := show.RoundAnswerCount(); got != 0 {
		t.Errorf("want 0 in the Lobby, got %d", got)
	}
}

// TestShow_RoundSecondsLeft covers #79: the countdown is 0 outside an open
// Round, ceiling-rounded while counting down, and 0 once the deadline passes
// — all driven by the injected clock, never a real wait.
func TestShow_RoundSecondsLeft(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	clock := newFakeNow()
	show := NewShow(db, clock.Now)

	if got := show.RoundSecondsLeft(); got != 0 {
		t.Errorf("want 0 in the Lobby, got %d", got)
	}

	if err := show.Advance(); err != nil { // -> Round 0, 15s countdown
		t.Fatal(err)
	}
	if got := show.RoundSecondsLeft(); got != 15 {
		t.Errorf("want 15 seconds left right after opening, got %d", got)
	}

	clock.Advance(10 * time.Second)
	if got := show.RoundSecondsLeft(); got != 5 {
		t.Errorf("want 5 seconds left after 10s pass, got %d", got)
	}

	clock.Advance(10 * time.Second) // past the deadline
	if got := show.RoundSecondsLeft(); got != 0 {
		t.Errorf("want 0 once the deadline has passed, got %d", got)
	}
}

// TestShow_RoundCallout covers #80: a majority answer and a minority "hot
// take", but only once the Round has closed, and only when there's an
// actual minority to call out.
func TestShow_RoundCallout(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	show := NewShow(db, time.Now)
	for _, id := range []string{"a", "b", "c"} {
		if err := db.CreateParticipant(id, id, id, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := show.Advance(); err != nil { // -> Round 0: "Tabs or spaces?"
		t.Fatal(err)
	}

	if got := show.RoundCallout(); got != nil {
		t.Errorf("want no callout while the Round is open, got %+v", got)
	}

	for id, answer := range map[string]string{"a": "Tabs", "b": "Tabs", "c": "Spaces"} {
		if err := show.SubmitRoundAnswer(id, answer); err != nil {
			t.Fatal(err)
		}
	}
	if err := show.CloseRound(); err != nil {
		t.Fatal(err)
	}

	got := show.RoundCallout()
	if got == nil {
		t.Fatal("want a callout once the Round has closed")
	}
	if got.MajorityAnswer != "Tabs" || got.MajorityCount != 2 {
		t.Errorf("want majority Tabs (2), got %+v", got)
	}
	if got.MinorityAnswer != "Spaces" || got.MinorityCount != 1 {
		t.Errorf("want minority Spaces (1), got %+v", got)
	}
}

func TestShow_RoundCalloutNilWithoutAMinority(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	show := NewShow(db, time.Now)
	db.CreateParticipant("a", "a", "A", true)
	db.CreateParticipant("b", "b", "B", true)
	if err := show.Advance(); err != nil { // -> Round 0
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if err := show.SubmitRoundAnswer(id, "Tabs"); err != nil {
			t.Fatal(err)
		}
	}
	if err := show.CloseRound(); err != nil {
		t.Fatal(err)
	}

	if got := show.RoundCallout(); got != nil {
		t.Errorf("want no callout when everyone answered the same way, got %+v", got)
	}
}

func TestShow_RoundCalloutNilForAFreeTextRound(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	show := NewShow(db, time.Now)
	db.CreateParticipant("a", "a", "A", true)
	db.CreateParticipant("b", "b", "B", true)
	if err := show.Advance(); err != nil { // -> Round 0
		t.Fatal(err)
	}
	if err := show.CloseRound(); err != nil {
		t.Fatal(err)
	}
	if err := show.Advance(); err != nil { // -> Round 1: "What's your go-to language right now?" (free text)
		t.Fatal(err)
	}
	for id, answer := range map[string]string{"a": "Go", "b": "Rust"} {
		if err := show.SubmitRoundAnswer(id, answer); err != nil {
			t.Fatal(err)
		}
	}
	if err := show.CloseRound(); err != nil {
		t.Fatal(err)
	}

	if got := show.RoundCallout(); got != nil {
		t.Errorf("want no callout for a free-text Round, got %+v", got)
	}
}

// TestShow_RevealPlaysPairByPairOnTheInjectedClock covers #83: the Reveal
// shows one pair at a time, advancing to the next on its own once the
// injected clock reaches its deadline, and lands on the Afterparty once the
// last pair has had its turn.
func TestShow_RevealPlaysPairByPairOnTheInjectedClock(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "c", "d"} {
		if err := db.CreateParticipant(id, id, id, true); err != nil {
			t.Fatal(err)
		}
	}
	pair(t, db, "a", "b")
	pair(t, db, "c", "d")
	clock := newFakeNow()
	show := NewShow(db, clock.Now)
	advanceThroughRounds(t, show)
	if err := show.Advance(); err != nil { // -> Reveal
		t.Fatal(err)
	}

	first, ok := show.CurrentRevealPair()
	if !ok {
		t.Fatal("want a pair on stage at the start of the Reveal")
	}
	if current, total := show.RevealProgress(); current != 1 || total != 2 {
		t.Errorf("want pair 1 of 2, got %d of %d", current, total)
	}

	clock.Advance(DefaultRevealPairDuration)

	second, ok := show.CurrentRevealPair()
	if !ok {
		t.Fatal("want a second pair on stage once the first's time is up")
	}
	if current, _ := show.RevealProgress(); current != 2 {
		t.Errorf("want pair 2, got %d", current)
	}
	if first.A.ID == second.A.ID {
		t.Error("want a different pair the second time")
	}

	clock.Advance(DefaultRevealPairDuration)

	if got := show.State().Segment; got != SegmentAfterparty {
		t.Errorf("want the Afterparty after the last pair's time is up, got %q", got)
	}
	if _, ok := show.CurrentRevealPair(); ok {
		t.Error("want no current pair in the Afterparty")
	}
}

// TestShow_SkipToNextPairAdvancesImmediately covers #83: the Host can move
// the Reveal along without waiting for the clock.
func TestShow_SkipToNextPairAdvancesImmediately(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "c", "d"} {
		if err := db.CreateParticipant(id, id, id, true); err != nil {
			t.Fatal(err)
		}
	}
	pair(t, db, "a", "b")
	pair(t, db, "c", "d")
	show := NewShow(db, time.Now)
	advanceThroughRounds(t, show)
	if err := show.Advance(); err != nil { // -> Reveal
		t.Fatal(err)
	}

	if err := show.SkipToNextPair(); err != nil {
		t.Fatal(err)
	}
	if current, _ := show.RevealProgress(); current != 2 {
		t.Errorf("want pair 2 after one skip, got %d", current)
	}

	if err := show.SkipToNextPair(); err != nil {
		t.Fatal(err)
	}
	if got := show.State().Segment; got != SegmentAfterparty {
		t.Errorf("want the Afterparty after skipping past the last pair, got %q", got)
	}
}

func TestShow_AdvanceRejectedWhileRevealPlaying(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	db.CreateParticipant("a", "a", "A", true)
	db.CreateParticipant("b", "b", "B", true)
	pair(t, db, "a", "b")
	show := NewShow(db, time.Now)
	advanceThroughRounds(t, show)
	if err := show.Advance(); err != nil { // -> Reveal
		t.Fatal(err)
	}

	if err := show.Advance(); !errors.Is(err, ErrRevealStillPlaying) {
		t.Errorf("want ErrRevealStillPlaying, got %v", err)
	}
}

func TestShow_SkipToNextPairRejectedOutsideTheReveal(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	show := NewShow(db, time.Now)

	if err := show.SkipToNextPair(); !errors.Is(err, ErrRevealNotPlaying) {
		t.Errorf("want ErrRevealNotPlaying in the Lobby, got %v", err)
	}
}

func TestShow_SkipToNextPairInactiveInTheClassicFlow(t *testing.T) {
	db := newTestDB(t)
	show := NewShow(db, time.Now)

	if err := show.SkipToNextPair(); !errors.Is(err, ErrShowInactive) {
		t.Errorf("want ErrShowInactive, got %v", err)
	}
}

// TestShow_PairRevealed covers #83: nobody's pair is revealed before its
// turn, and everybody's is at or after it — the rule behind "no handle or
// name before its turn".
func TestShow_PairRevealed(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "c", "d"} {
		if err := db.CreateParticipant(id, id, id, true); err != nil {
			t.Fatal(err)
		}
	}
	pair(t, db, "a", "b")
	pair(t, db, "c", "d")
	clock := newFakeNow()
	show := NewShow(db, clock.Now)

	if !show.PairRevealed("a") {
		t.Error("PairRevealed is only meaningful once EventState.IsRevealed is true; before the Reveal it's vacuously true")
	}

	advanceThroughRounds(t, show)
	if err := show.Advance(); err != nil { // -> Reveal, pair 1: a-b (registration order)
		t.Fatal(err)
	}

	if !show.PairRevealed("a") || !show.PairRevealed("b") {
		t.Error("want a and b revealed at their own turn")
	}
	if show.PairRevealed("c") || show.PairRevealed("d") {
		t.Error("want c and d not yet revealed")
	}

	clock.Advance(DefaultRevealPairDuration)

	if !show.PairRevealed("c") || !show.PairRevealed("d") {
		t.Error("want c and d revealed at their turn")
	}
}

func TestShow_RevealProgressIsZeroOutsideTheReveal(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	show := NewShow(db, time.Now)

	if current, total := show.RevealProgress(); current != 0 || total != 0 {
		t.Errorf("want 0 of 0 in the Lobby, got %d of %d", current, total)
	}
}

// TestShow_CurrentRevealPairFalseWithNoPairsToShow covers the degenerate
// case of entering the Reveal with nobody to reveal (#83).
func TestShow_CurrentRevealPairFalseWithNoPairsToShow(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	show := NewShow(db, time.Now)
	advanceThroughRounds(t, show)
	if err := show.Advance(); err != nil { // -> Reveal, with nobody to reveal
		t.Fatal(err)
	}

	if _, ok := show.CurrentRevealPair(); ok {
		t.Error("want no current pair when there's nobody to reveal")
	}
}

// TestShow_WildcardGetsItsOwnRevealSlot covers #83: the wildcard gets its
// own turn, alone, instead of being skipped.
func TestShow_WildcardGetsItsOwnRevealSlot(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	db.CreateParticipant("a", "a", "A", true)
	db.CreateParticipant("b", "b", "B", true)
	db.CreateParticipant("w", "w", "W", true)
	pair(t, db, "a", "b")
	if err := NewParticipantStore(db).Change(ParticipantChange{ID: "w", Wildcard: boolPtr(true)}); err != nil {
		t.Fatal(err)
	}
	show := NewShow(db, time.Now)
	advanceThroughRounds(t, show)
	if err := show.Advance(); err != nil { // -> Reveal
		t.Fatal(err)
	}

	if err := show.SkipToNextPair(); err != nil { // a-b comes first (registered before w)
		t.Fatal(err)
	}
	second, ok := show.CurrentRevealPair()
	if !ok {
		t.Fatal("want the wildcard's own slot")
	}
	if !second.Wildcard || second.B != nil || second.A.ID != "w" {
		t.Errorf("want the wildcard alone in their own slot, got %+v", second)
	}
	if !show.PairRevealed("w") {
		t.Error("want the wildcard revealed at their turn")
	}
}

func TestShowState_DefaultsToLobbyForAnUnreadableStoredValue(t *testing.T) {
	db := newTestDB(t)
	db.db.Exec(`INSERT OR REPLACE INTO event_state (key, value) VALUES (?, ?)`, showStateKey, "not json")

	if got := db.ShowState(); got.Segment != SegmentLobby {
		t.Errorf("want the Lobby for an unreadable stored value, got %+v", got)
	}
}
