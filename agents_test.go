package main

import (
	"fmt"
	"strings"
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

func TestFallbackQuestions(t *testing.T) {
	// This test verifies that GitHub users with LLM failures get ExtraQuestions as fallback
	// We can't easily test this without mocking the LLM, but we can verify the structure

	// Verify ExtraQuestions has the expected questions
	expectedCount := 5
	if len(ExtraQuestions) != expectedCount {
		t.Errorf("expected %d ExtraQuestions, got %d", expectedCount, len(ExtraQuestions))
	}

	// Verify ExtraQuestions covers the key categories
	foundLanguages := false
	foundProjectType := false
	foundDevEnv := false
	foundWeirdestBug := false
	foundKeyboard := false

	for _, q := range ExtraQuestions {
		switch q.ID {
		case "extra_0":
			foundLanguages = true
		case "extra_1":
			foundProjectType = true
		case "extra_2":
			foundDevEnv = true
		case "extra_3":
			foundWeirdestBug = true
		case "extra_4":
			foundKeyboard = true
		}
	}

	if !foundLanguages {
		t.Error("ExtraQuestions missing languages question (extra_0)")
	}
	if !foundProjectType {
		t.Error("ExtraQuestions missing project type question (extra_1)")
	}
	if !foundDevEnv {
		t.Error("ExtraQuestions missing dev environment question (extra_2)")
	}
	if !foundWeirdestBug {
		t.Error("ExtraQuestions missing weirdest bug question (extra_3)")
	}
	if !foundKeyboard {
		t.Error("ExtraQuestions missing keyboard question (extra_4)")
	}
}

func TestTop5Candidates(t *testing.T) {
	matcher := &Matcher{}
	// Make 7 participants; p0 shares languages with p1..p5 (+3 each)
	p0 := makeParticipant("p0", []string{"Go"}, nil)
	var all []*Participant
	all = append(all, p0)
	for i := 1; i <= 6; i++ {
		langs := []string{"Go"}
		if i > 5 {
			langs = []string{"Rust"} // p6 shares nothing
		}
		all = append(all, makeParticipant(string(rune('p'+i)), langs, nil))
	}

	top := matcher.Top5Candidates(p0, all)
	if len(top) != 5 {
		t.Errorf("expected 5 candidates, got %d", len(top))
	}
	// p6 (no shared language) must not be in the top 5
	for _, c := range top {
		if c.ID == string(rune('p'+6)) {
			t.Error("p6 (no overlap) should not be in top 5")
		}
	}
}

func TestTop5Candidates_fewerThan5(t *testing.T) {
	matcher := &Matcher{}
	p0 := makeParticipant("p0", nil, nil)
	others := []*Participant{
		makeParticipant("p1", nil, nil),
		makeParticipant("p2", nil, nil),
	}
	all := append([]*Participant{p0}, others...)
	top := matcher.Top5Candidates(p0, all)
	if len(top) != 2 {
		t.Errorf("expected 2 candidates (n-1), got %d", len(top))
	}
}

func TestCollectCandidatePairs(t *testing.T) {
	matcher := &Matcher{}
	var ps []*Participant
	for i := 0; i < 6; i++ {
		ps = append(ps, makeParticipant(string(rune('a'+i)), nil, nil))
	}

	pairs := matcher.CollectCandidatePairs(ps)

	// Upper bound: N*5 = 30, but with dedup fewer
	if len(pairs) > 6*5 {
		t.Errorf("too many pairs: %d", len(pairs))
	}

	// Each pair must appear exactly once (no duplicates)
	seen := map[string]bool{}
	for _, p := range pairs {
		k := pairKey(p[0], p[1])
		if seen[k] {
			t.Errorf("duplicate pair: %s", k)
		}
		seen[k] = true
	}

	// No self-pairs
	for _, p := range pairs {
		if p[0].ID == p[1].ID {
			t.Error("self-pair found")
		}
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

func TestGreedyMatch_allMatched(t *testing.T) {
	matcher := &Matcher{}
	var ps []*Participant
	for i := 0; i < 6; i++ {
		ps = append(ps, makeParticipant(string(rune('a'+i)), nil, nil))
	}

	pairs := matcher.GreedyMatch(ps)

	if len(pairs) != 3 {
		t.Errorf("expected 3 pairs for 6 participants, got %d", len(pairs))
	}

	seen := map[string]bool{}
	for _, pair := range pairs {
		for _, p := range pair {
			if seen[p.ID] {
				t.Errorf("participant %s appears more than once", p.ID)
			}
			seen[p.ID] = true
		}
	}

	if len(seen) != 6 {
		t.Errorf("expected all 6 participants matched, got %d", len(seen))
	}
}

func TestGreedyMatch_oddNumber(t *testing.T) {
	matcher := &Matcher{}
	var ps []*Participant
	for i := 0; i < 5; i++ {
		ps = append(ps, makeParticipant(string(rune('a'+i)), nil, nil))
	}

	pairs := matcher.GreedyMatch(ps)

	// 5 participants → 2 pairs, 1 leftover (greedyMatch leaves odd one out)
	if len(pairs) != 2 {
		t.Errorf("expected 2 pairs for 5 participants, got %d", len(pairs))
	}
}

func TestFilterFixedQuestions(t *testing.T) {
	// filterFixedQuestions removes fixed_1 from FixedQuestions
	pipeline := &AgentPipeline{}
	filtered := pipeline.filterFixedQuestions()
	
	// Should have one less question than FixedQuestions
	expectedLen := len(FixedQuestions) - 1
	if len(filtered) != expectedLen {
		t.Errorf("expected %d filtered questions, got %d", expectedLen, len(filtered))
	}
	
	// Should not contain fixed_1
	for _, q := range filtered {
		if q.ID == "fixed_1" {
			t.Error("filtered questions should not contain fixed_1")
		}
	}
	
	// Should contain all other questions
	found := make(map[string]bool)
	for _, q := range filtered {
		found[q.ID] = true
	}
	for _, q := range FixedQuestions {
		if q.ID != "fixed_1" && !found[q.ID] {
			t.Errorf("expected to find question %s in filtered list", q.ID)
		}
	}
}

func TestStringsToQuestions(t *testing.T) {
	pipeline := &AgentPipeline{}
	
	texts := []string{"Q1", "Q2", "Q3"}
	questions := pipeline.stringsToQuestions(texts, "custom")
	
	if len(questions) != len(texts) {
		t.Errorf("expected %d questions, got %d", len(texts), len(questions))
	}
	
	for i, q := range questions {
		if q.Text != texts[i] {
			t.Errorf("question %d: expected text %q, got %q", i, texts[i], q.Text)
		}
		if q.ID != fmt.Sprintf("custom_%d", i) {
			t.Errorf("question %d: expected ID %q, got %q", i, fmt.Sprintf("custom_%d", i), q.ID)
		}
		if q.Options != nil {
			t.Errorf("question %d: expected nil options, got %v", i, q.Options)
		}
	}
}

func TestComputeInterestsFromCompleteProfile(t *testing.T) {
	pipeline := &AgentPipeline{}
	
	// Test with GitHub profile
	profile := &CompleteProfile{
		GitHubProfile: &GitHubProfile{
			Languages: []string{"Go", "Python"},
			TopTopics: []string{"web", "api"},
		},
	}
	
	interests := pipeline.computeInterestsFromCompleteProfile(profile)
	
	if interests == nil {
		t.Fatal("expected non-nil interests")
	}
	
	// Check languages
	if langs, ok := interests["languages"].([]string); !ok {
		t.Errorf("expected languages to be []string, got %T", interests["languages"])
	} else if len(langs) != 2 {
		t.Errorf("expected 2 languages, got %d", len(langs))
	}
	
	// Check tools (topics)
	if tools, ok := interests["tools"].([]string); !ok {
		t.Errorf("expected tools to be []string, got %T", interests["tools"])
	} else if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}
}

func TestBuildPersonaPrompt(t *testing.T) {
	pipeline := &AgentPipeline{}
	
	profile := &CompleteProfile{
		GitHubProfile: &GitHubProfile{
			Login:   "testuser",
			Name:    "Test User",
			Bio:     "Test bio",
			Company: "Test Co",
		},
		InterviewAnswers: map[string]string{
			"q1": "answer1",
			"q2": "answer2",
		},
	}
	
	prompt := pipeline.buildPersonaPrompt(profile)
	
	// Should contain GitHub info
	if !strings.Contains(prompt, "@testuser") {
		t.Error("prompt should contain GitHub handle")
	}
	
	// Should contain interview answers
	if !strings.Contains(prompt, "Q[q1]:") {
		t.Error("prompt should contain interview answers")
	}
}

func TestBuildCompleteProfile(t *testing.T) {
	// Create a minimal AgentPipeline (we don't need the clients for this test)
	ap := &AgentPipeline{}

	profile := &GitHubProfile{
		Login:    "testuser",
		Languages: []string{"Go", "Python"},
	}

	interviewAnswers := map[string]string{
		"fixed_0": "Tabs",
		"fixed_1": "Go",
	}

	completeProfile := ap.buildCompleteProfile(profile, nil, interviewAnswers)

	if completeProfile.GitHubProfile != profile {
		t.Error("GitHubProfile not set correctly")
	}

	if len(completeProfile.InterviewAnswers) != len(interviewAnswers) {
		t.Error("InterviewAnswers length mismatch")
	}
	for k, v := range interviewAnswers {
		if completeProfile.InterviewAnswers[k] != v {
			t.Errorf("InterviewAnswers[%s] = %s, want %s", k, completeProfile.InterviewAnswers[k], v)
		}
	}

	if completeProfile.Interests == nil {
		t.Error("Interests should not be nil")
	}

	// Check that interests were computed from GitHubProfile
	if langs, ok := completeProfile.Interests["languages"].([]string); !ok || len(langs) != 2 {
		t.Errorf("expected languages in interests, got %v", completeProfile.Interests["languages"])
	}
}

