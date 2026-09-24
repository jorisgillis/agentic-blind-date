package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
)

// AgentPipeline orchestrates the agent workflow for participant onboarding and matching.
// It coordinates GitHub profile fetching, persona generation, interview questions,
// and match scoring using LLM.
type AgentPipeline struct {
	db        *DB
	github    GitHubAPI
	llm       LLM
	matcher   *Matcher
	interview *Interview
	relations *Relationships
	matchMu   sync.Mutex // Serializes matching operations to prevent race conditions
}

// NewAgentPipeline creates a new AgentPipeline with the given dependencies.
func NewAgentPipeline(db *DB, github GitHubAPI, llm LLM, matcher *Matcher, interview *Interview, relations *Relationships) *AgentPipeline {
	return &AgentPipeline{
		db:        db,
		github:    github,
		llm:       llm,
		matcher:   matcher,
		interview: interview,
		relations: relations,
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

	a.db.SetPersona(participantID, persona.Name, persona.Tagline)
	a.db.UpdateInterests(participantID, interests)
	a.db.UpdatePipelineStep(participantID, "ready")
	a.db.LogActivity(fmt.Sprintf("✅ %s is ready for matching!", persona.Name))

	// Continuous Matching: every Participant who becomes ready is matched right away.
	ready, err := a.db.GetParticipant(participantID)
	if err != nil {
		log.Printf("RunFinalSetup: reloading %s for matching: %v", participantID, err)
		return
	}
	if err := a.RunContinuousMatching(ready); err != nil {
		log.Printf("Continuous matching for %s failed: %v", participantID, err)
	}
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
	system := `You are a fun tech personality generator for a programming meetup blind date event.
Create a funny, tongue-in-cheek anonymous persona based on a developer's profile and interview answers.
Respond with ONLY a valid JSON object — no markdown, no backticks:
{"name": "The [Adjective] [Tech Noun]", "tagline": "<funny one-liner max 60 chars>"}`

	prompt := a.buildPersonaPrompt(profile)

	response, err := a.llm.Chat(system, prompt)
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

// Rematch breaks every Match and pairs all ready Participants again, as one
// matching operation: Continuous Matching cannot interleave with it.
func (a *AgentPipeline) Rematch() error {
	a.matchMu.Lock()
	defer a.matchMu.Unlock()

	if err := a.relations.UnpairAll(); err != nil {
		return err
	}
	a.db.LogActivity("🔮 The matchmaker agents are at work...")

	participants, err := a.db.GetAllByStep("ready")
	if err != nil {
		return err
	}
	if len(participants) < 2 {
		return fmt.Errorf("need at least 2 ready participants, got %d", len(participants))
	}

	for _, m := range a.matcher.MatchPool(participants) {
		if _, err := a.storeMatch(m); err != nil {
			return err
		}
	}

	a.db.LogActivity("💞 Rematch complete")
	return nil
}

// storeMatch records a Match through the Relationship module and returns the
// Participants it displaced from earlier Matches.
func (a *AgentPipeline) storeMatch(m Match) ([]string, error) {
	displaced, err := a.relations.Pair(m)
	if err != nil {
		return nil, fmt.Errorf("storing match %s ↔ %s: %w", m.A.ID, m.B.ID, err)
	}
	a.db.LogActivity(fmt.Sprintf("💘 %s ↔ %s (%d%%)", m.A.PersonaName, m.B.PersonaName, m.Result.Score))
	return displaced, nil
}

// RunContinuousMatching matches a Participant who just became ready against the
// other ready and matched Participants, breaking a weaker Match when needed.
func (a *AgentPipeline) RunContinuousMatching(newcomer *Participant) error {
	a.matchMu.Lock()
	defer a.matchMu.Unlock()

	a.db.LogActivity(fmt.Sprintf("🔮 Matching %s against existing pool...", newcomer.PersonaName))

	all, err := a.db.GetAllParticipants()
	if err != nil {
		return fmt.Errorf("GetAllParticipants: %w", err)
	}
	var others []*Participant
	for _, p := range all {
		if p.ID != newcomer.ID && (p.PipelineStep == "ready" || p.PipelineStep == "matched") {
			others = append(others, p)
		}
	}

	m := a.matcher.MatchNewcomer(newcomer, others)
	if m == nil {
		a.db.LogActivity(fmt.Sprintf("⏳ %s is ready but no match available yet", newcomer.PersonaName))
		return nil
	}
	displaced, err := a.storeMatch(*m)
	if err != nil {
		return err
	}
	if len(displaced) > 0 {
		a.db.LogActivity(fmt.Sprintf("🔄 %s took over %s's previous match", newcomer.PersonaName, m.B.PersonaName))
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
