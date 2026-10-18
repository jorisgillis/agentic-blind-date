package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file covers #148: the Evening module owns the host's two semantic
// operations — advancing the Phase and resetting the event — and every
// consequence they fire, so the sequencing is testable without HTTP.

// newEvening wires the Evening module on a fresh test database the way
// NewHandler wires it in production. The slides directory is returned so a
// test can leave a stray file in it for the reset's sweep.
func newEvening(t *testing.T, gh *fakeGitHub) (*Evening, *DB, *Matcher, *Missions, string) {
	t.Helper()
	if gh == nil {
		gh = newFakeGitHub()
	}
	db := newTestDB(t)
	llm := missionLLM()
	matcher := NewMatcher(db, gh, llm)
	missions := NewMissions(db, matcher, llm)
	meetings := NewMeetings(db, NewEventStore(db))
	dir := t.TempDir()
	return NewEvening(db, missions, meetings, NewSlideDecks(db, dir), matcher), db, matcher, missions, dir
}

func TestEvening_ClosingTheEventNamesTheChampion(t *testing.T) {
	evening, db, _, _, _ := newEvening(t, nil)
	seed(t, db, "a", "The Gopher", "ready")
	seed(t, db, "b", "The Crab", "ready")
	events := NewEventStore(db)
	if err := events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	completeMeeting(t, db, NewMeetings(db, events), "a", "b")

	if err := evening.Advance(PhaseClosed); err != nil {
		t.Fatalf("closing the event: %v", err)
	}

	msgs, err := db.GetRecentActivity(10)
	if err != nil {
		t.Fatal(err)
	}
	logged := strings.Join(msgs, "\n")
	if !strings.Contains(logged, PhaseClosed.Activity()) {
		t.Errorf("the close logs the phase's ticker line, got %v", msgs)
	}
	if !strings.Contains(logged, "The Gopher") || !strings.Contains(logged, "takes the Merge Trophy with 1 connection!") {
		t.Errorf("the close names the champion on the ticker, got %v", msgs)
	}
}

func TestEvening_TheSocialTransitionLogsTheTickerBeforeDealingCards(t *testing.T) {
	evening, db, _, _, _ := newEvening(t, nil)
	seed(t, db, "a", "The Gopher", "ready")
	seed(t, db, "b", "The Crab", "ready")
	seed(t, db, "c", "The Hermit", "ready")

	if err := evening.Advance(PhaseSocial); err != nil {
		t.Fatalf("advancing to the social: %v", err)
	}

	// The ticker line is logged before the mission cards are generated: the
	// background worker pool is still dealing, but the line is already there.
	msgs, err := db.GetRecentActivity(10)
	if err != nil {
		t.Fatal(err)
	}
	if logged := strings.Join(msgs, "\n"); !strings.Contains(logged, PhaseSocial.Activity()) {
		t.Errorf("the social transition logs the phase's ticker line, got %v", msgs)
	}
	eventually(t, "a card for every ready participant", func() bool {
		return cardCount(t, db) == 3
	})
}

func TestEvening_ResetClearsTheMatcherCache(t *testing.T) {
	gh := newFakeGitHub().withFollow("a", "b")
	evening, db, matcher, _, dir := newEvening(t, gh)
	a := testParticipant("a", "The Gopher", []string{"Go"})
	b := testParticipant("b", "The Crab", []string{"Rust"})

	matcher.PairScore(a, b)
	if n := gh.followLookups(); n != 1 {
		t.Fatalf("warming the follow cache: want 1 lookup, got %d", n)
	}
	// A stray slide file: the Slide Deck rows went with the reset; their
	// files go with the sweep.
	stray := filepath.Join(dir, "speaker-slides.pdf")
	if err := os.WriteFile(stray, []byte("%PDF"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := evening.Reset(); err != nil {
		t.Fatalf("reset: %v", err)
	}

	matcher.PairScore(a, b)
	if n := gh.followLookups(); n != 2 {
		t.Errorf("a reset clears the matcher cache: want 2 lookups, got %d", n)
	}
	if _, err := os.Stat(stray); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a reset sweeps the slide files away, got %v", err)
	}
	if got := NewEventStore(db).State(); got != PhaseOnboarding {
		t.Errorf("a reset returns the event to onboarding, got %q", got)
	}
}

func TestEvening_ASamePhaseAdvanceFiresNothing(t *testing.T) {
	evening, db, _, missions, _ := newEvening(t, nil)
	seed(t, db, "a", "The Gopher", "ready")
	seed(t, db, "b", "The Crab", "ready")
	seed(t, db, "c", "The Hermit", "ready")

	if err := evening.Advance(PhaseOnboarding); err != nil {
		t.Fatalf("a same-phase advance: %v", err)
	}
	if got := NewEventStore(db).State(); got != PhaseOnboarding {
		t.Errorf("a same-phase advance must not change the phase, got %q", got)
	}
	if msgs, _ := db.GetRecentActivity(10); len(msgs) != 0 {
		t.Errorf("a same-phase advance logs nothing, got %v", msgs)
	}
	if missions.Has("a") {
		t.Error("a same-phase advance must not generate mission cards")
	}

	// From the social, advancing to the social again is the same no-op:
	// the ticker keeps what the real transition logged and no second round
	// of cards is dealt.
	if err := evening.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the dealt line settles", func() bool {
		msgs, _ := db.GetRecentActivity(10)
		return strings.Contains(strings.Join(msgs, "\n"), "The Mission Agent dealt 3 mission cards")
	})
	before, err := db.GetRecentActivity(10)
	if err != nil {
		t.Fatal(err)
	}
	if err := evening.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	after, err := db.GetRecentActivity(10)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Errorf("a same-phase advance fires nothing, want %v, got %v", before, after)
	}
}
