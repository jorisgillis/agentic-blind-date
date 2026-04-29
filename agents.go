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
func (a *AgentPipeline) RunMatching() error {
	a.db.SetPhase("matching")
	a.db.LogActivity("🔮 The matchmaker agents are at work...")

	participants, err := a.db.GetAllByStep("ready")
	if err != nil {
		return err
	}
	if len(participants) < 2 {
		return fmt.Errorf("need at least 2 ready participants, got %d", len(participants))
	}

	pairs := greedyMatch(participants)

	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)

	for _, pair := range pairs {
		wg.Add(1)
		go func(p1, p2 *Participant) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			result, err := a.generateMatch(p1, p2)
			if err != nil {
				log.Printf("Match generation error for %s/%s: %v", p1.GitHubHandle, p2.GitHubHandle, err)
				result = &matchResult{
					Score:       42,
					Reason:      "The algorithm has spoken. We cannot explain.",
					RedFlags:    []string{},
					GreenFlags:  []string{"You're both here tonight"},
					Icebreakers: []string{"What brings you to this meetup?", "What are you currently building?", "Best tech talk you've seen recently?"},
				}
			}

			redJSON, _ := json.Marshal(result.RedFlags)
			greenJSON, _ := json.Marshal(result.GreenFlags)
			iceJSON, _ := json.Marshal(result.Icebreakers)

			a.db.SetMatched(p1.ID, p2.ID, result.Score, result.Reason, string(redJSON), string(greenJSON), string(iceJSON))
			a.db.SetMatched(p2.ID, p1.ID, result.Score, result.Reason, string(redJSON), string(greenJSON), string(iceJSON))
			a.db.LogActivity(fmt.Sprintf("💘 %s ↔ %s (%d%%)", p1.PersonaName, p2.PersonaName, result.Score))
		}(pair[0], pair[1])
	}

	wg.Wait()
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

func extractJSON(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start == -1 || end == -1 || end <= start {
		return s
	}
	return s[start : end+1]
}
