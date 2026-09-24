package main

import (
	"testing"
)

func makeParticipant(id string, langs []string, answers map[string]string) *Participant {
	return &Participant{
		ID:           id,
		GitHubHandle: id,
		PersonaName:  "The " + id,
		Profile:     &GitHubProfile{Login: id, Languages: langs},
		Answers:      answers,
	}
}

func makeParticipantWithTopics(id string, langs []string, topics []string, answers map[string]string) *Participant {
	return &Participant{
		ID:           id,
		GitHubHandle: id,
		PersonaName:  "The " + id,
		Profile:     &GitHubProfile{Login: id, Languages: langs, TopTopics: topics},
		Answers:      answers,
	}
}

func makeParticipantWithProjectType(id string, langs []string, projectType string, answers map[string]string) *Participant {
	profile := GitHubProfile{Login: id, Languages: langs}
	if projectType != "" {
		profile.ExtraAnswers = &ExtraAnswers{ProjectType: projectType}
	}
	return &Participant{
		ID:           id,
		GitHubHandle: id,
		PersonaName:  "The " + id,
		Profile:     &profile,
		Answers:      answers,
	}
}

func makeParticipantWithDevEnv(id string, langs []string, devEnv []string, answers map[string]string) *Participant {
	profile := GitHubProfile{Login: id, Languages: langs}
	if len(devEnv) > 0 {
		profile.ExtraAnswers = &ExtraAnswers{DevEnvironment: devEnv}
	}
	return &Participant{
		ID:           id,
		GitHubHandle: id,
		PersonaName:  "The " + id,
		Profile:     &profile,
		Answers:      answers,
	}
}

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
	a := makeParticipant("a", []string{"Go", "Python"}, nil)
	b := makeParticipant("b", []string{"Go", "Rust"}, nil)

	score := matcher.PairScore(a, b)
	if score != 3 {
		t.Errorf("expected 3 (one shared language), got %d", score)
	}

	c := makeParticipant("c", []string{"Go", "Python"}, nil)
	score2 := matcher.PairScore(a, c)
	if score2 != 6 {
		t.Errorf("expected 6 (two shared languages), got %d", score2)
	}
}

func TestPairScore_answers(t *testing.T) {
	matcher := &Matcher{}
	a := makeParticipant("a", nil, map[string]string{"0": "Tabs", "1": "Go"})
	b := makeParticipant("b", nil, map[string]string{"0": "Tabs", "1": "Python"})

	score := matcher.PairScore(a, b)
	if score != 1 {
		t.Errorf("expected 1 (one matching answer), got %d", score)
	}
}

func TestPairScore_combined(t *testing.T) {
	matcher := &Matcher{}
	a := makeParticipant("a", []string{"Go"}, map[string]string{"0": "Tabs"})
	b := makeParticipant("b", []string{"Go"}, map[string]string{"0": "Tabs"})

	score := matcher.PairScore(a, b)
	if score != 4 {
		t.Errorf("expected 4 (3 language + 1 answer), got %d", score)
	}
}

func TestPairScore_noOverlap(t *testing.T) {
	matcher := &Matcher{}
	a := makeParticipant("a", []string{"Go"}, map[string]string{"0": "Tabs"})
	b := makeParticipant("b", []string{"Rust"}, map[string]string{"0": "Spaces"})

	if score := matcher.PairScore(a, b); score != 0 {
		t.Errorf("expected 0, got %d", score)
	}
}

// Note: Follow relationship scoring requires GitHub API access and is deferred for a follow-up
// func TestPairScore_followRelationships

func TestPairScore_topics(t *testing.T) {
	matcher := &Matcher{}
	a := makeParticipantWithTopics("a", []string{"Go"}, []string{"web", "api"}, nil)
	b := makeParticipantWithTopics("b", []string{"Python"}, []string{"web", "data"}, nil)

	score := matcher.PairScore(a, b)
	expected := 2 // 1 shared topic (web) * 2 points
	if score != expected {
		t.Errorf("expected %d (one shared topic), got %d", expected, score)
	}

	c := makeParticipantWithTopics("c", []string{"Rust"}, []string{"web", "api"}, nil)
	score2 := matcher.PairScore(a, c)
	expected2 := 4 // 2 shared topics * 2 points
	if score2 != expected2 {
		t.Errorf("expected %d (two shared topics), got %d", expected2, score2)
	}
}

func TestPairScore_projectTypes(t *testing.T) {
	matcher := &Matcher{}
	a := makeParticipantWithProjectType("a", []string{"Go"}, "Web", nil)
	b := makeParticipantWithProjectType("b", []string{"Python"}, "Web", nil)

	score := matcher.PairScore(a, b)
	expected := 2 // shared project type
	if score != expected {
		t.Errorf("expected %d (shared project type), got %d", expected, score)
	}

	c := makeParticipantWithProjectType("c", []string{"Rust"}, "Backend", nil)
	score2 := matcher.PairScore(a, c)
	if score2 != 0 {
		t.Errorf("expected 0 (different project types), got %d", score2)
	}
}

func TestPairScore_devEnvironments(t *testing.T) {
	matcher := &Matcher{}
	a := makeParticipantWithDevEnv("a", []string{"Go"}, []string{"IDE", "VIM"}, nil)
	b := makeParticipantWithDevEnv("b", []string{"Python"}, []string{"IDE", "Cloud"}, nil)

	score := matcher.PairScore(a, b)
	expected := 1 // 1 shared dev environment
	if score != expected {
		t.Errorf("expected %d (one shared dev env), got %d", expected, score)
	}

	c := makeParticipantWithDevEnv("c", []string{"Rust"}, []string{"IDE", "VIM"}, nil)
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
	// Test empty interests
	if result := fmtInterests(nil); result != "" {
		t.Errorf("expected empty string for nil, got %s", result)
	}

	if result := fmtInterests(map[string]interface{}{}); result != "" {
		t.Errorf("expected empty string for empty map, got %s", result)
	}

	// Test with languages
	interests := map[string]interface{}{
		"languages": []string{"Go", "Python"},
	}
	result := fmtInterests(interests)
	if result != "languages: Go, Python" {
		t.Errorf("expected 'languages: Go, Python', got %s", result)
	}

	// Test with multiple categories
	interests = map[string]interface{}{
		"languages": []string{"Go", "Python"},
		"tools":    []string{"Docker"},
	}
	result = fmtInterests(interests)
	if result != "languages: Go, Python; tools: Docker" && result != "tools: Docker; languages: Go, Python" {
		t.Errorf("expected both categories, got %s", result)
	}

	// Test with empty slice
	interests = map[string]interface{}{
		"languages": []string{},
	}
	result = fmtInterests(interests)
	if result != "" {
		t.Errorf("expected empty string for empty slice, got %s", result)
	}

	// Test with non-slice value (should be skipped)
	interests = map[string]interface{}{
		"languages": "Go", // Not a slice
	}
	result = fmtInterests(interests)
	if result != "" {
		t.Errorf("expected empty string for non-slice value, got %s", result)
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
