package main

import (
	"errors"
	"testing"
)

func TestParticipantStore_GetReadsOneParticipant(t *testing.T) {
	db := newTestDB(t)
	store := NewParticipantStore(db)
	db.CreateParticipant("p", "octo", "Octo", true)

	got, err := store.Get("p")

	if err != nil || got.ID != "p" || got.GitHubHandle != "octo" {
		t.Fatalf("want Participant p, got %+v (err %v)", got, err)
	}
}

func TestParticipantStore_GetOfAnUnknownParticipantIsNotFound(t *testing.T) {
	store := NewParticipantStore(newTestDB(t))

	_, err := store.Get("nobody")

	if !errors.Is(err, ErrParticipantNotFound) {
		t.Errorf("want ErrParticipantNotFound, got %v", err)
	}
}

func TestParticipantStore_GetOverABrokenDatabaseIsAFailureNotNotFound(t *testing.T) {
	db := newTestDB(t)
	store := NewParticipantStore(db)
	db.CreateParticipant("p", "p", "P", true)
	breakDB(db)

	_, err := store.Get("p")

	if err == nil || errors.Is(err, ErrParticipantNotFound) {
		t.Errorf("a broken database is a failure, not not-found: got %v", err)
	}
}

func TestParticipantStore_AllReadsEveryParticipant(t *testing.T) {
	db := newTestDB(t)
	store := NewParticipantStore(db)
	db.CreateParticipant("a", "a", "A", true)
	db.CreateParticipant("b", "b", "B", true)

	got, err := store.All()

	if err != nil || len(got) != 2 {
		t.Fatalf("want 2 Participants, got %d (err %v)", len(got), err)
	}
}

func TestParticipantStore_AllOverABrokenDatabaseIsAFailure(t *testing.T) {
	db := newTestDB(t)
	store := NewParticipantStore(db)
	breakDB(db)

	if _, err := store.All(); err == nil {
		t.Error("want a failure")
	}
}

func TestParticipantStore_CreateRegistersAParticipant(t *testing.T) {
	db := newTestDB(t)
	store := NewParticipantStore(db)

	if err := store.Create("p", "octo", "Octo", true); err != nil {
		t.Fatal(err)
	}

	got, err := store.Get("p")
	if err != nil || got.GitHubHandle != "octo" || got.Name != "Octo" || !got.HasGitHub {
		t.Errorf("want the created Participant, got %+v (err %v)", got, err)
	}
}

func TestParticipantStore_CreateOverABrokenDatabaseIsAFailure(t *testing.T) {
	db := newTestDB(t)
	store := NewParticipantStore(db)
	breakDB(db)

	if err := store.Create("p", "p", "P", true); err == nil {
		t.Error("want a failure")
	}
}

func TestParticipantStore_ChangeSetsTypedInterestsAndMatch(t *testing.T) {
	db := newTestDB(t)
	store := NewParticipantStore(db)
	db.CreateParticipant("p", "p", "P", true)
	interests := &Interests{Languages: []string{"Go"}, Tools: []string{"cli"}, Domains: []string{"Backend"}}
	match := &matchResult{Score: 77, Reason: "Both love Go", RedFlags: []string{"tabs"}, GreenFlags: []string{"Go"}, Icebreakers: []string{"Why Go?"}}

	if err := store.Change(ParticipantChange{ID: "p", Interests: interests, Match: match}); err != nil {
		t.Fatal(err)
	}

	got := reload(t, db, "p")
	if len(got.Interests.Languages) != 1 || got.Interests.Languages[0] != "Go" {
		t.Errorf("interests round trip: got %+v", got.Interests)
	}
	if got.CompatScore != 77 || got.CompatReason != "Both love Go" {
		t.Errorf("match round trip: got score %d reason %q", got.CompatScore, got.CompatReason)
	}
	red, err := decodeMatchResult(got.CompatScore, got.CompatReason, got.RedFlags, got.GreenFlags, got.Icebreakers)
	if err != nil || len(red.RedFlags) != 1 || red.RedFlags[0] != "tabs" {
		t.Errorf("match flags round trip: got %+v (err %v)", red, err)
	}
}

func TestParticipantStore_ChangeAppliesSeveralParticipantsInOneTransaction(t *testing.T) {
	db := newTestDB(t)
	store := NewParticipantStore(db)
	db.CreateParticipant("a", "a", "A", true)
	db.CreateParticipant("b", "b", "B", true)

	err := store.Change(
		ParticipantChange{ID: "a", Interests: &Interests{Languages: []string{"Go"}}},
		ParticipantChange{ID: "b", Interests: &Interests{Languages: []string{"Rust"}}},
	)

	if err != nil {
		t.Fatal(err)
	}
	langsA := reload(t, db, "a").Interests.Languages
	langsB := reload(t, db, "b").Interests.Languages
	if len(langsA) != 1 || langsA[0] != "Go" || len(langsB) != 1 || langsB[0] != "Rust" {
		t.Errorf("both changes should apply: a=%v b=%v", langsA, langsB)
	}
}

func TestParticipantStore_ChangeOfAnUnknownParticipantIsNotFoundAndAppliesNothing(t *testing.T) {
	db := newTestDB(t)
	store := NewParticipantStore(db)
	db.CreateParticipant("a", "a", "A", true)

	err := store.Change(
		ParticipantChange{ID: "a", Interests: &Interests{Languages: []string{"Go"}}},
		ParticipantChange{ID: "nobody", Interests: &Interests{Languages: []string{"Rust"}}},
	)

	if !errors.Is(err, ErrParticipantNotFound) {
		t.Errorf("want ErrParticipantNotFound, got %v", err)
	}
	if got := reload(t, db, "a").Interests.Languages; len(got) != 0 {
		t.Errorf("the whole transaction should roll back, got %v", got)
	}
}

func TestParticipantStore_ChangeOfAnUnknownParticipantsMatchIsNotFound(t *testing.T) {
	store := NewParticipantStore(newTestDB(t))

	err := store.Change(ParticipantChange{ID: "nobody", Match: &matchResult{Score: 1}})

	if !errors.Is(err, ErrParticipantNotFound) {
		t.Errorf("want ErrParticipantNotFound, got %v", err)
	}
}

func TestParticipantStore_ChangeOverABrokenDatabaseIsAFailure(t *testing.T) {
	db := newTestDB(t)
	store := NewParticipantStore(db)
	db.CreateParticipant("a", "a", "A", true)
	breakDB(db)

	err := store.Change(ParticipantChange{ID: "a", Interests: &Interests{Languages: []string{"Go"}}})

	if err == nil || errors.Is(err, ErrParticipantNotFound) {
		t.Errorf("a broken database is a failure, not not-found: got %v", err)
	}
}

func TestParticipantStore_ChangeReportsAFailureDuringTheUpdateItself(t *testing.T) {
	db := newTestDB(t)
	store := NewParticipantStore(db)
	db.CreateParticipant("a", "a", "A", true)
	failWrites(t, db, "interests")

	err := store.Change(ParticipantChange{ID: "a", Interests: &Interests{Languages: []string{"Go"}}})

	if err == nil || errors.Is(err, ErrParticipantNotFound) {
		t.Errorf("a failing update is a failure, not not-found: got %v", err)
	}
}

func TestParticipantStore_ChangeAnnouncesOnTheChangeFeed(t *testing.T) {
	db := newTestDB(t)
	store := NewParticipantStore(db)
	db.CreateParticipant("a", "a", "A", true)
	ch, stop := db.Subscribe()
	defer stop()

	if err := store.Change(ParticipantChange{ID: "a", Interests: &Interests{Languages: []string{"Go"}}}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-ch:
	default:
		t.Error("want a change signal")
	}
}

func TestParticipantStore_ChangeOfNothingIsANoOpThatStillCommits(t *testing.T) {
	db := newTestDB(t)
	store := NewParticipantStore(db)

	if err := store.Change(); err != nil {
		t.Errorf("an empty change list should just succeed, got %v", err)
	}
}

func TestParticipantStore_ChangeAdvancesThePipelineStep(t *testing.T) {
	db := newTestDB(t)
	store := NewParticipantStore(db)
	db.CreateParticipant("p", "p", "P", true)

	for _, step := range []Step{StepInterviewing, StepCreatingPersona, StepReady} {
		if err := store.Change(ParticipantChange{ID: "p", PipelineStep: stepPtr(step)}); err != nil {
			t.Fatalf("advancing to %s: %v", step, err)
		}
		if got := reload(t, db, "p").PipelineStep; got != step {
			t.Fatalf("step: want %s, got %s", step, got)
		}
	}
}

func TestParticipantStore_ChangeRejectsIllegalPipelineTransitions(t *testing.T) {
	for name, tc := range map[string]struct {
		from, to Step
	}{
		"nothing leads back to the first step": {StepFetchingGitHub, StepFetchingGitHub},
		"skip the Interview":                   {StepFetchingGitHub, StepReady},
		"skip the Persona":                     {StepInterviewing, StepReady},
		"go back":                              {StepReady, StepInterviewing},
		"repeat":                               {StepCreatingPersona, StepCreatingPersona},
	} {
		t.Run(name, func(t *testing.T) {
			db := newTestDB(t)
			store := NewParticipantStore(db)
			db.CreateParticipant("p", "p", "P", true)
			forceStep(db, "p", tc.from)

			err := store.Change(ParticipantChange{ID: "p", PipelineStep: stepPtr(tc.to)})

			if !errors.Is(err, ErrIllegalTransition) {
				t.Errorf("%s → %s: want ErrIllegalTransition, got %v", tc.from, tc.to, err)
			}
			if got := reload(t, db, "p").PipelineStep; got != tc.from {
				t.Errorf("a rejected transition must not change the step: got %s", got)
			}
		})
	}
}

func TestParticipantStore_ChangeReportsAFailureAdvancingTheStep(t *testing.T) {
	db := newTestDB(t)
	store := NewParticipantStore(db)
	db.CreateParticipant("p", "p", "P", true)
	failWrites(t, db, "pipeline_step")

	err := store.Change(ParticipantChange{ID: "p", PipelineStep: stepPtr(StepInterviewing)})

	if err == nil || errors.Is(err, ErrIllegalTransition) {
		t.Errorf("a failing update is a failure, not an illegal transition: got %v", err)
	}
}

func TestParticipantStore_StartInterviewOpensTheInterview(t *testing.T) {
	db := newTestDB(t)
	store := NewParticipantStore(db)
	db.CreateParticipant("p", "p", "P", true)
	questions := []Question{{ID: "q1", Text: "Tabs or spaces?"}}

	if err := store.StartInterview("p", &GitHubProfile{Login: "p"}, questions); err != nil {
		t.Fatal(err)
	}

	got := reload(t, db, "p")
	if got.PipelineStep != StepInterviewing || len(got.Questions) != 1 || got.Questions[0].ID != "q1" || got.Profile.Login != "p" {
		t.Errorf("want the Interview open with its questions and profile, got %+v", got)
	}
}

func TestParticipantStore_StartInterviewOnlyWhileBeingPrepared(t *testing.T) {
	db := newTestDB(t)
	store := NewParticipantStore(db)
	db.CreateParticipant("p", "p", "P", true)
	if err := store.StartInterview("p", &GitHubProfile{}, []Question{{ID: "q1"}}); err != nil {
		t.Fatal(err)
	}

	err := store.StartInterview("p", &GitHubProfile{}, []Question{{ID: "other"}})

	if !errors.Is(err, ErrIllegalTransition) {
		t.Errorf("a running Interview cannot be started again: got %v", err)
	}
	if qs := reload(t, db, "p").Questions; len(qs) != 1 || qs[0].ID != "q1" {
		t.Errorf("the running Interview keeps its questions, got %+v", qs)
	}
}

func TestParticipantStore_StartInterviewReportsAFailure(t *testing.T) {
	db := newTestDB(t)
	store := NewParticipantStore(db)
	db.CreateParticipant("p", "p", "P", true)
	failWrites(t, db)

	err := store.StartInterview("p", &GitHubProfile{}, []Question{{ID: "q1"}})

	if err == nil || errors.Is(err, ErrIllegalTransition) {
		t.Errorf("a failing write is a failure, not an illegal transition: got %v", err)
	}
}

func TestParticipantStore_ChangeDeleteReportsAGenuineDatabaseFailure(t *testing.T) {
	db := newTestDB(t)
	store := NewParticipantStore(db)
	db.CreateParticipant("p", "p", "P", true)
	breakOnFault(t, db, "delete")

	if err := store.Change(ParticipantChange{ID: "p", Delete: true}); err == nil {
		t.Error("want a failure")
	}
}

func TestParticipantStore_ChangeMatchedWithReportsAGenuineDatabaseFailure(t *testing.T) {
	db := newTestDB(t)
	store := NewParticipantStore(db)
	db.CreateParticipant("p", "p", "P", true)
	breakOnFault(t, db, "pair")

	err := store.Change(ParticipantChange{ID: "p", MatchedWith: strPtr("q")})

	if err == nil {
		t.Error("want a failure")
	}
}

func TestParticipantStore_ChangeProfileReportsAGenuineDatabaseFailure(t *testing.T) {
	db := newTestDB(t)
	store := NewParticipantStore(db)
	db.CreateParticipant("p", "p", "P", true)
	breakOnFault(t, db, "profile")

	if err := store.Change(ParticipantChange{ID: "p", Profile: &GitHubProfile{}}); err == nil {
		t.Error("want a failure")
	}
}

func TestParticipantStore_ChangeAnswersReportsAGenuineDatabaseFailure(t *testing.T) {
	db := newTestDB(t)
	store := NewParticipantStore(db)
	db.CreateParticipant("p", "p", "P", true)
	breakOnFault(t, db, "answers")

	if err := store.Change(ParticipantChange{ID: "p", Answers: map[string]string{"a": "b"}}); err == nil {
		t.Error("want a failure")
	}
}

func TestParticipantStore_ChangeAdvanceStepReportsAGenuineDatabaseFailure(t *testing.T) {
	db := newTestDB(t)
	store := NewParticipantStore(db)
	db.CreateParticipant("p", "p", "P", true)
	breakOnFault(t, db, "pipeline_step")

	err := store.Change(ParticipantChange{ID: "p", PipelineStep: stepPtr(StepInterviewing)})

	if err == nil {
		t.Error("want a failure")
	}
}

func TestParticipantStore_StartInterviewReportsAGenuineDatabaseFailure(t *testing.T) {
	db := newTestDB(t)
	store := NewParticipantStore(db)
	db.CreateParticipant("p", "p", "P", true)
	breakOnFault(t, db, "start_interview")

	if err := store.StartInterview("p", &GitHubProfile{}, nil); err == nil {
		t.Error("want a failure")
	}
}

func TestDecodeMatchResult_AnyUndecodableListInvalidatesTheWholeAssessment(t *testing.T) {
	if _, err := decodeMatchResult(1, "x", "not json", "[]", "[]"); err == nil {
		t.Error("want an error when a list cannot be decoded")
	}
}

func TestEncodeMatchResult_NilListsEncodeAsEmptyArrays(t *testing.T) {
	red, green, ice := encodeMatchResult(&matchResult{Score: 1, Reason: "x"})

	if red != "[]" || green != "[]" || ice != "[]" {
		t.Errorf("want empty arrays, got %q %q %q", red, green, ice)
	}
}
