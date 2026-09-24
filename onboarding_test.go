package main

import (
	"sync"
	"testing"
)

func onboardingFor(t *testing.T, llm *fakeLLM, gh *fakeGitHub) (*Onboarding, *DB) {
	t.Helper()
	db := newTestDB(t)
	interview := NewInterview(db, llm)
	matcher := NewMatcher(db, gh, llm)
	matchmaking := NewMatchmaking(db, matcher, NewRelationships(db))
	return NewOnboarding(db, gh, interview, NewPersonas(llm), matchmaking), db
}

func answerEverything(t *testing.T, o *Onboarding, db *DB, id string) {
	t.Helper()
	eventually(t, "interview started", func() bool { return reload(t, db, id).PipelineStep == StepInterviewing })
	for {
		p := reload(t, db, id)
		q := NewInterview(db, nil).Next(p)
		if q == nil {
			return
		}
		answer := "whatever"
		if q.Question.MaxSelections > 1 {
			answer = `["` + q.Question.Options[0] + `"]`
		}
		if _, err := o.Answer(p, answer); err != nil {
			t.Fatalf("answering %s: %v", q.Question.ID, err)
		}
	}
}

func TestOnboarding_FromRegistrationToReady(t *testing.T) {
	gh := newFakeGitHub().withProfile(&GitHubProfile{Login: "octo", Languages: []string{"Go"}, TopTopics: []string{"cli"}})
	llm := newFakeLLM().
		on("interviewer", `{"questions": ["Why?", "How?", "When?"]}`).
		on("personality generator", `{"name": "The Gopher", "tagline": "Ships"}`)
	o, db := onboardingFor(t, llm, gh)

	id, err := o.Register("Octo", "octo", true)
	if err != nil {
		t.Fatal(err)
	}
	answerEverything(t, o, db, id)

	var p *Participant
	eventually(t, "ready", func() bool {
		p = reload(t, db, id)
		return p.PipelineStep == StepReady
	})
	if p.PersonaName != "The Gopher" {
		t.Errorf("persona: got %q", p.PersonaName)
	}
	langs, _ := p.Interests["languages"].([]any)
	tools, _ := p.Interests["tools"].([]any)
	if len(langs) != 1 || langs[0] != "Go" || len(tools) != 1 || tools[0] != "cli" {
		t.Errorf("interests from the GitHub profile: got %v", p.Interests)
	}
}

func TestOnboarding_RegisteringAKnownHandleResumesTheSameParticipant(t *testing.T) {
	o, _ := onboardingFor(t, newFakeLLM(), newFakeGitHub())

	first, err := o.Register("Octo", "octo", true)
	if err != nil {
		t.Fatal(err)
	}
	again, err := o.Register("Octo", "octo", true)

	if err != nil || again != first {
		t.Errorf("want the same Participant %s, got %s (err %v)", first, again, err)
	}
}

func TestOnboarding_RegisteringWithoutGitHubNeedsNoHandle(t *testing.T) {
	o, db := onboardingFor(t, newFakeLLM(), newFakeGitHub())

	a, errA := o.Register("Ada", "", false)
	b, errB := o.Register("Bob", "", false)

	if errA != nil || errB != nil || a == b {
		t.Fatalf("two Non-GitHub Users: %s %s (%v %v)", a, b, errA, errB)
	}
	if p := reload(t, db, a); p.HasGitHub || p.Name != "Ada" {
		t.Errorf("got %+v", p)
	}
}

func TestOnboarding_SubmittingTheLastAnswerTwiceCreatesOnePersona(t *testing.T) {
	llm := newFakeLLM().on("personality generator", `{"name": "The Gopher", "tagline": "Ships"}`)
	o, db := onboardingFor(t, llm, newFakeGitHub())
	db.CreateParticipant("p", "p", "P", true)
	db.SetProfile("p", &GitHubProfile{})
	db.SetQuestions("p", []Question{{ID: "q1", Text: "Last one?"}})
	forceStep(db, "p", StepInterviewing)

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o.Answer(reload(t, db, "p"), "yes")
		}()
	}
	wg.Wait()

	eventually(t, "ready", func() bool { return reload(t, db, "p").PipelineStep == StepReady })
	if n := llm.callsMatching("personality generator"); n != 1 {
		t.Errorf("personas created: want 1, got %d", n)
	}
}
