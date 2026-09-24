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
		go o.finish(p.ID)
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
		o.db.LogActivity(fmt.Sprintf("🔍 Fetching @%s's GitHub profile...", handle))
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

// finish creates the Persona and Interests, makes the Participant ready, and
// hands them to Continuous Matching.
func (o *Onboarding) finish(participantID string) {
	// Only one finish per Participant: a second submit of the last answer stops here.
	if err := o.db.AdvanceStep(participantID, StepCreatingPersona); err != nil {
		log.Printf("Onboarding: %v", err)
		return
	}
	o.db.LogActivity(fmt.Sprintf("🎭 Crafting persona for participant %s...", participantID))

	p, err := o.db.GetParticipant(participantID)
	if err != nil || p.Profile == nil {
		log.Printf("Onboarding: cannot finish %s: %v", participantID, err)
		return
	}
	persona := o.personas.Create(p)
	o.db.SetPersona(participantID, persona.Name, persona.Tagline)
	o.db.UpdateInterests(participantID, interestsOf(p.Profile))
	if err := o.db.AdvanceStep(participantID, StepReady); err != nil {
		log.Printf("Onboarding: %v", err)
		return
	}
	o.db.LogActivity(fmt.Sprintf("✅ %s is ready for matching!", persona.Name))

	ready, err := o.db.GetParticipant(participantID)
	if err != nil {
		log.Printf("Onboarding: reloading %s for matching: %v", participantID, err)
		return
	}
	if err := o.matchmaking.MatchNewcomer(ready); err != nil {
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
