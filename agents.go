package main

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// AgentPipeline orchestrates the agent workflow for participant onboarding and matching.
// It coordinates GitHub profile fetching, persona generation, interview questions,
// and match scoring using LLM.
type AgentPipeline struct {
	db      *DB
	github  *GitHubClient
	mistral *MistralClient
	matcher *Matcher
	matchMu sync.Mutex              // Serializes matching operations to prevent race conditions
	llmCache map[string]*matchResult // In-memory cache for LLM match scores
	cacheMu  sync.Mutex              // Protects llmCache
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

// RunSetup fetches GitHub profile, creates persona, generates custom questions.
// Runs in a goroutine after participant registration.
func (a *AgentPipeline) RunSetup(participantID, githubHandle string) {
	a.RunSetupWithExtraAnswers(participantID, githubHandle, "", "", "", "", "", "", "")
}

// RunSetupWithExtraAnswers handles both GitHub and non-GitHub participants.
func (a *AgentPipeline) RunSetupWithExtraAnswers(
	participantID,
	githubHandle,
	languages,
	projectType,
	devEnvironment,
	weirdestBug,
	keyboard,
	keyboardOther,
	devEnvOther string) {
	var profile *GitHubProfile
	var isGitHubUser bool

	if languages == "" && projectType == "" {
		isGitHubUser = true
		a.db.LogActivity(fmt.Sprintf("🔍 Fetching @%s's GitHub profile...", githubHandle))
		var err error
		profile, err = a.github.FetchProfile(githubHandle)
		if err != nil {
			log.Printf("Failed to fetch GitHub profile for @%s: %v", githubHandle, err)
			// Continue with minimal profile
			profile = &GitHubProfile{Login: githubHandle, Name: githubHandle}
		}
		if profile == nil {
			profile = &GitHubProfile{Login: githubHandle, Name: githubHandle}
		}
	} else {
		isGitHubUser = false
		a.db.LogActivity(fmt.Sprintf("📝 Processing extra answers for non-GitHub user..."))

		var langSlice []string
		if languages != "" {
			json.Unmarshal([]byte(languages), &langSlice)
		}

		var devEnvSlice []string
		if devEnvironment != "" {
			json.Unmarshal([]byte(devEnvironment), &devEnvSlice)
		}

		if keyboard == "Other" {
			keyboard = keyboardOther
		}

		if len(devEnvSlice) == 1 && devEnvSlice[0] == "Other" {
			devEnvSlice = []string{devEnvOther}
		}

		profile = &GitHubProfile{
			ExtraAnswers: &ExtraAnswers{
				Languages:      langSlice,
				ProjectType:    projectType,
				DevEnvironment: devEnvSlice,
				WeirdestBug:    weirdestBug,
				Keyboard:       keyboard,
			},
		}
	}

	a.db.UpdatePipelineStep(participantID, "interviewing")

	var activityMsg string
	if isGitHubUser {
		activityMsg = fmt.Sprintf("🔍 Fetching @%s's GitHub profile...", githubHandle)
	} else {
		activityMsg = "📝 Preparing interview questions..."
	}
	a.db.LogActivity(activityMsg)

	var questions []Question
	if isGitHubUser {
		customQs, err := a.generateCustomQuestions(profile)
		if err != nil {
			log.Printf("Custom questions error: %v", err)
			// Use ExtraQuestions as fallback for consistency with non-GitHub users
			customQs = make([]string, len(ExtraQuestions))
			for i, q := range ExtraQuestions {
				customQs[i] = q.Text
			}
		}
		questions = append(FixedQuestions, a.stringsToQuestions(customQs, "custom")...)
	} else {
		questions = append(ExtraQuestions, a.filterFixedQuestions()...)
	}

	log.Printf("[DEBUG] RunSetup: About to update profile with %d questions for participant %s", len(questions), participantID)
	for i, q := range questions {
		log.Printf("[DEBUG] Question %d: ID=%s, Text=%s, Options=%v", i, q.ID, q.Text, q.Options)
	}
	a.db.UpdateProfile(participantID, profile, "", "", questions)

	if !isGitHubUser && profile.ExtraAnswers != nil {
		a.db.UpdateExtraAnswers(participantID, profile.ExtraAnswers)
	}

	a.db.UpdatePipelineStep(participantID, "interviewing")
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

	profile := *p.Profile
	answers := p.Answers
	if answers == nil {
		answers = map[string]string{}
	}

	extraAnswers := p.Extra

	completeProfile := a.buildCompleteProfile(&profile, extraAnswers, answers)

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

func (a *AgentPipeline) computeInterestsFromCompleteProfile(profile *CompleteProfile) map[string]interface{} {
	interests := map[string]interface{}{
		"languages": []string{},
		"tools":     []string{},
		"domains":   []string{},
	}

	if profile.GitHubProfile != nil {
		gp := profile.GitHubProfile
		interests["languages"] = gp.Languages
		interests["tools"] = gp.TopTopics
	} else if profile.ExtraAnswers != nil {
		ea := profile.ExtraAnswers
		interests["languages"] = ea.Languages
		interests["tools"] = ea.DevEnvironment
		if ea.ProjectType != "" {
			interests["domains"] = []string{ea.ProjectType}
		}
	}

	return interests
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

	if profile.GitHubProfile != nil && profile.GitHubProfile.Login != "" {
		parts = append(parts, fmt.Sprintf("GitHub: @%s", profile.GitHubProfile.Login))
		parts = append(parts, profile.GitHubProfile.Summary())
	} else if profile.ExtraAnswers != nil {
		ea := profile.ExtraAnswers
		if len(ea.Languages) > 0 {
			parts = append(parts, "Languages: "+strings.Join(ea.Languages, ", "))
		}
		if ea.ProjectType != "" {
			parts = append(parts, "Project type: "+ea.ProjectType)
		}
		if len(ea.DevEnvironment) > 0 {
			parts = append(parts, "Dev environment: "+strings.Join(ea.DevEnvironment, ", "))
		}
		if ea.WeirdestBug != "" {
			parts = append(parts, "Weirdest bug: "+ea.WeirdestBug)
		}
		if ea.Keyboard != "" {
			parts = append(parts, "Keyboard: "+ea.Keyboard)
		}
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

func (a *AgentPipeline) generateFallbackPersona(profile *GitHubProfile, isGitHubUser bool) *personaResult {
	toTitle := func(s string) string {
		if s == "" {
			return s
		}
		return strings.ToUpper(s[:1]) + s[1:]
	}

	if isGitHubUser {
		return &personaResult{
			Name:    "The " + toTitle(profile.Login),
			Tagline: "Mysterious coder. Ships things.",
		}
	}

	var name string
	if len(profile.ExtraAnswers.Languages) > 0 {
		name = "The " + toTitle(profile.ExtraAnswers.Languages[0]) + " Developer"
	} else {
		name = "The Mysterious Coder"
	}
	return &personaResult{
		Name:    name,
		Tagline: "Ships things without GitHub.",
	}
}

func (a *AgentPipeline) computeInterests(profile *GitHubProfile) map[string]interface{} {
	interests := map[string]interface{}{
		"languages": []string{},
		"tools":     []string{},
		"domains":   []string{},
	}

	if profile.ExtraAnswers != nil {
		ea := profile.ExtraAnswers
		interests["languages"] = ea.Languages
		interests["tools"] = ea.DevEnvironment
		if ea.ProjectType != "" {
			interests["domains"] = []string{ea.ProjectType}
		}
	} else {
		interests["languages"] = profile.Languages
		interests["tools"] = profile.TopTopics
	}

	return interests
}

func (a *AgentPipeline) generatePersona(profile *GitHubProfile) (*personaResult, error) {
	if a.mistral == nil {
		return nil, fmt.Errorf("mistral client not initialized")
	}
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

// getCachedMatchResult retrieves a cached match result or returns nil if not found
func (a *AgentPipeline) getCachedMatchResult(p1, p2 *Participant) *matchResult {
	// Initialize in-memory cache if nil
	if a.llmCache == nil {
		a.llmCache = make(map[string]*matchResult)
	}

	key := pairKey(p1, p2)

	// Check in-memory cache first
	a.cacheMu.Lock()
	if cached, exists := a.llmCache[key]; exists {
		a.cacheMu.Unlock()
		return cached
	}
	a.cacheMu.Unlock()

	// Check SQLite cache
	if a.db != nil {
		cacheEntry, exists := a.db.GetLLMCache(key)
		if exists {
			result := &matchResult{
				Score:       cacheEntry.Score,
				Reason:      cacheEntry.Reason,
				RedFlags:    strings.Split(cacheEntry.RedFlags, ","),
				GreenFlags:  strings.Split(cacheEntry.GreenFlags, ","),
				Icebreakers: strings.Split(cacheEntry.Icebreakers, ","),
			}
			// Store in in-memory cache for future access
			a.cacheMu.Lock()
			a.llmCache[key] = result
			a.cacheMu.Unlock()
			return result
		}
	}

	return nil
}

// cacheMatchResult stores a match result in both in-memory and SQLite caches
func (a *AgentPipeline) cacheMatchResult(p1, p2 *Participant, result *matchResult) {
	// Initialize in-memory cache if nil
	if a.llmCache == nil {
		a.llmCache = make(map[string]*matchResult)
	}

	key := pairKey(p1, p2)

	// Store in in-memory cache
	a.cacheMu.Lock()
	a.llmCache[key] = result
	a.cacheMu.Unlock()

	// Store in SQLite cache (best-effort)
	if a.db != nil {
		redFlags := strings.Join(result.RedFlags, ",")
		greenFlags := strings.Join(result.GreenFlags, ",")
		icebreakers := strings.Join(result.Icebreakers, ",")
		a.db.SetLLMCache(key, result.Score, result.Reason, redFlags, greenFlags, icebreakers)
	}
}

// clearLLMCache clears both in-memory and SQLite caches
func (a *AgentPipeline) clearLLMCache() {
	// Clear in-memory cache
	a.cacheMu.Lock()
	a.llmCache = make(map[string]*matchResult)
	a.cacheMu.Unlock()

	// Clear SQLite cache
	if a.db != nil {
		a.db.ClearLLMCache()
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

				// Check persistent cache first
				if cached := a.getCachedMatchResult(p1, p2); cached != nil {
					a.db.LogActivity(fmt.Sprintf("🤝 %s ↔ %s: %d%% (cached)", p1.PersonaName, p2.PersonaName, cached.Score))
					return
				}

				sem <- struct{}{}
				result, err := a.matcher.GenerateMatch(p1, p2)
				<-sem

				if err != nil {
					log.Printf("Match scoring error for %s/%s: %v", p1.GitHubHandle, p2.GitHubHandle, err)
					result = defaultMatchResult()
				}

				// Cache the result
				a.cacheMatchResult(p1, p2, result)

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
		if cached := a.getCachedMatchResult(p1, p2); cached != nil {
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
		if cached := a.getCachedMatchResult(p1, p2); cached != nil {
			// Use cached result
			finalPairs = append(finalPairs, fp)
		} else {
			// Generate new match
			result, err := a.matcher.GenerateMatch(p1, p2)
			if err != nil {
				log.Printf("Fallback match error for %s/%s: %v", p1.GitHubHandle, p2.GitHubHandle, err)
				result = defaultMatchResult()
			}
			a.cacheMatchResult(p1, p2, result)
			finalPairs = append(finalPairs, fp)
		}
	}

	// Store results for all final pairs
	for _, pair := range finalPairs {
		p1, p2 := pair[0], pair[1]
		// Get the result from persistent cache
		result := a.getCachedMatchResult(p1, p2)
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

func (a *AgentPipeline) generateMatch(p1, p2 *Participant) (*matchResult, error) {
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
	if a.github != nil && p1.GitHubHandle != "" && p2.GitHubHandle != "" {
		aFollowsB, bFollowsA := a.github.CheckMutualFollow(p1.GitHubHandle, p2.GitHubHandle)
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

func fmtInterests(interests map[string]interface{}) string {
	if len(interests) == 0 {
		return ""
	}
	var parts []string
	for category, items := range interests {
		if itemSlice, ok := items.([]string); ok && len(itemSlice) > 0 {
			parts = append(parts, category+": "+strings.Join(itemSlice, ", "))
		}
	}
	return strings.Join(parts, "; ")
}

func (a *AgentPipeline) stringsToQuestions(texts []string, prefix string) []Question {
	var questions []Question
	for i, text := range texts {
		questions = append(questions, Question{
			ID:      prefix + "_" + strconv.Itoa(i),
			Text:    text,
			Options: nil,
		})
	}
	return questions
}

func (a *AgentPipeline) filterFixedQuestions() []Question {
	var filtered []Question
	for _, q := range FixedQuestions {
		if q.ID != "fixed_1" {
			filtered = append(filtered, q)
		}
	}
	return filtered
}

func pairKey(a, b *Participant) string {
	if a.ID < b.ID {
		return a.ID + ":" + b.ID
	}
	return b.ID + ":" + a.ID
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

			// Check persistent cache first
			if cached := a.getCachedMatchResult(p1, p2); cached != nil {
				a.db.LogActivity(fmt.Sprintf("🤝 %s ↔ %s: %d%% (cached)", p1.PersonaName, p2.PersonaName, cached.Score))
				return
			}

			sem <- struct{}{}
			result, err := a.matcher.GenerateMatch(p1, p2)
			<-sem

			if err != nil {
				log.Printf("Match scoring error for %s/%s: %v", p1.GitHubHandle, p2.GitHubHandle, err)
				result = defaultMatchResult()
			}

			// Cache the result
			a.cacheMatchResult(p1, p2, result)

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
		if cached := a.getCachedMatchResult(newParticipant, candidate); cached != nil {
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
		result := a.getCachedMatchResult(newParticipant, bestMatch)
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

func extractJSON(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start == -1 || end == -1 || end <= start {
		return s
	}
	return s[start : end+1]
}
