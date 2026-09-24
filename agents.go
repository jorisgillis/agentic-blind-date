package main

import (
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
	matcher   *Matcher
	interview *Interview
	relations *Relationships
	personas  *Personas
	matchMu   sync.Mutex // Serializes matching operations to prevent race conditions
}

// NewAgentPipeline creates a new AgentPipeline with the given dependencies.
func NewAgentPipeline(db *DB, github GitHubAPI, matcher *Matcher, interview *Interview, relations *Relationships, personas *Personas) *AgentPipeline {
	return &AgentPipeline{
		db:        db,
		github:    github,
		matcher:   matcher,
		interview: interview,
		relations: relations,
		personas:  personas,
	}
}

// RunSetup fetches the GitHub profile (GitHub users only) and starts the Interview.
// Runs in a goroutine after participant registration.
func (a *AgentPipeline) RunSetup(participantID string) {
	p, err := a.db.GetParticipant(participantID)
	if err != nil {
		log.Printf("RunSetup: participant %s not found: %v", participantID, err)
		return
	}

	profile := &GitHubProfile{}
	if p.HasGitHub {
		handle := p.GitHubHandle
		profile = &GitHubProfile{Login: handle, Name: handle}
		a.db.LogActivity(fmt.Sprintf("🔍 Fetching @%s's GitHub profile...", handle))
		fetched, err := a.github.FetchProfile(handle)
		if err != nil {
			log.Printf("Failed to fetch GitHub profile for @%s: %v", handle, err)
		} else if fetched != nil {
			profile = fetched
		}
	} else {
		a.db.LogActivity("📝 Processing non-GitHub user...")
	}
	a.db.LogActivity("📝 Preparing interview questions...")

	if err := a.interview.Start(p, profile); err != nil {
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

	persona := a.personas.Create(p)
	interests := interestsOf(p.Profile)

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

// interestsOf computes a Participant's Interests, preferring GitHub data and
// filling the gaps from ExtraAnswers, so Non-GitHub Users get Interests too.
func interestsOf(profile *GitHubProfile) map[string]interface{} {
	languages, tools, domains := []string{}, []string{}, []string{}
	languages = append(languages, profile.Languages...)
	tools = append(tools, profile.TopTopics...)
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
// other ready and matched Participants, taking over a weaker Match when needed.
// A partner displaced by a take-over is matched straight away in the same way.
// Participants already paired in this chain are not taken over again, so the
// chain ends.
func (a *AgentPipeline) RunContinuousMatching(newcomer *Participant) error {
	a.matchMu.Lock()
	defer a.matchMu.Unlock()

	inChain := map[string]bool{}
	queue := []string{newcomer.ID}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		displaced, err := a.matchOne(id, inChain)
		if err != nil {
			return err
		}
		queue = append(queue, displaced...)
	}
	return nil
}

// matchOne finds a partner for one Participant, skipping those paired earlier
// in the chain, and returns whoever the new Match displaced.
func (a *AgentPipeline) matchOne(id string, inChain map[string]bool) ([]string, error) {
	all, err := a.db.GetAllParticipants()
	if err != nil {
		return nil, fmt.Errorf("GetAllParticipants: %w", err)
	}
	var newcomer *Participant
	var others []*Participant
	for _, p := range all {
		switch {
		case p.ID == id:
			newcomer = p
		case inChain[p.ID]:
		case p.PipelineStep == "ready":
			others = append(others, p)
		}
	}
	if newcomer == nil {
		return nil, nil
	}
	a.db.LogActivity(fmt.Sprintf("🔮 Matching %s against existing pool...", newcomer.PersonaName))

	m := a.matcher.MatchNewcomer(newcomer, others)
	if m == nil {
		a.db.LogActivity(fmt.Sprintf("⏳ %s is ready but no match available yet", newcomer.PersonaName))
		return nil, nil
	}
	displaced, err := a.storeMatch(*m)
	if err != nil {
		return nil, err
	}
	inChain[m.A.ID], inChain[m.B.ID] = true, true
	if len(displaced) > 0 {
		a.db.LogActivity(fmt.Sprintf("🔄 %s took over %s's previous match", newcomer.PersonaName, m.B.PersonaName))
	}
	return displaced, nil
}

func extractJSON(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start == -1 || end == -1 || end <= start {
		return s
	}
	return s[start : end+1]
}
