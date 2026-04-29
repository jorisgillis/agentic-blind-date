package main

import (
	"encoding/json"
	"testing"
)

func makeParticipant(id string, langs []string, answers map[string]string) *Participant {
	profile := GitHubProfile{Login: id, Languages: langs}
	profileJSON, _ := json.Marshal(profile)
	answersJSON, _ := json.Marshal(answers)
	return &Participant{
		ID:          id,
		GitHubHandle: id,
		PersonaName: "The " + id,
		ProfileJSON:  string(profileJSON),
		AnswersJSON:  string(answersJSON),
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
	a := makeParticipant("a", []string{"Go", "Python"}, nil)
	b := makeParticipant("b", []string{"Go", "Rust"}, nil)

	score := pairScore(a, b)
	if score != 3 {
		t.Errorf("expected 3 (one shared language), got %d", score)
	}

	c := makeParticipant("c", []string{"Go", "Python"}, nil)
	score2 := pairScore(a, c)
	if score2 != 6 {
		t.Errorf("expected 6 (two shared languages), got %d", score2)
	}
}

func TestPairScore_answers(t *testing.T) {
	a := makeParticipant("a", nil, map[string]string{"0": "Tabs", "1": "Go"})
	b := makeParticipant("b", nil, map[string]string{"0": "Tabs", "1": "Python"})

	score := pairScore(a, b)
	if score != 1 {
		t.Errorf("expected 1 (one matching answer), got %d", score)
	}
}

func TestPairScore_combined(t *testing.T) {
	a := makeParticipant("a", []string{"Go"}, map[string]string{"0": "Tabs"})
	b := makeParticipant("b", []string{"Go"}, map[string]string{"0": "Tabs"})

	score := pairScore(a, b)
	if score != 4 {
		t.Errorf("expected 4 (3 language + 1 answer), got %d", score)
	}
}

func TestPairScore_noOverlap(t *testing.T) {
	a := makeParticipant("a", []string{"Go"}, map[string]string{"0": "Tabs"})
	b := makeParticipant("b", []string{"Rust"}, map[string]string{"0": "Spaces"})

	if score := pairScore(a, b); score != 0 {
		t.Errorf("expected 0, got %d", score)
	}
}

func TestTop5Candidates(t *testing.T) {
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

	top := top5Candidates(p0, all)
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
	p0 := makeParticipant("p0", nil, nil)
	others := []*Participant{
		makeParticipant("p1", nil, nil),
		makeParticipant("p2", nil, nil),
	}
	all := append([]*Participant{p0}, others...)
	top := top5Candidates(p0, all)
	if len(top) != 2 {
		t.Errorf("expected 2 candidates (n-1), got %d", len(top))
	}
}

func TestCollectCandidatePairs(t *testing.T) {
	var ps []*Participant
	for i := 0; i < 6; i++ {
		ps = append(ps, makeParticipant(string(rune('a'+i)), nil, nil))
	}

	pairs := collectCandidatePairs(ps)

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
	var ps []*Participant
	for i := 0; i < 6; i++ {
		ps = append(ps, makeParticipant(string(rune('a'+i)), nil, nil))
	}

	pairs := greedyMatch(ps)

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
	var ps []*Participant
	for i := 0; i < 5; i++ {
		ps = append(ps, makeParticipant(string(rune('a'+i)), nil, nil))
	}

	pairs := greedyMatch(ps)

	// 5 participants → 2 pairs, 1 leftover (greedyMatch leaves odd one out)
	if len(pairs) != 2 {
		t.Errorf("expected 2 pairs for 5 participants, got %d", len(pairs))
	}
}
