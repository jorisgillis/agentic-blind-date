package main

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestShow_InactiveInTheClassicFlow(t *testing.T) {
	db := newTestDB(t)
	show := NewShow(db)

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
	show := NewShow(db)

	if got := show.State(); got.Segment != SegmentLobby {
		t.Errorf("want the Lobby, got %+v", got)
	}
}

// TestShow_SegmentsAdvanceInOrder covers #78: one call to Advance moves the
// show exactly one segment forward, through every Round in turn, into
// matchmaking, the Reveal and the Afterparty — where it stops.
func TestShow_SegmentsAdvanceInOrder(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	show := NewShow(db)

	want := []ShowState{
		{Segment: SegmentRound, Round: 0},
		{Segment: SegmentRound, Round: 1},
		{Segment: SegmentRound, Round: 2},
		{Segment: SegmentRound, Round: 3},
		{Segment: SegmentRound, Round: 4},
		{Segment: SegmentRound, Round: 5},
		{Segment: SegmentMatchmaking},
		{Segment: SegmentReveal},
		{Segment: SegmentAfterparty},
	}
	for i, w := range want {
		if err := show.Advance(); err != nil {
			t.Fatalf("advance %d: %v", i, err)
		}
		if got := show.State(); got != w {
			t.Fatalf("advance %d: want %+v, got %+v", i, w, got)
		}
	}

	if err := show.Advance(); !errors.Is(err, ErrShowSegmentOrder) {
		t.Errorf("want ErrShowSegmentOrder past the Afterparty, got %v", err)
	}
}

func TestShow_EnteringTheRevealRevealsTheMatches(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	show := NewShow(db)
	for i := 0; i < 7; i++ { // Lobby -> Round 0..5 -> Matchmaking
		if err := show.Advance(); err != nil {
			t.Fatal(err)
		}
	}
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
	show := NewShow(db)
	for i := 0; i < 7; i++ { // Lobby -> Round 0..5 -> Matchmaking
		if err := show.Advance(); err != nil {
			t.Fatal(err)
		}
	}
	breakOnFault(t, db, "reveal")

	if err := show.Advance(); err == nil { // -> Reveal
		t.Error("want a genuine failure revealing the Matches")
	}
	if got := show.State().Segment; got != SegmentMatchmaking {
		t.Errorf("a failed Reveal must not have advanced the segment, got %q", got)
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
	show := NewShow(db)
	if err := show.Advance(); err != nil { // -> Round 0
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
	show := NewShow(db)
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
		{ShowState{Segment: SegmentReveal}, "Move to the Afterparty"},
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

func TestShowState_DefaultsToLobbyForAnUnreadableStoredValue(t *testing.T) {
	db := newTestDB(t)
	db.db.Exec(`INSERT OR REPLACE INTO event_state (key, value) VALUES (?, ?)`, showStateKey, "not json")

	if got := db.ShowState(); got.Segment != SegmentLobby {
		t.Errorf("want the Lobby for an unreadable stored value, got %+v", got)
	}
}
