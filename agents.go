package main

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
)

// AgentPipeline orchestrates the agent workflow for participant onboarding and matching.
// It coordinates GitHub profile fetching, persona generation, interview questions,
// and match scoring using LLM.
type AgentPipeline struct {
	db      *DB
	github  GitHubAPI
	mistral LLM
	matcher   *Matcher
	interview *Interview
	matchMu   sync.Mutex // Serializes matching operations to prevent race conditions
}

// NewAgentPipeline creates a new AgentPipeline with the given dependencies.
func NewAgentPipeline(db *DB, github GitHubAPI, mistral LLM, matcher *Matcher, interview *Interview) *AgentPipeline {
	return &AgentPipeline{
		db:        db,
		github:    github,
		mistral:   mistral,
		matcher:   matcher,
		interview: interview,
	}
}

type personaResult struct {
	Name    string `json:"name"`
	Tagline string `json:"tagline"`
}


// CompleteProfile combines all data about a participant for persona generation
type CompleteProfile struct {
	GitHubProfile    *GitHubProfile
	ExtraAnswers     *ExtraAnswers
	InterviewAnswers map[string]string
	Interests        map[string]interface{}
}

// RunSetup fetches the GitHub profile (GitHub users only) and starts the Interview.
// Runs in a goroutine after participant registration.
func (a *AgentPipeline) RunSetup(participantID, githubHandle string) {
	// Non-GitHub Users get a generated "no-github-" handle at registration.
	isGitHubUser := !strings.HasPrefix(githubHandle, "no-github-")

	profile := &GitHubProfile{}
	if isGitHubUser {
		profile = &GitHubProfile{Login: githubHandle, Name: githubHandle}
		a.db.LogActivity(fmt.Sprintf("🔍 Fetching @%s's GitHub profile...", githubHandle))
		fetched, err := a.github.FetchProfile(githubHandle)
		if err != nil {
			log.Printf("Failed to fetch GitHub profile for @%s: %v", githubHandle, err)
		} else if fetched != nil {
			profile = fetched
		}
	} else {
		a.db.LogActivity("📝 Processing non-GitHub user...")
	}
	a.db.LogActivity("📝 Preparing interview questions...")

	if err := a.interview.Start(participantID, profile, isGitHubUser); err != nil {
		log.Printf("Starting interview for %s failed: %v", participantID, err)
		return
	}
	a.db.LogActivity("✅ Ready for the interview!")
}

// RunFinalSetup generates persona and computes interests after all interview answers are collected.
func (a *AgentPipeline) RunFinalSetup(participantID string) {
	a.db.LogActivity(fmt.Sprintf("🎭 Crafting persona for participant %s...", participantID))

	p, err := a.db.GetParticipant(participantID)
	if err != nil {
		log.Printf("RunFinalSetup: participant not found: %v", err)
		return
	}

	if p.Profile == nil {
		log.Printf("RunFinalSetup: profile is nil for participant %s", participantID)
		return
	}

	a.db.UpdatePipelineStep(participantID, "creating_persona")

	profile := *p.Profile
	answers := p.Answers
	if answers == nil {
		answers = map[string]string{}
	}

	completeProfile := a.buildCompleteProfile(&profile, profile.ExtraAnswers, answers)

	persona, err := a.generatePersonaFromCompleteProfile(completeProfile)
	if err != nil {
		log.Printf("Persona generation error: %v", err)
		persona = a.generateFallbackPersonaFromCompleteProfile(completeProfile)
	}

	interests := a.computeInterestsFromCompleteProfile(completeProfile)

	a.db.UpdateProfile(participantID, &profile, persona.Name, persona.Tagline, p.Questions)
	a.db.UpdateInterests(participantID, interests)
	a.db.UpdatePipelineStep(participantID, "ready")
	a.db.LogActivity(fmt.Sprintf("✅ %s is ready for matching!", persona.Name))
}

func (a *AgentPipeline) buildCompleteProfile(profile *GitHubProfile, extraAnswers *ExtraAnswers, interviewAnswers map[string]string) *CompleteProfile {
	interests := a.computeInterestsFromCompleteProfile(&CompleteProfile{
		GitHubProfile:    profile,
		ExtraAnswers:     extraAnswers,
		InterviewAnswers: interviewAnswers,
	})

	return &CompleteProfile{
		GitHubProfile:    profile,
		ExtraAnswers:     extraAnswers,
		InterviewAnswers: interviewAnswers,
		Interests:        interests,
	}
}

// computeInterestsFromCompleteProfile prefers GitHub data and fills the gaps
// from ExtraAnswers, so both GitHub users and Non-GitHub Users get Interests.
func (a *AgentPipeline) computeInterestsFromCompleteProfile(profile *CompleteProfile) map[string]interface{} {
	languages, tools, domains := []string{}, []string{}, []string{}
	if gp := profile.GitHubProfile; gp != nil {
		languages = append(languages, gp.Languages...)
		tools = append(tools, gp.TopTopics...)
	}
	if ea := profile.ExtraAnswers; ea != nil {
		if len(languages) == 0 {
			languages = append(languages, ea.Languages...)
		}
		if len(tools) == 0 {
			tools = append(tools, ea.DevEnvironment...)
		}
		if ea.ProjectType != "" {
			domains = []string{ea.ProjectType}
		}
	}
	return map[string]interface{}{"languages": languages, "tools": tools, "domains": domains}
}

func (a *AgentPipeline) generatePersonaFromCompleteProfile(profile *CompleteProfile) (*personaResult, error) {
	if a.mistral == nil {
		return nil, fmt.Errorf("mistral client not initialized")
	}

	system := `You are a fun tech personality generator for a programming meetup blind date event.
Create a funny, tongue-in-cheek anonymous persona based on a developer's profile and interview answers.
Respond with ONLY a valid JSON object — no markdown, no backticks:
{"name": "The [Adjective] [Tech Noun]", "tagline": "<funny one-liner max 60 chars>"}`

	prompt := a.buildPersonaPrompt(profile)

	response, err := a.mistral.Chat(system, prompt)
	if err != nil {
		return nil, err
	}

	var result personaResult
	if err := json.Unmarshal([]byte(extractJSON(response)), &result); err != nil {
		return nil, fmt.Errorf("persona parse error: %v (raw: %s)", err, response)
	}
	return &result, nil
}

func (a *AgentPipeline) buildPersonaPrompt(profile *CompleteProfile) string {
	var parts []string
	gp := profile.GitHubProfile
	if gp == nil {
		gp = &GitHubProfile{}
	}
	if profile.ExtraAnswers != nil && gp.ExtraAnswers == nil {
		withExtra := *gp
		withExtra.ExtraAnswers = profile.ExtraAnswers
		gp = &withExtra
	}
	if summary := gp.Summary(); summary != "" {
		parts = append(parts, summary)
	}

	parts = append(parts, "\nInterview answers:")
	for qid, answer := range profile.InterviewAnswers {
		parts = append(parts, fmt.Sprintf("Q[%s]: %s", qid, answer))
	}

	return strings.Join(parts, "\n")
}

func (a *AgentPipeline) generateFallbackPersonaFromCompleteProfile(profile *CompleteProfile) *personaResult {
	toTitle := func(s string) string {
		if s == "" {
			return s
		}
		return strings.ToUpper(s[:1]) + s[1:]
	}

	if profile.GitHubProfile != nil && profile.GitHubProfile.Login != "" {
		return &personaResult{
			Name:    "The " + toTitle(profile.GitHubProfile.Login),
			Tagline: "Mysterious coder. Ships things.",
		}
	}

	var name string
	if profile.ExtraAnswers != nil && len(profile.ExtraAnswers.Languages) > 0 {
		name = "The " + toTitle(profile.ExtraAnswers.Languages[0]) + " Developer"
	} else if profile.InterviewAnswers != nil {
		if lang, ok := profile.InterviewAnswers["fixed_1"]; ok && lang != "" {
			name = "The " + toTitle(lang) + " Developer"
		} else {
			name = "The Mysterious Coder"
		}
	} else {
		name = "The Mysterious Coder"
	}
	return &personaResult{
		Name:    name,
		Tagline: "Ships things.",
	}
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
	candidatePairs := a.matcher.CollectCandidatePairs(participants)
	a.db.LogActivity(fmt.Sprintf("🔍 Evaluating %d candidate pairs...", len(candidatePairs)))

	// Phase 2: LLM-score all candidate pairs, concurrency=2, with persistent caching
	var wg sync.WaitGroup
	sem := make(chan struct{}, 2)

		for _, pair := range candidatePairs {
			wg.Add(1)
			go func(p1, p2 *Participant) {
				defer wg.Done()

				sem <- struct{}{}
				result := a.score(p1, p2)
				<-sem

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
		p1, p2 := pair[0], pair[1]
		// Check persistent cache for this pair
		if cached := a.score(p1, p2); cached != nil {
			scored = append(scored, llmPair{pair, cached.Score})
		} else {
			// If not in persistent cache, it should have been scored in Phase 2
			// This shouldn't happen, but handle it gracefully
			log.Printf("Warning: pair %s:%s not scored in Phase 2", p1.ID, p2.ID)
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
	for _, fp := range a.matcher.GreedyMatch(unmatched) {
		p1, p2 := fp[0], fp[1]
		// Check persistent cache first
		if cached := a.score(p1, p2); cached != nil {
			// Use cached result
			finalPairs = append(finalPairs, fp)
		} else {
			a.score(p1, p2)
			finalPairs = append(finalPairs, fp)
		}
	}

	// Store results for all final pairs
	for _, pair := range finalPairs {
		p1, p2 := pair[0], pair[1]
		// Get the result from persistent cache
		result := a.score(p1, p2)
		if result == nil {
			// Shouldn't happen, but fallback to default
			log.Printf("Warning: no cached result for final pair %s:%s", p1.ID, p2.ID)
			result = defaultMatchResult()
		}
		redJSON, err := json.Marshal(result.RedFlags)
		if err != nil {
			log.Printf("Failed to marshal red flags: %v", err)
			continue
		}
		greenJSON, err := json.Marshal(result.GreenFlags)
		if err != nil {
			log.Printf("Failed to marshal green flags: %v", err)
			continue
		}
		iceJSON, err := json.Marshal(result.Icebreakers)
		if err != nil {
			log.Printf("Failed to marshal icebreakers: %v", err)
			continue
		}
		a.db.SetMatched(p1.ID, p2.ID, result.Score, result.Reason, string(redJSON), string(greenJSON), string(iceJSON))
		a.db.SetMatched(p2.ID, p1.ID, result.Score, result.Reason, string(redJSON), string(greenJSON), string(iceJSON))
		a.db.LogActivity(fmt.Sprintf("💘 %s ↔ %s (%d%%)", p1.PersonaName, p2.PersonaName, result.Score))
	}

	a.db.SetPhase("revealed")
	a.db.LogActivity("🎉 All matches revealed!")
	return nil
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
		allParticipants, err := a.db.GetAllParticipants()
		if err != nil {
			return fmt.Errorf("GetAllParticipants: %w", err)
		}
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
	candidates := a.matcher.Top5Candidates(newParticipant, pool)
	if len(candidates) == 0 {
		return fmt.Errorf("no candidates found for %s", newParticipant.PersonaName)
	}

	a.db.LogActivity(fmt.Sprintf("🔍 Evaluating %d candidates for %s...", len(candidates), newParticipant.PersonaName))

	// Phase 2: LLM-score all candidate pairs
	var wg sync.WaitGroup
	sem := make(chan struct{}, 2)

	for _, candidate := range candidates {
		wg.Add(1)
		go func(p1, p2 *Participant) {
			defer wg.Done()

			sem <- struct{}{}
			result := a.score(p1, p2)
			<-sem

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
		// Get the cached result for this pair
		if cached := a.score(newParticipant, candidate); cached != nil {
			scored = append(scored, scoredCandidate{candidate, cached.Score})
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

		// Get the match result (from persistent cache or generate)
		result := a.score(newParticipant, bestMatch)
		if result == nil {
			// Shouldn't happen since we scored all candidates in Phase 2, but fallback
			log.Printf("Warning: no cached result for best match %s:%s", newParticipant.ID, bestMatch.ID)
			result = defaultMatchResult()
		}

		// Store results for both participants
		redJSON, err := json.Marshal(result.RedFlags)
		if err != nil {
			log.Printf("Failed to marshal red flags: %v", err)
			return nil
		}
		greenJSON, err := json.Marshal(result.GreenFlags)
		if err != nil {
			log.Printf("Failed to marshal green flags: %v", err)
			return nil
		}
		iceJSON, err := json.Marshal(result.Icebreakers)
		if err != nil {
			log.Printf("Failed to marshal icebreakers: %v", err)
			return nil
		}

		a.db.SetMatched(newParticipant.ID, bestMatch.ID, result.Score, result.Reason, string(redJSON), string(greenJSON), string(iceJSON))
		a.db.SetMatched(bestMatch.ID, newParticipant.ID, result.Score, result.Reason, string(redJSON), string(greenJSON), string(iceJSON))

		a.db.LogActivity(fmt.Sprintf("💘 %s ↔ %s (%d%%)", newParticipant.PersonaName, bestMatch.PersonaName, result.Score))
	} else {
		a.db.LogActivity(fmt.Sprintf("⏳ %s is ready but no match available yet", newParticipant.PersonaName))
	}

	return nil
}

// score returns the Matcher's assessment of a Pair, or the default result when scoring fails.
func (a *AgentPipeline) score(p1, p2 *Participant) *matchResult {
	result, err := a.matcher.ScorePair(p1, p2)
	if err != nil {
		log.Printf("Match scoring error for %s/%s: %v", p1.GitHubHandle, p2.GitHubHandle, err)
		return defaultMatchResult()
	}
	return result
}

func extractJSON(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start == -1 || end == -1 || end <= start {
		return s
	}
	return s[start : end+1]
}
