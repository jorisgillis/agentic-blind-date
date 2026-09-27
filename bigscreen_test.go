package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBigScreen_SnapshotIsCachedUntilSomethingChanges(t *testing.T) {
	db := newTestDB(t)
	bs := NewBigScreen(db, NewMatcher(db, newFakeGitHub(), newFakeLLM()), NewShow(db, time.Now))

	first := bs.Snapshot()
	second := bs.Snapshot()
	if first != second {
		t.Error("nothing changed: Snapshot should return the cached instance, not rebuild")
	}

	db.CreateParticipant("p", "p", "P", true)
	third := bs.Snapshot()
	if third == second {
		t.Error("a change should invalidate the cache")
	}
	if third.Count != 1 {
		t.Errorf("Count: want 1, got %d", third.Count)
	}
}

func TestBigScreen_EachMatchListedOnceWithBothPersonas(t *testing.T) {
	db := newTestDB(t)
	seed(t, db, "a", "The Gopher", "ready")
	seed(t, db, "b", "The Crab", "ready")
	pair(t, db, "a", "b")
	bs := NewBigScreen(db, NewMatcher(db, newFakeGitHub(), newFakeLLM()), NewShow(db, time.Now))

	snap := bs.Snapshot()

	if len(snap.Matches) != 1 {
		t.Fatalf("want exactly one Match, got %d: %+v", len(snap.Matches), snap.Matches)
	}
	m := snap.Matches[0]
	names := map[string]bool{m.A.Name: true, m.B.Name: true}
	if !names["The Gopher"] || !names["The Crab"] {
		t.Errorf("both Participants' own Persona should appear: %+v", m)
	}
	if m.Score != 91 {
		t.Errorf("score: want 91, got %d", m.Score)
	}
}

func TestBigScreen_MatchIdentityHiddenUntilTheReveal(t *testing.T) {
	db := newTestDB(t)
	seed(t, db, "a", "The Gopher", "ready")
	seed(t, db, "b", "The Crab", "ready")
	pair(t, db, "a", "b")
	bs := NewBigScreen(db, NewMatcher(db, newFakeGitHub(), newFakeLLM()), NewShow(db, time.Now))

	before := bs.Snapshot().Matches[0]
	if before.A.Handle != "" || before.B.Handle != "" {
		t.Errorf("before the Reveal, a Match must not carry a handle: %+v", before)
	}

	db.Reveal()
	after := bs.Snapshot().Matches[0]
	if after.A.Handle == "" || after.B.Handle == "" {
		t.Errorf("after the Reveal, a Match should identify both Participants: %+v", after)
	}
}

// TestBigScreen_PayloadKeysUseEventStateVocabulary covers #55: the JSON
// payload uses Event State vocabulary instead of "phase"/"phase_label".
func TestBigScreen_PayloadKeysUseEventStateVocabulary(t *testing.T) {
	db := newTestDB(t)
	bs := NewBigScreen(db, NewMatcher(db, newFakeGitHub(), newFakeLLM()), NewShow(db, time.Now))

	data, err := json.Marshal(bs.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["phase"]; ok {
		t.Error(`payload still has a "phase" key`)
	}
	if _, ok := fields["phase_label"]; ok {
		t.Error(`payload still has a "phase_label" key`)
	}
	if _, ok := fields["event_state"]; !ok {
		t.Error(`payload should have an "event_state" key`)
	}
	if _, ok := fields["label"]; !ok {
		t.Error(`payload should have a "label" key`)
	}
}
