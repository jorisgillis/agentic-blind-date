package main

import (
	"fmt"
	"log"

	"github.com/google/uuid"
)

// Onboarding takes a Participant from registration to ready: it fetches the
// GitHub profile, starts the Interview, and once every question is answered
// creates the Persona, computes Interests, makes them ready and hands them to
// Continuous Matching. Preparation and finishing run in the background.
type Onboarding struct {
	db          *DB
	github      GitHubAPI
	interview   *Interview
	personas    *Personas
	matchmaking *Matchmaking
}

// NewOnboarding creates the Onboarding module.
func NewOnboarding(db *DB, github GitHubAPI, interview *Interview, personas *Personas, matchmaking *Matchmaking) *Onboarding {
	return &Onboarding{db: db, github: github, interview: interview, personas: personas, matchmaking: matchmaking}
}

// Register signs a Participant up and starts preparing their Interview in the
// background. Registering a GitHub handle that is already known resumes that
// Participant. Non-GitHub Users get a generated handle, which is only a key.
func (o *Onboarding) Register(name, handle string, hasGitHub bool) (string, error) {
	if hasGitHub {
		if existing, err := o.db.GetParticipantByHandle(handle); err == nil {
			o.resume(existing)
			return existing.ID, nil
		}
	}
	id := uuid.New().String()
	if !hasGitHub {
		handle = "no-github-" + id[:8]
	}
	if err := o.db.CreateParticipant(id, handle, name, hasGitHub); err != nil {
		return "", err
	}
	go o.prepare(id)
	return id, nil
}

// Answer records an answer to the Participant's next question. The answer that
// completes the Interview finishes onboarding in the background.
func (o *Onboarding) Answer(p *Participant, raw string) (done bool, err error) {
	done, err = o.interview.Submit(p, raw)
	if done {
		completed := *p
		go o.finish(&completed)
	}
	return done, err
}

// prepare fetches the GitHub profile (GitHub users only) and starts the Interview.
func (o *Onboarding) prepare(participantID string) {
	p, err := o.db.GetParticipant(participantID)
	if err != nil {
		log.Printf("Onboarding: participant %s not found: %v", participantID, err)
		return
	}

	profile := &GitHubProfile{}
	if p.HasGitHub {
		handle := p.GitHubHandle
		profile = &GitHubProfile{Login: handle, Name: handle}
		o.db.LogActivity("🔍 Fetching a GitHub profile...")
		fetched, err := o.github.FetchProfile(handle)
		if err != nil {
			log.Printf("Failed to fetch GitHub profile for @%s: %v", handle, err)
		} else if fetched != nil {
			profile = fetched
		}
	} else {
		o.db.LogActivity("📝 Processing non-GitHub user...")
	}
	o.db.LogActivity("📝 Preparing interview questions...")

	if err := o.interview.Start(p, profile); err != nil {
		log.Printf("Starting interview for %s failed: %v", participantID, err)
		return
	}
	o.db.LogActivity("✅ Ready for the interview!")
}

// Resume restarts onboarding work that was interrupted, for example by a
// restart: Participants still being prepared, or whose Persona was being created.
func (o *Onboarding) Resume() {
	all, err := o.db.GetAllParticipants()
	if err != nil {
		log.Printf("Onboarding: resume: %v", err)
		return
	}
	for _, p := range all {
		o.resume(p)
	}
}

func (o *Onboarding) resume(p *Participant) {
	switch p.PipelineStep {
	case StepFetchingGitHub:
		go o.prepare(p.ID)
	case StepCreatingPersona:
		go o.becomeReady(p)
	}
}

// finish moves the Participant on from a completed Interview.
func (o *Onboarding) finish(p *Participant) {
	// Only one finish per Participant: a second submit of the last answer stops here.
	if err := o.db.AdvanceStep(p.ID, StepCreatingPersona); err != nil {
		log.Printf("Onboarding: %v", err)
		return
	}
	o.becomeReady(p)
}

// becomeReady creates the Persona and Interests, makes the Participant ready,
// and hands them to Continuous Matching. Failures to save the Persona or
// Interests are logged rather than leaving the Participant stuck.
func (o *Onboarding) becomeReady(p *Participant) {
	o.db.LogActivity("🎭 Crafting a new persona...")
	if p.Profile == nil {
		p.Profile = &GitHubProfile{}
	}
	persona := o.personas.Create(p)
	if err := o.db.SetPersona(p.ID, persona.Name, persona.Tagline); err != nil {
		log.Printf("Onboarding: saving persona of %s: %v", p.ID, err)
	}
	if err := o.db.UpdateInterests(p.ID, interestsOf(p.Profile)); err != nil {
		log.Printf("Onboarding: saving interests of %s: %v", p.ID, err)
	}
	if err := o.db.AdvanceStep(p.ID, StepReady); err != nil {
		log.Printf("Onboarding: %v", err)
		return
	}
	o.db.LogActivity(fmt.Sprintf("✅ %s is ready for matching!", persona.Name))

	if err := o.matchmaking.MatchNewcomer(p); err != nil {
		log.Printf("Continuous matching for %s failed: %v", p.ID, err)
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
