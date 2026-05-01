package main

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
)

type AgentPipeline struct {
	db      *DB
	github  *GitHubClient
	mistral *MistralClient
	matchMu sync.Mutex // Serializes matching operations to prevent race conditions
}

type personaResult struct {
	Name    string `json:"name"`
	Tagline string `json:"tagline"`
}

type matchResult struct {
	Score       int      `json:"score"`
	Reason      string   `json:"reason"`
	RedFlags    []string `json:"red_flags"`
	GreenFlags  []string `json:"green_flags"`
	Icebreakers []string `json:"icebreakers"`
}

// RunSetup fetches GitHub profile, creates persona, generates custom questions.
// Runs in a goroutine after participant registration.
func (a *AgentPipeline) RunSetup(participantID, githubHandle string) {
	a.db.LogActivity(fmt.Sprintf("🔍 Fetching @%s's GitHub profile...", githubHandle))

	profile, err := a.github.FetchProfile(githubHandle)
	if err != nil {
		log.Printf("GitHub fetch error for %s: %v", githubHandle, err)
		profile = &GitHubProfile{Login: githubHandle, Name: githubHandle}
	}
	profileJSON, _ := json.Marshal(profile)

	a.db.UpdatePipelineStep(participantID, "creating_persona")
	a.db.LogActivity(fmt.Sprintf("🎭 Crafting persona for @%s...", githubHandle))

	persona, err := a.generatePersona(profile)
	if err != nil {
		log.Printf("Persona generation error: %v", err)
		persona = &personaResult{
			Name:    "The " + strings.Title(githubHandle),
			Tagline: "Mysterious coder. Ships things.",
		}
	}

	a.db.LogActivity(fmt.Sprintf("🤔 Preparing interview for %s...", persona.Name))

	customQs, err := a.generateCustomQuestions(profile)
	if err != nil {
		log.Printf("Custom questions error: %v", err)
		customQs = []string{
			"What's your most controversial tech opinion?",
			"Worst bug you've ever shipped to production?",
			"Coffee or tea while coding?",
		}
	}
	customQsJSON, _ := json.Marshal(customQs)

	a.db.UpdateProfile(participantID, string(profileJSON), persona.Name, persona.Tagline, string(customQsJSON))
	a.db.UpdatePipelineStep(participantID, "interviewing")
	a.db.LogActivity(fmt.Sprintf("✅ %s is ready for the interview!", persona.Name))
}

func (a *AgentPipeline) generatePersona(profile *GitHubProfile) (*personaResult, error) {
	system := `You are a fun tech personality generator for a programming meetup blind date event.
Create a funny, tongue-in-cheek anonymous persona based on a GitHub profile.
Respond with ONLY a valid JSON object — no markdown, no backticks:
{"name": "The [Adjective] [Tech Noun]", "tagline": "<funny one-liner max 60 chars>"}`

	response, err := a.mistral.Chat(system, "Create a persona for:\n\n"+profile.Summary())
	if err != nil {
		return nil, err
	}

	var result personaResult
	if err := json.Unmarshal([]byte(extractJSON(response)), &result); err != nil {
		return nil, fmt.Errorf("persona parse error: %v (raw: %s)", err, response)
	}
	return &result, nil
}

func (a *AgentPipeline) generateCustomQuestions(profile *GitHubProfile) ([]string, error) {
	system := `You are an interviewer at a tech meetup blind date event.
Generate 3 fun, opinionated questions tailored to this developer's GitHub profile.
Questions should be conversational and tech-related.
Respond with ONLY valid JSON — no markdown:
{"questions": ["...", "...", "..."]}`

	response, err := a.mistral.Chat(system, "Generate 3 personalized questions for:\n\n"+profile.Summary())
	if err != nil {
		return nil, err
	}

	var result struct {
		Questions []string `json:"questions"`
	}
	if err := json.Unmarshal([]byte(extractJSON(response)), &result); err != nil {
		return nil, fmt.Errorf("questions parse error: %v", err)
	}
	if len(result.Questions) == 0 {
		return nil, fmt.Errorf("empty questions")
	}
	return result.Questions, nil
}

// RunMatching pairs all ready participants and generates match results via Mistral.
// Phase 1: heuristic top-5 per participant → candidate pairs.
// Phase 2: LLM-score every unique candidate pair (concurrency=2, cached).
// Phase 3: greedy assignment from LLM scores.
func (a *AgentPipeline) RunMatching() error {
	a.matchMu.Lock()
	defer a.matchMu.Unlock()

	a.db.SetPhase("matching")
	a.db.LogActivity("🔮 The matchmaker agents are at work...")

	participants, err := a.db.GetAllByStep("ready")
	if err != nil {
		return err
	}
	if len(participants) < 2 {
		return fmt.Errorf("need at least 2 ready participants, got %d", len(participants))
	}

	// Phase 1: heuristic top-5 narrows the candidate pool
	candidatePairs := collectCandidatePairs(participants)
	a.db.LogActivity(fmt.Sprintf("🔍 Evaluating %d candidate pairs...", len(candidatePairs)))

	// Phase 2: LLM-score all candidate pairs, concurrency=2, in-memory cache
	cache := map[string]*matchResult{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 2)

	for _, pair := range candidatePairs {
		wg.Add(1)
		go func(p1, p2 *Participant) {
			defer wg.Done()
			key := pairKey(p1, p2)

			mu.Lock()
			if _, exists := cache[key]; exists {
				mu.Unlock()
				return
			}
			mu.Unlock()

			sem <- struct{}{}
			result, err := a.generateMatch(p1, p2)
			<-sem

			if err != nil {
				log.Printf("Match scoring error for %s/%s: %v", p1.GitHubHandle, p2.GitHubHandle, err)
				result = defaultMatchResult()
			}

			mu.Lock()
			cache[key] = result
			mu.Unlock()

			a.db.LogActivity(fmt.Sprintf("🤝 %s ↔ %s: %d%%", p1.PersonaName, p2.PersonaName, result.Score))
		}(pair[0], pair[1])
	}
	wg.Wait()

	// Phase 3: greedy assignment using LLM scores
	type llmPair struct {
		pair  [2]*Participant
		score int
	}
	var scored []llmPair
	for _, pair := range candidatePairs {
		if r := cache[pairKey(pair[0], pair[1])]; r != nil {
			scored = append(scored, llmPair{pair, r.Score})
		}
	}
	sort.Slice(scored, func(a, b int) bool { return scored[a].score > scored[b].score })

	paired := map[string]bool{}
	var finalPairs [][2]*Participant
	for _, lp := range scored {
		p1, p2 := lp.pair[0], lp.pair[1]
		if !paired[p1.ID] && !paired[p2.ID] {
			finalPairs = append(finalPairs, lp.pair)
			paired[p1.ID] = true
			paired[p2.ID] = true
		}
	}

	// Fallback: any participant not covered by top-5 overlap gets heuristic-paired
	var unmatched []*Participant
	for _, p := range participants {
		if !paired[p.ID] {
			unmatched = append(unmatched, p)
		}
	}
	for _, fp := range greedyMatch(unmatched) {
		p1, p2 := fp[0], fp[1]
		result, err := a.generateMatch(p1, p2)
		if err != nil {
			log.Printf("Fallback match error for %s/%s: %v", p1.GitHubHandle, p2.GitHubHandle, err)
			result = defaultMatchResult()
		}
		cache[pairKey(p1, p2)] = result
		finalPairs = append(finalPairs, fp)
	}

	// Store results for all final pairs
	for _, pair := range finalPairs {
		p1, p2 := pair[0], pair[1]
		result := cache[pairKey(p1, p2)]
		redJSON, _ := json.Marshal(result.RedFlags)
		greenJSON, _ := json.Marshal(result.GreenFlags)
		iceJSON, _ := json.Marshal(result.Icebreakers)
		a.db.SetMatched(p1.ID, p2.ID, result.Score, result.Reason, string(redJSON), string(greenJSON), string(iceJSON))
		a.db.SetMatched(p2.ID, p1.ID, result.Score, result.Reason, string(redJSON), string(greenJSON), string(iceJSON))
		a.db.LogActivity(fmt.Sprintf("💘 %s ↔ %s (%d%%)", p1.PersonaName, p2.PersonaName, result.Score))
	}

	a.db.SetPhase("revealed")
	a.db.LogActivity("🎉 All matches revealed!")
	return nil
}

func (a *AgentPipeline) generateMatch(p1, p2 *Participant) (*matchResult, error) {
	system := `You are the matchmaker at a tech meetup blind date event.
Analyze two developers' profiles and produce a fun, humorous compatibility assessment.
Respond with ONLY valid JSON — no markdown:
{"score": <0-100>, "reason": "<one funny sentence max 80 chars>", "red_flags": ["...", "..."], "green_flags": ["...", "..."], "icebreakers": ["...", "...", "..."]}`

	var p1Profile, p2Profile GitHubProfile
	json.Unmarshal([]byte(p1.ProfileJSON), &p1Profile)
	json.Unmarshal([]byte(p2.ProfileJSON), &p2Profile)

	var p1Ans, p2Ans map[string]string
	json.Unmarshal([]byte(p1.AnswersJSON), &p1Ans)
	json.Unmarshal([]byte(p2.AnswersJSON), &p2Ans)

	user := fmt.Sprintf(`Compare these two developers:

DEVELOPER 1 (%s):
%s
Interview answers: %v

DEVELOPER 2 (%s):
%s
Interview answers: %v`,
		p1.PersonaName, p1Profile.Summary(), p1Ans,
		p2.PersonaName, p2Profile.Summary(), p2Ans,
	)

	response, err := a.mistral.Chat(system, user)
	if err != nil {
		return nil, err
	}

	var result matchResult
	if err := json.Unmarshal([]byte(extractJSON(response)), &result); err != nil {
		return nil, fmt.Errorf("match parse error: %v (raw: %s)", err, response)
	}
	return &result, nil
}

func pairKey(a, b *Participant) string {
	if a.ID < b.ID {
		return a.ID + ":" + b.ID
	}
	return b.ID + ":" + a.ID
}

func top5Candidates(p *Participant, all []*Participant) []*Participant {
	type scored struct {
		participant *Participant
		score       int
	}
	var candidates []scored
	for _, other := range all {
		if other.ID == p.ID {
			continue
		}
		candidates = append(candidates, scored{other, pairScore(p, other)})
	}
	sort.Slice(candidates, func(a, b int) bool { return candidates[a].score > candidates[b].score })
	k := 5
	if len(candidates) < k {
		k = len(candidates)
	}
	result := make([]*Participant, k)
	for i := range k {
		result[i] = candidates[i].participant
	}
	return result
}

func collectCandidatePairs(participants []*Participant) [][2]*Participant {
	seen := map[string]bool{}
	var pairs [][2]*Participant
	for _, p := range participants {
		for _, candidate := range top5Candidates(p, participants) {
			key := pairKey(p, candidate)
			if !seen[key] {
				seen[key] = true
				pairs = append(pairs, [2]*Participant{p, candidate})
			}
		}
	}
	return pairs
}

func defaultMatchResult() *matchResult {
	return &matchResult{
		Score:       42,
		Reason:      "The algorithm has spoken. We cannot explain.",
		RedFlags:    []string{},
		GreenFlags:  []string{"You're both here tonight"},
		Icebreakers: []string{"What brings you to this meetup?", "What are you currently building?", "Best tech talk you've seen recently?"},
	}
}

// greedyMatch pairs participants by maximum language/answer overlap.
func greedyMatch(participants []*Participant) [][2]*Participant {
	n := len(participants)
	paired := make([]bool, n)

	type scoredPair struct{ i, j, score int }
	var all []scoredPair
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			all = append(all, scoredPair{i, j, pairScore(participants[i], participants[j])})
		}
	}
	sort.Slice(all, func(a, b int) bool { return all[a].score > all[b].score })

	var pairs [][2]*Participant
	for _, sp := range all {
		if !paired[sp.i] && !paired[sp.j] {
			pairs = append(pairs, [2]*Participant{participants[sp.i], participants[sp.j]})
			paired[sp.i] = true
			paired[sp.j] = true
		}
	}
	return pairs
}

func pairScore(a, b *Participant) int {
	score := 0

	var aP, bP GitHubProfile
	json.Unmarshal([]byte(a.ProfileJSON), &aP)
	json.Unmarshal([]byte(b.ProfileJSON), &bP)

	aLangs := map[string]bool{}
	for _, l := range aP.Languages {
		aLangs[l] = true
	}
	for _, l := range bP.Languages {
		if aLangs[l] {
			score += 3
		}
	}

	var aAns, bAns map[string]string
	json.Unmarshal([]byte(a.AnswersJSON), &aAns)
	json.Unmarshal([]byte(b.AnswersJSON), &bAns)

	for k, av := range aAns {
		if bv, ok := bAns[k]; ok && av == bv {
			score++
		}
	}
	return score
}

// RunContinuousMatching matches a single new ready participant against the existing pool of ready, unmatched participants.
// Uses the same 3-phase algorithm: heuristic top-5, LLM scoring, greedy selection.
func (a *AgentPipeline) RunContinuousMatching(newParticipant *Participant) error {
	a.matchMu.Lock()
	defer a.matchMu.Unlock()

	a.db.LogActivity(fmt.Sprintf("🔮 Matching %s against existing pool...", newParticipant.PersonaName))

	// Get all ready participants who haven't been matched yet (excluding the new one)
	others, err := a.db.GetReadyUnmatched()
	if err != nil {
		return err
	}

	// Filter out the new participant if somehow included
	var pool []*Participant
	for _, p := range others {
		if p.ID != newParticipant.ID {
			pool = append(pool, p)
		}
	}

	breakingExistingMatch := false
	if len(pool) == 0 {
		// All existing participants are matched — consider breaking the weakest pair
		allParticipants, _ := a.db.GetAllParticipants()
		var matched []*Participant
		for _, p := range allParticipants {
			if p.PipelineStep == "matched" && p.MatchedWith != "" && p.ID != newParticipant.ID {
				matched = append(matched, p)
			}
		}
		if len(matched) == 0 {
			a.db.LogActivity(fmt.Sprintf("⏳ %s is ready but no match available yet", newParticipant.PersonaName))
			return nil
		}
		a.db.LogActivity(fmt.Sprintf("🔄 All slots filled — evaluating if %s fits better somewhere...", newParticipant.PersonaName))
		pool = matched
		breakingExistingMatch = true
	}

	// Phase 1: Get top-5 candidates from the pool for the new participant
	candidates := top5Candidates(newParticipant, pool)
	if len(candidates) == 0 {
		return fmt.Errorf("no candidates found for %s", newParticipant.PersonaName)
	}

	a.db.LogActivity(fmt.Sprintf("🔍 Evaluating %d candidates for %s...", len(candidates), newParticipant.PersonaName))

	// Phase 2: LLM-score all candidate pairs
	cache := map[string]*matchResult{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 2)

	for _, candidate := range candidates {
		wg.Add(1)
		go func(p1, p2 *Participant) {
			defer wg.Done()
			key := pairKey(p1, p2)

			mu.Lock()
			if _, exists := cache[key]; exists {
				mu.Unlock()
				return
			}
			mu.Unlock()

			sem <- struct{}{}
			result, err := a.generateMatch(p1, p2)
			<-sem

			if err != nil {
				log.Printf("Match scoring error for %s/%s: %v", p1.GitHubHandle, p2.GitHubHandle, err)
				result = defaultMatchResult()
			}

			mu.Lock()
			cache[key] = result
			mu.Unlock()

			a.db.LogActivity(fmt.Sprintf("🤝 %s ↔ %s: %d%%", p1.PersonaName, p2.PersonaName, result.Score))
		}(newParticipant, candidate)
	}
	wg.Wait()

	// Phase 3: Pick the best scoring candidate that is still unmatched
	type scoredCandidate struct {
		participant *Participant
		score       int
	}
	var scored []scoredCandidate
	for _, candidate := range candidates {
		if r := cache[pairKey(newParticipant, candidate)]; r != nil {
			scored = append(scored, scoredCandidate{candidate, r.Score})
		}
	}

	// Sort by score descending
	sort.Slice(scored, func(a, b int) bool { return scored[a].score > scored[b].score })

	// Find the best candidate — if breaking an existing match, any matched candidate is valid
	var bestMatch *Participant
	for _, sc := range scored {
		candidate, err := a.db.GetParticipant(sc.participant.ID)
		if err != nil {
			continue
		}
		if breakingExistingMatch || (candidate.MatchedWith == "" && candidate.PipelineStep == "ready") {
			bestMatch = candidate
			break
		}
	}

	if bestMatch == nil && !breakingExistingMatch {
		// All candidates were already matched, try heuristic fallback
		a.db.LogActivity(fmt.Sprintf("⚠️ All candidates for %s were already matched, trying fallback...", newParticipant.PersonaName))
		for _, p := range pool {
			candidate, err := a.db.GetParticipant(p.ID)
			if err != nil {
				continue
			}
			if candidate.MatchedWith == "" && candidate.PipelineStep == "ready" {
				bestMatch = candidate
				break
			}
		}
	}

	if bestMatch != nil {
		// If the best match was already paired, break that pair first
		if bestMatch.MatchedWith != "" {
			formerPartnerID := bestMatch.MatchedWith
			a.db.UnmatchParticipant(bestMatch.ID)
			a.db.UnmatchParticipant(formerPartnerID)
			a.db.LogActivity(fmt.Sprintf("🔄 Breaking %s's previous match to accommodate %s", bestMatch.PersonaName, newParticipant.PersonaName))
		}

		// Get the match result (from cache or generate)
		result := cache[pairKey(newParticipant, bestMatch)]
		if result == nil {
			result = defaultMatchResult()
		}

		// Store results for both participants
		redJSON, _ := json.Marshal(result.RedFlags)
		greenJSON, _ := json.Marshal(result.GreenFlags)
		iceJSON, _ := json.Marshal(result.Icebreakers)

		a.db.SetMatched(newParticipant.ID, bestMatch.ID, result.Score, result.Reason, string(redJSON), string(greenJSON), string(iceJSON))
		a.db.SetMatched(bestMatch.ID, newParticipant.ID, result.Score, result.Reason, string(redJSON), string(greenJSON), string(iceJSON))

		a.db.LogActivity(fmt.Sprintf("💘 %s ↔ %s (%d%%)", newParticipant.PersonaName, bestMatch.PersonaName, result.Score))
	} else {
		a.db.LogActivity(fmt.Sprintf("⏳ %s is ready but no match available yet", newParticipant.PersonaName))
	}

	return nil
}

func extractJSON(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start == -1 || end == -1 || end <= start {
		return s
	}
	return s[start : end+1]
}
