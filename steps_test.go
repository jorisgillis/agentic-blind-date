package main

import (
	"errors"
	"testing"
)

func TestAdvanceStep_FollowsThePipelineInOrder(t *testing.T) {
	db := newTestDB(t)
	db.CreateParticipant("p", "p", "P", true)

	for _, step := range []Step{StepInterviewing, StepCreatingPersona, StepReady} {
		if err := db.AdvanceStep("p", step); err != nil {
			t.Fatalf("advancing to %s: %v", step, err)
		}
		if got := reload(t, db, "p").PipelineStep; got != step {
			t.Fatalf("step: want %s, got %s", step, got)
		}
	}
}

func TestAdvanceStep_RejectsIllegalTransitions(t *testing.T) {
	for name, tc := range map[string]struct {
		from, to Step
	}{
		"skip the Interview": {StepFetchingGitHub, StepReady},
		"skip the Persona":   {StepInterviewing, StepReady},
		"go back":            {StepReady, StepInterviewing},
		"repeat":             {StepCreatingPersona, StepCreatingPersona},
	} {
		t.Run(name, func(t *testing.T) {
			db := newTestDB(t)
			db.CreateParticipant("p", "p", "P", true)
			forceStep(db, "p", tc.from)

			err := db.AdvanceStep("p", tc.to)

			if !errors.Is(err, ErrIllegalTransition) {
				t.Errorf("%s → %s: want ErrIllegalTransition, got %v", tc.from, tc.to, err)
			}
			if got := reload(t, db, "p").PipelineStep; got != tc.from {
				t.Errorf("a rejected transition must not change the step: got %s", got)
			}
		})
	}
}

func TestFinalSetup_RunsOnceEvenIfTheLastAnswerIsSubmittedTwice(t *testing.T) {
	llm := newFakeLLM().on("personality generator", `{"name": "The Gopher", "tagline": "Ships"}`)
	_, deps := newTestServer(t, llm, nil)
	seed(t, deps.db, "p", "", "interviewing")

	deps.agents.RunFinalSetup("p")
	deps.agents.RunFinalSetup("p")

	if n := llm.callsMatching("personality generator"); n != 1 {
		t.Errorf("persona created %d times, want once", n)
	}
}
