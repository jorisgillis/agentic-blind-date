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

func TestSteps_FailedWritesAndUnreachableSteps(t *testing.T) {
	db := newTestDB(t)
	db.CreateParticipant("p", "p", "P", true)

	if err := db.AdvanceStep("p", StepFetchingGitHub); !errors.Is(err, ErrIllegalTransition) {
		t.Errorf("nothing leads back to the first step: got %v", err)
	}
	restore := failWrites(t, db)
	if err := db.AdvanceStep("p", StepInterviewing); err == nil || errors.Is(err, ErrIllegalTransition) {
		t.Errorf("a failed write is reported as such: got %v", err)
	}
	if err := db.StartInterview("p", &GitHubProfile{}, nil); err == nil || errors.Is(err, ErrIllegalTransition) {
		t.Errorf("a failed write is reported as such: got %v", err)
	}
	restore()
	if got := reload(t, db, "p").PipelineStep; got != StepFetchingGitHub {
		t.Errorf("failed writes change nothing: got %s", got)
	}
}

func TestAdvanceStep_ReportsAGenuineDatabaseFailure(t *testing.T) {
	db := newTestDB(t)
	db.CreateParticipant("p", "p", "P", true)
	breakOnFault(t, db, "pipeline_step")

	if err := db.AdvanceStep("p", StepInterviewing); err == nil {
		t.Error("want a failure")
	}
}

func TestStartInterview_ReportsAGenuineDatabaseFailure(t *testing.T) {
	db := newTestDB(t)
	db.CreateParticipant("p", "p", "P", true)
	breakOnFault(t, db, "start_interview")

	if err := db.StartInterview("p", &GitHubProfile{}, nil); err == nil {
		t.Error("want a failure")
	}
}

func TestStartInterview_OnlyWhileBeingPrepared(t *testing.T) {
	db := newTestDB(t)
	db.CreateParticipant("p", "p", "P", true)
	db.StartInterview("p", &GitHubProfile{}, []Question{{ID: "q1"}})

	err := db.StartInterview("p", &GitHubProfile{}, []Question{{ID: "other"}})

	if !errors.Is(err, ErrIllegalTransition) {
		t.Errorf("a running Interview cannot be started again: got %v", err)
	}
	if qs := reload(t, db, "p").Questions; len(qs) != 1 || qs[0].ID != "q1" {
		t.Errorf("the running Interview keeps its questions, got %+v", qs)
	}
}
