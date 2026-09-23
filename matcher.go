package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// matchResult contains the LLM-generated compatibility assessment between two participants.
type matchResult struct {
	Score       int      `json:"score"`
	Reason      string   `json:"reason"`
	RedFlags    []string `json:"red_flags"`
	GreenFlags  []string `json:"green_flags"`
	Icebreakers []string `json:"icebreakers"`
}

// Matcher pairs Participants. It owns the heuristic Pair score, the LLM
// compatibility assessment (prompt, parsing, validation) and the two-level
// cache of assessments from ADR-0002.
type Matcher struct {
	db      *DB
	github  GitHubAPI
	mistral LLM

	mu    sync.Mutex
	cache map[string]*matchResult // in-memory level; the llm_cache table is the persistent level
}

// NewMatcher creates a new Matcher instance.
func NewMatcher(db *DB, github GitHubAPI, mistral LLM) *Matcher {
	return &Matcher{
		db:      db,
		github:  github,
		mistral: mistral,
		cache:   make(map[string]*matchResult),
	}
}

// defaultMatchResult returns a default match result when LLM scoring fails.
func defaultMatchResult() *matchResult {
	return &matchResult{
		Score:       42,
		Reason:      "The algorithm has spoken. We cannot explain.",
		RedFlags:    []string{},
		GreenFlags:  []string{"You're both here tonight"},
		Icebreakers: []string{"What brings you to this meetup?", "What are you currently building?", "Best tech talk you've seen recently?"},
	}
}

// ScorePair returns the LLM compatibility assessment for a Pair, in either
// order. Assessments are cached in memory and in the database; failures are
// not cached, so the next call retries.
func (m *Matcher) ScorePair(a, b *Participant) (*matchResult, error) {
	key := pairKey(a, b)

	m.mu.Lock()
	cached, ok := m.cache[key]
	m.mu.Unlock()
	if ok {
		return cached, nil
	}
	if cached, ok := m.db.GetLLMCache(key); ok {
		m.remember(key, cached)
		return cached, nil
	}

	result, err := m.assess(a, b)
	if err != nil {
		return nil, err
	}
	m.remember(key, result)
	m.db.SetLLMCache(key, result)
	return result, nil
}

// ClearCache forgets every cached assessment, in memory and in the database.
func (m *Matcher) ClearCache() {
	m.mu.Lock()
	m.cache = make(map[string]*matchResult)
	m.mu.Unlock()
	m.db.ClearLLMCache()
}

func (m *Matcher) remember(key string, result *matchResult) {
	m.mu.Lock()
	m.cache[key] = result
	m.mu.Unlock()
}

func pairKey(a, b *Participant) string {
	if a.ID < b.ID {
		return a.ID + ":" + b.ID
	}
	return b.ID + ":" + a.ID
}

func (m *Matcher) assess(p1, p2 *Participant) (*matchResult, error) {
	system := `You are the matchmaker at a tech meetup blind date event.
Analyze two developers' profiles and produce a fun, humorous compatibility assessment.
Respond with ONLY valid JSON — no markdown:
{"score": <0-100>, "reason": "<one funny sentence max 80 chars>", "red_flags": ["...", "..."], "green_flags": ["...", "..."], "icebreakers": ["<question one can ask the other>", "<question>", "<question>"]}`

	user := "Compare these two developers:\n\n" +
		describeDeveloper(1, p1) + "\n\n" + describeDeveloper(2, p2) + m.followNote(p1, p2)

	response, err := m.mistral.Chat(system, user)
	if err != nil {
		return nil, err
	}

	var reply struct {
		Score       *int     `json:"score"`
		Reason      string   `json:"reason"`
		RedFlags    []string `json:"red_flags"`
		GreenFlags  []string `json:"green_flags"`
		Icebreakers []string `json:"icebreakers"`
	}
	if err := json.Unmarshal([]byte(extractJSON(response)), &reply); err != nil {
		return nil, fmt.Errorf("match parse error: %v (raw: %s)", err, response)
	}
	if reply.Score == nil {
		return nil, fmt.Errorf("match reply has no score (raw: %s)", response)
	}
	return &matchResult{
		Score:       min(max(*reply.Score, 0), 100),
		Reason:      reply.Reason,
		RedFlags:    nonNil(reply.RedFlags),
		GreenFlags:  nonNil(reply.GreenFlags),
		Icebreakers: nonNil(reply.Icebreakers),
	}, nil
}

func describeDeveloper(n int, p *Participant) string {
	profile := p.Profile
	if profile == nil {
		profile = &GitHubProfile{}
	}
	answers := p.Answers
	if answers == nil {
		answers = map[string]string{}
	}
	s := fmt.Sprintf("DEVELOPER %d (%s):\n%s\nInterview answers: %v", n, p.PersonaName, profile.Summary(), answers)
	if interests := fmtInterests(p.Interests); interests != "" {
		s += "\nInterests: " + interests
	}
	return s
}

func (m *Matcher) followNote(p1, p2 *Participant) string {
	if m.github == nil || p1.GitHubHandle == "" || p2.GitHubHandle == "" {
		return ""
	}
	aFollowsB, bFollowsA := m.github.CheckMutualFollow(p1.GitHubHandle, p2.GitHubHandle)
	switch {
	case aFollowsB && bFollowsA:
		return fmt.Sprintf("\n\nNote: %s and %s already follow each other on GitHub!", p1.PersonaName, p2.PersonaName)
	case aFollowsB:
		return fmt.Sprintf("\n\nNote: %s already follows %s on GitHub.", p1.PersonaName, p2.PersonaName)
	case bFollowsA:
		return fmt.Sprintf("\n\nNote: %s already follows %s on GitHub.", p2.PersonaName, p1.PersonaName)
	}
	return ""
}

// fmtInterests renders Interests as "category: a, b; ...". Values may be
// []string (freshly computed) or []any (decoded from the database).
func fmtInterests(interests map[string]interface{}) string {
	categories := make([]string, 0, len(interests))
	for category := range interests {
		categories = append(categories, category)
	}
	sort.Strings(categories)

	var parts []string
	for _, category := range categories {
		var items []string
		switch v := interests[category].(type) {
		case []string:
			items = v
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					items = append(items, s)
				}
			}
		}
		if len(items) > 0 {
			parts = append(parts, category+": "+strings.Join(items, ", "))
		}
	}
	return strings.Join(parts, "; ")
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// PairScore calculates a numeric compatibility score between two participants.
func (m *Matcher) PairScore(a, b *Participant) int {
	score := 0

	aP := a.Profile
	if aP == nil {
		return 0
	}
	bP := b.Profile
	if bP == nil {
		return 0
	}

	// Score shared languages: +3 per match
	aLangs := map[string]bool{}
	for _, l := range aP.Languages {
		aLangs[l] = true
	}
	for _, l := range bP.Languages {
		if aLangs[l] {
			score += 3
		}
	}

	// Score shared topics: +2 per match
	aTopics := map[string]bool{}
	for _, t := range aP.TopTopics {
		aTopics[t] = true
	}
	for _, t := range bP.TopTopics {
		if aTopics[t] {
			score += 2
		}
	}

	// Score shared project types: +2 if same
	if aP.ExtraAnswers != nil && bP.ExtraAnswers != nil {
		if aP.ExtraAnswers.ProjectType != "" && aP.ExtraAnswers.ProjectType == bP.ExtraAnswers.ProjectType {
			score += 2
		}
	}

	// Score shared dev environments: +1 per match
	if aP.ExtraAnswers != nil && bP.ExtraAnswers != nil {
		aDevEnv := map[string]bool{}
		for _, e := range aP.ExtraAnswers.DevEnvironment {
			aDevEnv[e] = true
		}
		for _, e := range bP.ExtraAnswers.DevEnvironment {
			if aDevEnv[e] {
				score++
			}
		}
	}

	// Score matching interview answers: +1 per match
	aAns := a.Answers
	if aAns == nil {
		aAns = map[string]string{}
	}
	bAns := b.Answers
	if bAns == nil {
		bAns = map[string]string{}
	}

	for k, av := range aAns {
		if bv, ok := bAns[k]; ok && av == bv {
			score++
		}
	}

	return score
}

// Top5Candidates returns the top 5 most compatible candidates for a participant.
func (m *Matcher) Top5Candidates(p *Participant, all []*Participant) []*Participant {
	// Filter out self and already matched
	var candidates []*Participant
	for _, other := range all {
		if other.ID == p.ID || other.MatchedWith != "" {
			continue
		}
		candidates = append(candidates, other)
	}

	// Sort by pairScore descending
	for i := range candidates {
		for j := i + 1; j < len(candidates); j++ {
			if m.PairScore(p, candidates[j]) > m.PairScore(p, candidates[i]) {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	// Return top 5 (or fewer)
	if len(candidates) > 5 {
		return candidates[:5]
	}
	return candidates
}

// CollectCandidatePairs creates pairs from participants who are ready for matching.
func (m *Matcher) CollectCandidatePairs(participants []*Participant) [][2]*Participant {
	var pairs [][2]*Participant
	for i, p := range participants {
		if p.PipelineStep != "ready" {
			continue
		}
		for _, candidate := range m.Top5Candidates(p, participants) {
			// Only pair with those who come later in the list to avoid duplicates
			if candidate.PipelineStep == "ready" {
				found := false
				for _, existing := range pairs {
					if (existing[0].ID == p.ID && existing[1].ID == candidate.ID) ||
						(existing[0].ID == candidate.ID && existing[1].ID == p.ID) {
						found = true
						break
					}
				}
				if !found && i < len(participants)-1 {
					pairs = append(pairs, [2]*Participant{p, candidate})
				}
			}
		}
	}
	return pairs
}

// GreedyMatch pairs participants by maximum language/answer overlap.
// Returns a slice of participant pairs that have been matched.
func (m *Matcher) GreedyMatch(participants []*Participant) [][2]*Participant {
	// Make a copy to track matched participants
	type participantState struct {
		p      *Participant
		matched bool
	}
	states := make([]participantState, len(participants))
	for i, p := range participants {
		states[i] = participantState{p: p, matched: false}
	}

	var pairs [][2]*Participant

	// Try to match each participant with their best available candidate
	for i, state := range states {
		if state.matched {
			continue
		}

		// Find best unmatched candidate
		bestScore := -1
		bestIdx := -1
		for j, other := range states {
			if i == j || other.matched {
				continue
			}
			score := m.PairScore(state.p, other.p)
			if score > bestScore {
				bestScore = score
				bestIdx = j
			}
		}

		if bestIdx >= 0 {
			pairs = append(pairs, [2]*Participant{state.p, states[bestIdx].p})
			states[i].matched = true
			states[bestIdx].matched = true
		}
	}

	return pairs
}
