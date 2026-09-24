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
	langs, _ := got.Interests["languages"].([]any)
	if len(langs) != 1 || langs[0] != "Go" {
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
	langsA, _ := reload(t, db, "a").Interests["languages"].([]any)
	langsB, _ := reload(t, db, "b").Interests["languages"].([]any)
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
	if got, _ := reload(t, db, "a").Interests["languages"].([]any); len(got) != 0 {
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
