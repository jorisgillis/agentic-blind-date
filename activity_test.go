package main

import (
	"strings"
	"testing"
)

func TestActivity_MatchedAnnouncesTheScoreOnlyWhenGenuine(t *testing.T) {
	db := newTestDB(t)
	activity := NewActivity(db)

	activity.Matched("The Gopher", "The Crab", 87, true)
	activity.Matched("The Gopher", "The Otter", 42, false)

	lines, err := db.GetRecentActivity(2)
	if err != nil {
		t.Fatal(err)
	}
	if lines[0] != "💘 The Gopher ↔ The Otter" {
		t.Errorf("a failed assessment must not announce a score (#54): got %q", lines[0])
	}
	if lines[1] != "💘 The Gopher ↔ The Crab (87%)" {
		t.Errorf("a genuine assessment announces its score: got %q", lines[1])
	}
}

// TestMatchmaking_AFailedAssessmentIsNotAnnouncedAsARealScore covers #54
// end-to-end: when the LLM call for a stored Match fails, the fallback
// assessment's score must not reach the activity ticker as if it were real.
func TestMatchmaking_AFailedAssessmentIsNotAnnouncedAsARealScore(t *testing.T) {
	db := newTestDB(t)
	llm := newFakeLLM().onErr("matchmaker", fakeError("mistral HTTP 429"))
	mm := NewMatchmaking(db, NewMatcher(db, newFakeGitHub(), llm), NewRelationships(db))
	db.CreateParticipant("a", "a", "A", true)
	db.CreateParticipant("b", "b", "B", true)
	forceStep(db, "a", StepReady)
	forceStep(db, "b", StepReady)

	if err := mm.Rematch(); err != nil {
		t.Fatal(err)
	}

	lines, err := db.GetRecentActivity(10)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range lines {
		if strings.Contains(line, "42%") {
			t.Errorf("the default fallback score must never be announced as real: %q", line)
		}
	}
}
