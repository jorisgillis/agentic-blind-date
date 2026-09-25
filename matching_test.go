package main

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPairKey(t *testing.T) {
	a := &Participant{ID: "aaa"}
	b := &Participant{ID: "bbb"}

	k1 := pairKey(a, b)
	k2 := pairKey(b, a)

	if k1 != k2 {
		t.Errorf("pairKey not symmetric: %q vs %q", k1, k2)
	}
	if k1 != "aaa:bbb" {
		t.Errorf("expected smaller ID first, got %q", k1)
	}
}

func TestPairScore_languages(t *testing.T) {
	matcher := &Matcher{}
	a := testParticipant("a", "The a", []string{"Go", "Python"})
	b := testParticipant("b", "The b", []string{"Go", "Rust"})

	score := matcher.PairScore(a, b)
	if score != 3 {
		t.Errorf("expected 3 (one shared language), got %d", score)
	}

	c := testParticipant("c", "The c", []string{"Go", "Python"})
	score2 := matcher.PairScore(a, c)
	if score2 != 6 {
		t.Errorf("expected 6 (two shared languages), got %d", score2)
	}
}

func TestPairScore_answers(t *testing.T) {
	matcher := &Matcher{}
	a := testParticipant("a", "The a", nil, participantOpts{answers: map[string]string{"0": "Tabs", "1": "Go"}})
	b := testParticipant("b", "The b", nil, participantOpts{answers: map[string]string{"0": "Tabs", "1": "Python"}})

	score := matcher.PairScore(a, b)
	if score != 1 {
		t.Errorf("expected 1 (one matching answer), got %d", score)
	}
}

func TestPairScore_combined(t *testing.T) {
	matcher := &Matcher{}
	a := testParticipant("a", "The a", []string{"Go"}, participantOpts{answers: map[string]string{"0": "Tabs"}})
	b := testParticipant("b", "The b", []string{"Go"}, participantOpts{answers: map[string]string{"0": "Tabs"}})

	score := matcher.PairScore(a, b)
	if score != 4 {
		t.Errorf("expected 4 (3 language + 1 answer), got %d", score)
	}
}

func TestPairScore_noOverlap(t *testing.T) {
	matcher := &Matcher{}
	a := testParticipant("a", "The a", []string{"Go"}, participantOpts{answers: map[string]string{"0": "Tabs"}})
	b := testParticipant("b", "The b", []string{"Rust"}, participantOpts{answers: map[string]string{"0": "Spaces"}})

	if score := matcher.PairScore(a, b); score != 0 {
		t.Errorf("expected 0, got %d", score)
	}
}

// Note: Follow relationship scoring requires GitHub API access and is deferred for a follow-up
// func TestPairScore_followRelationships

func TestPairScore_topics(t *testing.T) {
	matcher := &Matcher{}
	a := testParticipant("a", "The a", []string{"Go"}, participantOpts{topics: []string{"web", "api"}})
	b := testParticipant("b", "The b", []string{"Python"}, participantOpts{topics: []string{"web", "data"}})

	score := matcher.PairScore(a, b)
	expected := 2 // 1 shared topic (web) * 2 points
	if score != expected {
		t.Errorf("expected %d (one shared topic), got %d", expected, score)
	}

	c := testParticipant("c", "The c", []string{"Rust"}, participantOpts{topics: []string{"web", "api"}})
	score2 := matcher.PairScore(a, c)
	expected2 := 4 // 2 shared topics * 2 points
	if score2 != expected2 {
		t.Errorf("expected %d (two shared topics), got %d", expected2, score2)
	}
}

func TestPairScore_projectTypes(t *testing.T) {
	matcher := &Matcher{}
	a := testParticipant("a", "The a", []string{"Go"}, participantOpts{projectType: "Web"})
	b := testParticipant("b", "The b", []string{"Python"}, participantOpts{projectType: "Web"})

	score := matcher.PairScore(a, b)
	expected := 2 // shared project type
	if score != expected {
		t.Errorf("expected %d (shared project type), got %d", expected, score)
	}

	c := testParticipant("c", "The c", []string{"Rust"}, participantOpts{projectType: "Backend"})
	score2 := matcher.PairScore(a, c)
	if score2 != 0 {
		t.Errorf("expected 0 (different project types), got %d", score2)
	}
}

func TestPairScore_devEnvironments(t *testing.T) {
	matcher := &Matcher{}
	a := testParticipant("a", "The a", []string{"Go"}, participantOpts{devEnv: []string{"IDE", "VIM"}})
	b := testParticipant("b", "The b", []string{"Python"}, participantOpts{devEnv: []string{"IDE", "Cloud"}})

	score := matcher.PairScore(a, b)
	expected := 1 // 1 shared dev environment
	if score != expected {
		t.Errorf("expected %d (one shared dev env), got %d", expected, score)
	}

	c := testParticipant("c", "The c", []string{"Rust"}, participantOpts{devEnv: []string{"IDE", "VIM"}})
	score2 := matcher.PairScore(a, c)
	expected2 := 2 // 2 shared dev environments
	if score2 != expected2 {
		t.Errorf("expected %d (two shared dev envs), got %d", expected2, score2)
	}
}

func TestExtractJSON_bareJSON(t *testing.T) {
	input := `{"name": "foo"}`
	got := extractJSON(input)
	if got != input {
		t.Errorf("expected %q, got %q", input, got)
	}
}

func TestExtractJSON_markdownWrapped(t *testing.T) {
	input := "```json\n{\"name\": \"foo\"}\n```"
	got := extractJSON(input)
	want := `{"name": "foo"}`
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestExtractJSON_trailingGarbage(t *testing.T) {
	input := `Here you go: {"name": "foo"} done!`
	got := extractJSON(input)
	want := `{"name": "foo"}`
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestFmtInterests(t *testing.T) {
	if result := fmtInterests(Interests{}); result != "" {
		t.Errorf("expected empty string for zero value, got %s", result)
	}

	result := fmtInterests(Interests{Languages: []string{"Go", "Python"}})
	if result != "languages: Go, Python" {
		t.Errorf("expected 'languages: Go, Python', got %s", result)
	}

	result = fmtInterests(Interests{Languages: []string{"Go", "Python"}, Tools: []string{"Docker"}})
	if result != "languages: Go, Python; tools: Docker" {
		t.Errorf("expected both categories, got %s", result)
	}

	if result := fmtInterests(Interests{Languages: []string{}}); result != "" {
		t.Errorf("expected empty string for an empty slice, got %s", result)
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "bare JSON",
			input:    `{"name": "test"}`,
			expected: `{"name": "test"}`,
		},
		{
			name:     "markdown wrapped",
			input:    "```json\n{\"name\": \"test\"}\n```",
			expected: `{"name": "test"}`,
		},
		{
			name:     "trailing garbage",
			input:    `{"name": "test"} some extra text`,
			expected: `{"name": "test"}`,
		},
		{
			name:     "no JSON",
			input:    "just plain text",
			expected: "just plain text",
		},
		{
			name:     "unclosed brace",
			input:    `{"name": "test"`,
			expected: `{"name": "test"`,
		},
		{
			name:     "nested JSON",
			input:    `text {"outer": {"inner": "value"}} more text`,
			expected: `{"outer": {"inner": "value"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractJSON(tt.input)
			if result != tt.expected {
				t.Errorf("extractJSON(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestMatchNewcomer_BreakingAMatchReturnsTheDisplacedParticipantToThePool(t *testing.T) {
	db := newTestDB(t)
	llm := newFakeLLM().onFunc("matchmaker", scoreTable(map[[2]string]int{{"N", "A"}: 70}))
	gh := newFakeGitHub()
	matchmaking := NewMatchmaking(db, NewMatcher(db, gh, llm), NewRelationships(db))
	for _, id := range []string{"A", "B", "N"} {
		db.CreateParticipant(id, id, id, true)
		db.SetProfile(id, &GitHubProfile{Login: id})
		db.SetPersona(id, id, "")
		forceStep(db, id, "ready")
	}
	NewRelationships(db).Pair(Match{A: reload(t, db, "A"), B: reload(t, db, "B"), Result: &matchResult{Score: 40, Reason: "meh"}})

	if err := matchmaking.MatchNewcomer(reload(t, db, "N")); err != nil {
		t.Fatal(err)
	}

	if n, a := reload(t, db, "N"), reload(t, db, "A"); n.MatchedWith != "A" || a.MatchedWith != "N" || a.CompatScore != 70 {
		t.Errorf("want N and A matched at 70, got N→%q A→%q (%d)", n.MatchedWith, a.MatchedWith, a.CompatScore)
	}
	if b := reload(t, db, "B"); b.MatchedWith != "" || b.PipelineStep != "ready" {
		t.Errorf("displaced B should be ready and unmatched, got step %s matched with %q", b.PipelineStep, b.MatchedWith)
	}
}

func TestMatchNewcomer_ADisplacedPartnerIsRematchedRightAway(t *testing.T) {
	db := newTestDB(t)
	llm := newFakeLLM().onFunc("matchmaker", scoreTable(map[[2]string]int{{"N", "A"}: 70, {"B", "C"}: 60}))
	gh := newFakeGitHub()
	rel := NewRelationships(db)
	matchmaking := NewMatchmaking(db, NewMatcher(db, gh, llm), rel)
	for _, id := range []string{"A", "B", "C", "D", "N"} {
		seed(t, db, id, id, "ready")
	}
	rel.Pair(Match{A: reload(t, db, "A"), B: reload(t, db, "B"), Result: &matchResult{Score: 40}})
	rel.Pair(Match{A: reload(t, db, "C"), B: reload(t, db, "D"), Result: &matchResult{Score: 30}})

	if err := matchmaking.MatchNewcomer(reload(t, db, "N")); err != nil {
		t.Fatal(err)
	}

	// N takes over A (70 beats 40); displaced B takes over C (60 beats 30);
	// displaced D beats nobody (and may not take B or C back) and stays in the Pool.
	for id, want := range map[string]string{"N": "A", "A": "N", "B": "C", "C": "B", "D": ""} {
		if got := reload(t, db, id).MatchedWith; got != want {
			t.Errorf("%s matched with %q, want %q", id, got, want)
		}
	}
	assertInvariant(t, db)
}

func TestMatchNewcomer_ADisplacedPartnerMayTakeOverAMatchFormedEarlierInTheChain(t *testing.T) {
	db := newTestDB(t)
	llm := newFakeLLM().onFunc("matchmaker", scoreTable(map[[2]string]int{{"N", "A"}: 70, {"B", "C"}: 60, {"D", "A"}: 80}))
	gh := newFakeGitHub()
	rel := NewRelationships(db)
	matchmaking := NewMatchmaking(db, NewMatcher(db, gh, llm), rel)
	for _, id := range []string{"A", "B", "C", "D", "N"} {
		seed(t, db, id, id, "ready")
	}
	rel.Pair(Match{A: reload(t, db, "A"), B: reload(t, db, "B"), Result: &matchResult{Score: 40}})
	rel.Pair(Match{A: reload(t, db, "C"), B: reload(t, db, "D"), Result: &matchResult{Score: 30}})

	if err := matchmaking.MatchNewcomer(reload(t, db, "N")); err != nil {
		t.Fatal(err)
	}

	// N takes A (70 > 40); displaced B takes C (60 > 30); displaced D takes A from N
	// (80 > 70), although A was paired earlier in this chain; displaced N beats nobody.
	for id, want := range map[string]string{"D": "A", "A": "D", "B": "C", "C": "B", "N": ""} {
		if got := reload(t, db, id).MatchedWith; got != want {
			t.Errorf("%s matched with %q, want %q", id, got, want)
		}
	}
	assertInvariant(t, db)
}

// TestMatchNewcomer_APathologicalChainStopsAtTheBound covers #48: the LLM's
// score for a pair is not perfectly repeatable, so a chain of take-overs
// isn't guaranteed to end on its own. The fake LLM here returns a different
// score on every call (call count mod 10, offset well above every seeded
// Match score), so the "assessments are cached and scores only rise" argument
// for why chains end on their own does not apply; maxChain is shrunk on this
// Matchmaking instance so the bound is reached within a small, deterministic
// pool instead of needing hundreds of Participants.
func TestMatchNewcomer_APathologicalChainStopsAtTheBound(t *testing.T) {
	db := newTestDB(t)
	var calls int64
	llm := newFakeLLM().onFunc("matchmaker", func(string) (string, error) {
		n := atomic.AddInt64(&calls, 1)
		return fmt.Sprintf(`{"score": %d, "reason": "x"}`, 90+n%10), nil
	})
	gh := newFakeGitHub()
	rel := NewRelationships(db)
	matchmaking := NewMatchmaking(db, NewMatcher(db, gh, llm), rel)
	matchmaking.maxChain = 3
	for _, id := range []string{"A", "B", "C", "D", "E", "F", "N"} {
		seed(t, db, id, id, "ready")
	}
	rel.Pair(Match{A: reload(t, db, "A"), B: reload(t, db, "B"), Result: &matchResult{Score: 40}})
	rel.Pair(Match{A: reload(t, db, "C"), B: reload(t, db, "D"), Result: &matchResult{Score: 30}})
	rel.Pair(Match{A: reload(t, db, "E"), B: reload(t, db, "F"), Result: &matchResult{Score: 20}})

	err := matchmaking.MatchNewcomer(reload(t, db, "N"))

	if err == nil || !strings.Contains(err.Error(), "chain of take-overs longer than 3") {
		t.Fatalf("want a chain-too-long error, got %v", err)
	}
	assertInvariant(t, db)
}

func matchmakingWithPool(t *testing.T) (*Matchmaking, *DB, *fakeLLM) {
	t.Helper()
	db := newTestDB(t)
	llm := newFakeLLM().on("matchmaker", `{"score": 70}`)
	for _, id := range []string{"A", "B", "N"} {
		seed(t, db, id, id, "ready")
	}
	return NewMatchmaking(db, NewMatcher(db, newFakeGitHub(), llm), NewRelationships(db)), db, llm
}

func TestMatchmaking_ReportsFailures(t *testing.T) {
	t.Run("rematch: breaking the Matches fails", func(t *testing.T) {
		mm, db, _ := matchmakingWithPool(t)
		NewRelationships(db).Pair(Match{A: reload(t, db, "A"), B: reload(t, db, "B"), Result: assessment(40)})
		failWrites(t, db, "unpair")
		if err := mm.Rematch(); err == nil {
			t.Error("want the failure reported")
		}
	})
	t.Run("rematch: storing a Match fails", func(t *testing.T) {
		mm, db, _ := matchmakingWithPool(t)
		failWrites(t, db, "pair")
		if err := mm.Rematch(); err == nil {
			t.Error("want the failure reported")
		}
	})
	t.Run("rematch: broken database", func(t *testing.T) {
		mm, db, _ := matchmakingWithPool(t)
		breakDB(db)
		if err := mm.Rematch(); err == nil {
			t.Error("want the failure reported")
		}
	})
	t.Run("newcomer: storing the Match fails", func(t *testing.T) {
		mm, db, _ := matchmakingWithPool(t)
		failWrites(t, db, "pair")
		if err := mm.MatchNewcomer(reload(t, db, "N")); err == nil {
			t.Error("want the failure reported")
		}
	})
	t.Run("newcomer: broken database", func(t *testing.T) {
		mm, db, _ := matchmakingWithPool(t)
		n := reload(t, db, "N")
		breakDB(db)
		if err := mm.MatchNewcomer(n); err == nil {
			t.Error("want the failure reported")
		}
	})
}

func TestMatchmaking_ANewcomerWhoNoLongerExistsIsSkipped(t *testing.T) {
	mm, _, llm := matchmakingWithPool(t)

	if err := mm.MatchNewcomer(&Participant{ID: "gone"}); err != nil {
		t.Fatal(err)
	}
	if llm.callsMatching("matchmaker") != 0 {
		t.Error("nothing to assess for a Participant who is gone")
	}
}
