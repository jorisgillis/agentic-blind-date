package main

import (
	"strings"
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
	if len(p.Interests.Languages) != 1 || p.Interests.Languages[0] != "Go" || len(p.Interests.Tools) != 1 || p.Interests.Tools[0] != "cli" {
		t.Errorf("interests from the GitHub profile: got %+v", p.Interests)
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

func TestOnboarding_RegisteringAgainResumesAPreparationThatNeverFinished(t *testing.T) {
	gh := newFakeGitHub().withProfile(&GitHubProfile{Login: "octo"})
	llm := newFakeLLM().on("interviewer", `{"questions": ["Why?", "How?", "When?"]}`)
	o, db := onboardingFor(t, llm, gh)
	// A previous preparation died (crash, restart) and left the Participant waiting.
	db.CreateParticipant("stuck", "octo", "Octo", true)

	id, err := o.Register("Octo", "octo", true)

	if err != nil || id != "stuck" {
		t.Fatalf("want the existing Participant, got %q (err %v)", id, err)
	}
	eventually(t, "interview started", func() bool { return reload(t, db, id).PipelineStep == StepInterviewing })
}

func TestOnboarding_ResumeFinishesAPersonaThatWasInterrupted(t *testing.T) {
	llm := newFakeLLM().on("personality generator", `{"name": "The Gopher", "tagline": "Ships"}`)
	o, db := onboardingFor(t, llm, newFakeGitHub())
	db.CreateParticipant("p", "p", "P", true)
	forceStep(db, "p", StepCreatingPersona)

	o.Resume()

	eventually(t, "ready", func() bool { return reload(t, db, "p").PipelineStep == StepReady })
	if got := reload(t, db, "p").PersonaName; got != "The Gopher" {
		t.Errorf("persona: got %q", got)
	}
}

// awaitingPersona puts a Participant at the persona step, as if their Interview just completed.
func awaitingPersona(t *testing.T, db *DB, id string) {
	t.Helper()
	db.CreateParticipant(id, id, id, true)
	db.SetProfile(id, &GitHubProfile{Login: id, Languages: []string{"Go"}})
	forceStep(db, id, StepCreatingPersona)
}

func TestOnboarding_RegistrationThatCannotBeSavedIsAnError(t *testing.T) {
	o, db := onboardingFor(t, newFakeLLM(), newFakeGitHub())
	failWrites(t, db)

	if _, err := o.Register("Ada", "", false); err == nil {
		t.Error("want an error when the Participant cannot be saved")
	}
}

func TestOnboarding_AFailedInterviewStartWritesNothing(t *testing.T) {
	gh := newFakeGitHub().withProfile(&GitHubProfile{Login: "octo", Languages: []string{"Go"}})
	llm := newFakeLLM().on("interviewer", `{"questions": ["Why?", "How?", "When?"]}`)
	o, db := onboardingFor(t, llm, gh)
	db.CreateParticipant("p", "octo", "Octo", true)
	restore := failWrites(t, db, "questions")

	o.Resume()
	eventually(t, "the interview start to be attempted", func() bool { return llm.callsMatching("interviewer") == 1 })

	p := reload(t, db, "p")
	if p.PipelineStep != StepFetchingGitHub || len(p.Questions) != 0 || (p.Profile != nil && len(p.Profile.Languages) != 0) {
		t.Errorf("a failed start must leave the Participant untouched, got step %s, %d questions, profile %+v", p.PipelineStep, len(p.Questions), p.Profile)
	}

	restore()
	o.Resume()
	eventually(t, "the resumed interview to start", func() bool { return reload(t, db, "p").PipelineStep == StepInterviewing })
}

func TestOnboarding_AParticipantBecomesReadyEvenIfPersonaOrInterestsCannotBeSaved(t *testing.T) {
	for _, column := range []string{"persona_name", "interests"} {
		t.Run(column, func(t *testing.T) {
			o, db := onboardingFor(t, newFakeLLM(), newFakeGitHub())
			awaitingPersona(t, db, "p")
			failWrites(t, db, column)

			o.Resume()

			eventually(t, "ready", func() bool { return reload(t, db, "p").PipelineStep == StepReady })
		})
	}
}

func TestOnboarding_AParticipantWithoutAProfileStillGetsAPersona(t *testing.T) {
	o, db := onboardingFor(t, newFakeLLM(), newFakeGitHub())
	awaitingPersona(t, db, "p")
	db.db.Exec(`UPDATE participants SET profile_json = 'null' WHERE id = 'p'`)

	o.Resume()

	eventually(t, "ready", func() bool { return reload(t, db, "p").PipelineStep == StepReady })
	if got := reload(t, db, "p").PersonaName; got != "The Mysterious Coder" {
		t.Errorf("persona: got %q", got)
	}
}

// TestOnboarding_ActivityNeverNamesAnyoneBeforeTheReveal covers #45: the Big
// Screen shows the activity ticker to the whole room, so no line written
// while onboarding a Participant may contain their handle, name or ID.
func TestOnboarding_ActivityNeverNamesAnyoneBeforeTheReveal(t *testing.T) {
	gh := newFakeGitHub().withProfile(&GitHubProfile{Login: "octohandle", Languages: []string{"Go"}})
	llm := newFakeLLM().
		on("interviewer", `{"questions": ["Why?", "How?", "When?"]}`).
		on("personality generator", `{"name": "The Gopher", "tagline": "Ships"}`)
	o, db := onboardingFor(t, llm, gh)

	id, err := o.Register("Real Name", "octohandle", true)
	if err != nil {
		t.Fatal(err)
	}
	answerEverything(t, o, db, id)
	eventually(t, "ready", func() bool { return reload(t, db, id).PipelineStep == StepReady })

	lines, err := db.GetRecentActivity(50)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) == 0 {
		t.Fatal("expected activity lines to have been logged")
	}
	for _, line := range lines {
		if strings.Contains(line, "octohandle") || strings.Contains(line, "Real Name") || strings.Contains(line, id) {
			t.Errorf("activity line names the Participant before the Reveal: %q", line)
		}
	}
}

func TestOnboarding_AParticipantRemovedWhileTheirPersonaIsCreatedIsNotMatched(t *testing.T) {
	var db *DB
	llm := newFakeLLM().
		onFunc("personality generator", func(string) (string, error) {
			NewRelationships(db).Remove("p")
			return `{"name": "The Ghost", "tagline": "Gone"}`, nil
		})
	o, testDB := onboardingFor(t, llm, newFakeGitHub())
	db = testDB
	awaitingPersona(t, db, "p")
	seed(t, db, "q", "Q", "ready")

	o.Resume()

	eventually(t, "persona attempted", func() bool { return llm.callsMatching("personality generator") == 1 })
	if llm.callsMatching("matchmaker") != 0 || reload(t, db, "q").IsMatched() {
		t.Error("a removed Participant must not be matched")
	}
}

func TestOnboarding_AFailedMatchLeavesTheParticipantReadyAndUnmatched(t *testing.T) {
	llm := newFakeLLM().on("matchmaker", `{"score": 80}`)
	o, db := onboardingFor(t, llm, newFakeGitHub())
	awaitingPersona(t, db, "p")
	seed(t, db, "q", "Q", "ready")
	failWrites(t, db, "matched_with")

	o.Resume()

	eventually(t, "matching attempted", func() bool { return llm.callsMatching("matchmaker") == 1 })
	eventually(t, "ready", func() bool { return reload(t, db, "p").PipelineStep == StepReady })
	if reload(t, db, "p").IsMatched() || reload(t, db, "q").IsMatched() {
		t.Error("a failed match must leave both unmatched")
	}
}

func TestOnboarding_ResumeWithABrokenDatabaseDoesNothing(t *testing.T) {
	o, db := onboardingFor(t, newFakeLLM(), newFakeGitHub())
	breakDB(db)

	o.Resume() // must not panic
}
