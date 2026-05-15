package main

import (
	"encoding/json"
	"fmt"
)

// matchResult contains the LLM-generated compatibility assessment between two participants.
type matchResult struct {
	Score       int      `json:"score"`
	Reason      string   `json:"reason"`
	RedFlags    []string `json:"red_flags"`
	GreenFlags  []string `json:"green_flags"`
	Icebreakers []string `json:"icebreakers"`
}

// Matcher provides matching functionality for participants.
type Matcher struct {
	github  *GitHubClient
	mistral *MistralClient
}

// NewMatcher creates a new Matcher instance.
func NewMatcher(github *GitHubClient, mistral *MistralClient) *Matcher {
	return &Matcher{
		github:  github,
		mistral: mistral,
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

// GenerateMatch generates a compatibility assessment between two participants using the LLM.
func (m *Matcher) GenerateMatch(p1, p2 *Participant) (*matchResult, error) {
	system := `You are the matchmaker at a tech meetup blind date event.
Analyze two developers' profiles and produce a fun, humorous compatibility assessment.
Respond with ONLY valid JSON — no markdown:
{"score": <0-100>, "reason": "<one funny sentence max 80 chars>", "red_flags": ["...", "..."], "green_flags": ["...", "..."], "icebreakers": ["<question one can ask the other>", "<question>", "<question>"]}`

	p1Profile := p1.Profile
	if p1Profile == nil {
		p1Profile = &GitHubProfile{}
	}
	p2Profile := p2.Profile
	if p2Profile == nil {
		p2Profile = &GitHubProfile{}
	}

	p1Ans := p1.Answers
	if p1Ans == nil {
		p1Ans = map[string]string{}
	}
	p2Ans := p2.Answers
	if p2Ans == nil {
		p2Ans = map[string]string{}
	}

	p1Interests := p1.Interests
	if p1Interests == nil {
		p1Interests = map[string]interface{}{}
	}
	p2Interests := p2.Interests
	if p2Interests == nil {
		p2Interests = map[string]interface{}{}
	}

	followNote := ""
	if m.github != nil && p1.GitHubHandle != "" && p2.GitHubHandle != "" {
		aFollowsB, bFollowsA := m.github.CheckMutualFollow(p1.GitHubHandle, p2.GitHubHandle)
		switch {
		case aFollowsB && bFollowsA:
			followNote = fmt.Sprintf("\nNote: %s and %s already follow each other on GitHub!", p1.PersonaName, p2.PersonaName)
		case aFollowsB:
			followNote = fmt.Sprintf("\nNote: %s already follows %s on GitHub.", p1.PersonaName, p2.PersonaName)
		case bFollowsA:
			followNote = fmt.Sprintf("\nNote: %s already follows %s on GitHub.", p2.PersonaName, p1.PersonaName)
		}
	}

	interestsNote := ""
	if p1Interests != nil || p2Interests != nil {
		p1InterestsStr := fmtInterests(p1Interests)
		p2InterestsStr := fmtInterests(p2Interests)
		if p1InterestsStr != "" && p2InterestsStr != "" {
			interestsNote = fmt.Sprintf("\nInterests: %s | %s", p1InterestsStr, p2InterestsStr)
		} else if p1InterestsStr != "" {
			interestsNote = fmt.Sprintf("\nInterests: %s", p1InterestsStr)
		} else if p2InterestsStr != "" {
			interestsNote = fmt.Sprintf("\nInterests: %s", p2InterestsStr)
		}
	}

	user := fmt.Sprintf("Compare these two developers:\n\nDEVELOPER 1 (%s):\n%s\nInterview answers: %v%s\n\nDEVELOPER 2 (%s):\n%s\nInterview answers: %v%s",
		p1.PersonaName, p1Profile.Summary(), p1Ans, interestsNote,
		p2.PersonaName, p2Profile.Summary(), p2Ans, followNote,
	)

	response, err := m.mistral.Chat(system, user)
	if err != nil {
		return nil, err
	}

	var result matchResult
	if err := json.Unmarshal([]byte(extractJSON(response)), &result); err != nil {
		return nil, fmt.Errorf("match parse error: %v (raw: %s)", err, response)
	}
	return &result, nil
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
