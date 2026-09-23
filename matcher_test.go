package main

import (
	"strings"
	"testing"
)

func TestDefaultMatchResult(t *testing.T) {
	result := defaultMatchResult()

	if result.Score != 42 {
		t.Errorf("expected score 42, got %d", result.Score)
	}
	if result.Reason == "" {
		t.Error("expected non-empty reason")
	}
	if len(result.RedFlags) != 0 {
		t.Errorf("expected 0 red flags, got %d", len(result.RedFlags))
	}
	if len(result.GreenFlags) != 1 {
		t.Errorf("expected 1 green flag, got %d", len(result.GreenFlags))
	}
	if len(result.Icebreakers) != 3 {
		t.Errorf("expected 3 icebreakers, got %d", len(result.Icebreakers))
	}
}

const matchReply = `{"score": 87, "reason": "Both refuse to use tabs", "red_flags": ["argues about vim, emacs, and nano"], "green_flags": [], "icebreakers": ["Why Go?", "Monorepo, yes or no?"]}`

func dev(id, persona string, langs ...string) *Participant {
	anyLangs := make([]any, len(langs))
	for i, l := range langs {
		anyLangs[i] = l
	}
	return &Participant{
		ID: id, GitHubHandle: id, PersonaName: persona,
		Profile:   &GitHubProfile{Login: id, Languages: langs},
		Answers:   map[string]string{"fixed_0": "Tabs"},
		Interests: map[string]interface{}{"languages": anyLangs}, // as decoded from the database
	}
}

func TestMatcherScorePair_ScoresAPairOnceInEitherOrder(t *testing.T) {
	llm := newFakeLLM().on("matchmaker", matchReply)
	m := NewMatcher(newTestDB(t), newFakeGitHub(), llm)
	a, b := dev("a", "The Gopher", "Go"), dev("b", "The Crab", "Rust")

	first, err := m.ScorePair(a, b)
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.ScorePair(b, a)
	if err != nil {
		t.Fatal(err)
	}

	if n := llm.callsMatching("matchmaker"); n != 1 {
		t.Errorf("LLM calls: want 1, got %d", n)
	}
	if first.Score != 87 || second.Score != 87 {
		t.Errorf("scores: got %d and %d", first.Score, second.Score)
	}
}

func TestMatcherScorePair_CachedResultSurvivesARestartExactly(t *testing.T) {
	db := newTestDB(t)
	llm := newFakeLLM().on("matchmaker", matchReply)
	a, b := dev("a", "The Gopher", "Go"), dev("b", "The Crab", "Rust")
	if _, err := NewMatcher(db, newFakeGitHub(), llm).ScorePair(a, b); err != nil {
		t.Fatal(err)
	}

	restarted := NewMatcher(db, newFakeGitHub(), llm)
	got, err := restarted.ScorePair(a, b)
	if err != nil {
		t.Fatal(err)
	}

	if n := llm.callsMatching("matchmaker"); n != 1 {
		t.Errorf("a restarted Matcher should reuse the persistent cache; LLM calls: %d", n)
	}
	if len(got.RedFlags) != 1 || got.RedFlags[0] != "argues about vim, emacs, and nano" {
		t.Errorf("red flags must round-trip exactly, got %q", got.RedFlags)
	}
	if got.GreenFlags == nil || len(got.GreenFlags) != 0 {
		t.Errorf("empty green flags must stay empty, got %q", got.GreenFlags)
	}
	if len(got.Icebreakers) != 2 || got.Icebreakers[1] != "Monorepo, yes or no?" {
		t.Errorf("icebreakers: got %q", got.Icebreakers)
	}
}

func TestMatcherScorePair_FailuresAreNotCached(t *testing.T) {
	llm := newFakeLLM().onErr("matchmaker", fakeError("mistral HTTP 429"))
	m := NewMatcher(newTestDB(t), newFakeGitHub(), llm)
	a, b := dev("a", "The Gopher", "Go"), dev("b", "The Crab", "Rust")

	if _, err := m.ScorePair(a, b); err == nil {
		t.Fatal("want error")
	}
	m.ScorePair(a, b)

	if n := llm.callsMatching("matchmaker"); n != 2 {
		t.Errorf("a failed scoring must be retried next time; LLM calls: %d", n)
	}
}

func TestMatcherScorePair_PromptDescribesEachDeveloperWithTheirOwnInterestsAndFollows(t *testing.T) {
	llm := newFakeLLM().on("matchmaker", matchReply)
	gh := newFakeGitHub().withFollow("a", "b")
	m := NewMatcher(newTestDB(t), gh, llm)

	m.ScorePair(dev("a", "The Gopher", "Go"), dev("b", "The Crab", "Rust"))

	call, _ := llm.lastCallMatching("matchmaker")
	dev1, dev2, found := strings.Cut(call.User, "DEVELOPER 2")
	if !found {
		t.Fatalf("prompt has no DEVELOPER 2 section:\n%s", call.User)
	}
	if !strings.Contains(dev1, "Interests: languages: Go") || strings.Contains(dev1, "Rust") {
		t.Errorf("DEVELOPER 1 section should hold only their own interests:\n%s", dev1)
	}
	if !strings.Contains(dev2, "Interests: languages: Rust") {
		t.Errorf("DEVELOPER 2 section should hold their own interests:\n%s", dev2)
	}
	if !strings.Contains(call.User, "The Gopher already follows The Crab on GitHub.") {
		t.Errorf("prompt should mention the follow relationship:\n%s", call.User)
	}
}

func TestMatcherScorePair_ClampsOutOfRangeScores(t *testing.T) {
	for reply, want := range map[string]int{
		`{"score": 140, "reason": "Too good"}`: 100,
		`{"score": -5, "reason": "Oof"}`:       0,
	} {
		llm := newFakeLLM().on("matchmaker", reply)
		m := NewMatcher(newTestDB(t), newFakeGitHub(), llm)

		got, err := m.ScorePair(dev("a", "A"), dev("b", "B"))

		if err != nil || got.Score != want {
			t.Errorf("%s: want score %d, got %+v (err %v)", reply, want, got, err)
		}
	}
}

func TestMatcherScorePair_RejectsRepliesWithoutAScore(t *testing.T) {
	llm := newFakeLLM().on("matchmaker", `{"reason": "forgot the number"}`)
	m := NewMatcher(newTestDB(t), newFakeGitHub(), llm)

	if _, err := m.ScorePair(dev("a", "A"), dev("b", "B")); err == nil {
		t.Error("a reply without a score should be an error")
	}
}
