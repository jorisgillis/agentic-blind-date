package main

import "testing"

func TestEventAdmin_Reveal(t *testing.T) {
	db := newTestDB(t)
	admin := NewEventAdmin(db, NewMatcher(db, newFakeGitHub(), newFakeLLM()), NewMatchmaking(db, NewMatcher(db, newFakeGitHub(), newFakeLLM()), NewRelationships(db)))

	if err := admin.Reveal(); err != nil {
		t.Fatal(err)
	}

	if !db.EventState().IsRevealed() {
		t.Error("Event State should be revealed")
	}
	lines, _ := db.GetRecentActivity(1)
	if len(lines) != 1 || lines[0] != "🎉 The matches are revealed!" {
		t.Errorf("Reveal should announce itself on the ticker, got %v", lines)
	}
}

func TestEventAdmin_ResetClearsParticipantsActivityAndTheLLMCache(t *testing.T) {
	db := newTestDB(t)
	llm := newFakeLLM().on("matchmaker", matchReply)
	matcher := NewMatcher(db, newFakeGitHub(), llm)
	admin := NewEventAdmin(db, matcher, NewMatchmaking(db, matcher, NewRelationships(db)))
	db.CreateParticipant("p", "p", "P", true)
	matcher.ScorePair(testParticipant("a", "A", []string{"Go"}), testParticipant("b", "B", []string{"Go"})) // populate the cache
	db.Reveal()

	if err := admin.Reset(); err != nil {
		t.Fatal(err)
	}

	if db.ParticipantCount() != 0 {
		t.Error("Reset should clear Participants")
	}
	if db.EventState().IsRevealed() {
		t.Error("Reset should return to before the Reveal")
	}
	if _, ok := db.GetLLMCache(pairKey(testParticipant("a", "", []string{"Go"}), testParticipant("b", "", []string{"Go"}))); ok {
		t.Error("Reset should clear the LLM cache")
	}
}

// TestEventAdmin_ResetCannotInterleaveWithContinuousMatching covers #57: a
// Reset running at the same moment as Continuous Matching must not corrupt
// state — Matchmaking's lock serializes them.
func TestEventAdmin_ResetCannotInterleaveWithContinuousMatching(t *testing.T) {
	llm := newFakeLLM().on("matchmaker", `{"score": 70, "reason": "fine"}`)
	db := newTestDB(t)
	matcher := NewMatcher(db, newFakeGitHub(), llm)
	relations := NewRelationships(db)
	mm := NewMatchmaking(db, matcher, relations)
	admin := NewEventAdmin(db, matcher, mm)
	for _, id := range []string{"a", "b", "n"} {
		db.CreateParticipant(id, id, id, true)
		forceStep(db, id, StepReady)
	}
	newcomer := reload(t, db, "n") // read before either goroutine starts: Reset may delete "n" mid-race

	done := make(chan error, 2)
	go func() { done <- admin.Reset() }()
	go func() { done <- mm.MatchNewcomer(newcomer) }()
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}

	// Whichever ran first, the outcome must be one consistent state, not a
	// mix: either the Reset cleared everyone, or the newcomer got matched.
	assertInvariant(t, db)
}

func TestEventAdmin_RematchHandleReportsCompletion(t *testing.T) {
	llm := newFakeLLM().on("matchmaker", `{"score": 70, "reason": "fine"}`)
	db := newTestDB(t)
	matcher := NewMatcher(db, newFakeGitHub(), llm)
	mm := NewMatchmaking(db, matcher, NewRelationships(db))
	admin := NewEventAdmin(db, matcher, mm)
	seed(t, db, "a", "A", "ready")
	seed(t, db, "b", "B", "ready")

	if err := admin.Rematch().Wait(); err != nil {
		t.Fatal(err)
	}

	if reload(t, db, "a").MatchedWith != "b" {
		t.Error("Rematch should have paired a and b")
	}
}

func TestEventAdmin_RematchHandleReportsFailure(t *testing.T) {
	db := newTestDB(t)
	matcher := NewMatcher(db, newFakeGitHub(), newFakeLLM())
	mm := NewMatchmaking(db, matcher, NewRelationships(db))
	admin := NewEventAdmin(db, matcher, mm)
	seed(t, db, "a", "A", "ready") // only one ready Participant: Rematch fails

	if err := admin.Rematch().Wait(); err == nil {
		t.Error("want an error")
	}
	lines, _ := db.GetRecentActivity(1)
	if len(lines) == 0 || lines[0] == "🔄 Admin triggered full rematch" {
		t.Errorf("the failure should reach the ticker too: %v", lines)
	}
}
