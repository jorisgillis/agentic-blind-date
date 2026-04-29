package main

import (
	"testing"
)

func testDB(t *testing.T) *DB {
	t.Helper()
	db, err := initDB(":memory:")
	if err != nil {
		t.Fatalf("initDB: %v", err)
	}
	// In-memory SQLite creates a new database per connection; pin to one connection.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestCreateAndGetParticipant(t *testing.T) {
	db := testDB(t)

	if err := db.CreateParticipant("id-1", "octocat", "Octo Cat"); err != nil {
		t.Fatalf("CreateParticipant: %v", err)
	}

	p, err := db.GetParticipant("id-1")
	if err != nil {
		t.Fatalf("GetParticipant: %v", err)
	}

	if p.ID != "id-1" {
		t.Errorf("ID: want id-1, got %s", p.ID)
	}
	if p.GitHubHandle != "octocat" {
		t.Errorf("GitHubHandle: want octocat, got %s", p.GitHubHandle)
	}
	if p.Name != "Octo Cat" {
		t.Errorf("Name: want 'Octo Cat', got %s", p.Name)
	}
	if p.PipelineStep != "fetching_github" {
		t.Errorf("PipelineStep: want fetching_github, got %s", p.PipelineStep)
	}
}

func TestGetParticipantByHandle(t *testing.T) {
	db := testDB(t)
	db.CreateParticipant("id-2", "torvalds", "Linus")

	p, err := db.GetParticipantByHandle("torvalds")
	if err != nil {
		t.Fatalf("GetParticipantByHandle: %v", err)
	}
	if p.ID != "id-2" {
		t.Errorf("want id-2, got %s", p.ID)
	}
}

func TestCreateParticipant_duplicate(t *testing.T) {
	db := testDB(t)
	db.CreateParticipant("id-1", "octocat", "")
	err := db.CreateParticipant("id-2", "octocat", "")
	if err == nil {
		t.Error("expected error for duplicate github_handle, got nil")
	}
}

func TestUpdatePipelineStep(t *testing.T) {
	db := testDB(t)
	db.CreateParticipant("id-1", "octocat", "")

	if err := db.UpdatePipelineStep("id-1", "interviewing"); err != nil {
		t.Fatalf("UpdatePipelineStep: %v", err)
	}

	p, _ := db.GetParticipant("id-1")
	if p.PipelineStep != "interviewing" {
		t.Errorf("want interviewing, got %s", p.PipelineStep)
	}
}

func TestUpdateProfile(t *testing.T) {
	db := testDB(t)
	db.CreateParticipant("id-1", "octocat", "")

	err := db.UpdateProfile("id-1", `{"login":"octocat"}`, "The Octo", "Ships things", `["q1","q2"]`)
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}

	p, _ := db.GetParticipant("id-1")
	if p.PersonaName != "The Octo" {
		t.Errorf("PersonaName: want 'The Octo', got %s", p.PersonaName)
	}
	if p.PersonaTagline != "Ships things" {
		t.Errorf("PersonaTagline: want 'Ships things', got %s", p.PersonaTagline)
	}
	if p.ProfileJSON != `{"login":"octocat"}` {
		t.Errorf("ProfileJSON unexpected: %s", p.ProfileJSON)
	}
	if p.CustomQuestions != `["q1","q2"]` {
		t.Errorf("CustomQuestions unexpected: %s", p.CustomQuestions)
	}
}

func TestUpdateAnswers(t *testing.T) {
	db := testDB(t)
	db.CreateParticipant("id-1", "octocat", "")

	answers := `{"0":"Tabs","1":"Go"}`
	if err := db.UpdateAnswers("id-1", answers); err != nil {
		t.Fatalf("UpdateAnswers: %v", err)
	}

	p, _ := db.GetParticipant("id-1")
	if p.AnswersJSON != answers {
		t.Errorf("AnswersJSON: want %s, got %s", answers, p.AnswersJSON)
	}
}

func TestSetMatched(t *testing.T) {
	db := testDB(t)
	db.CreateParticipant("id-1", "alice", "")
	db.CreateParticipant("id-2", "bob", "")

	err := db.SetMatched("id-1", "id-2", 87, "Great combo!", `["flag1"]`, `["flag2"]`, `["starter"]`)
	if err != nil {
		t.Fatalf("SetMatched: %v", err)
	}

	p, _ := db.GetParticipant("id-1")
	if p.MatchedWith != "id-2" {
		t.Errorf("MatchedWith: want id-2, got %s", p.MatchedWith)
	}
	if p.CompatScore != 87 {
		t.Errorf("CompatScore: want 87, got %d", p.CompatScore)
	}
	if p.CompatReason != "Great combo!" {
		t.Errorf("CompatReason: want 'Great combo!', got %s", p.CompatReason)
	}
	if p.PipelineStep != "matched" {
		t.Errorf("PipelineStep: want matched, got %s", p.PipelineStep)
	}
}

func TestGetAllByStep(t *testing.T) {
	db := testDB(t)
	db.CreateParticipant("id-1", "alice", "")
	db.CreateParticipant("id-2", "bob", "")
	db.UpdatePipelineStep("id-1", "ready")

	ready, err := db.GetAllByStep("ready")
	if err != nil {
		t.Fatalf("GetAllByStep: %v", err)
	}
	if len(ready) != 1 || ready[0].ID != "id-1" {
		t.Errorf("expected [id-1], got %v", ready)
	}

	fetching, _ := db.GetAllByStep("fetching_github")
	if len(fetching) != 1 || fetching[0].ID != "id-2" {
		t.Errorf("expected [id-2], got %v", fetching)
	}
}

func TestPhase(t *testing.T) {
	db := testDB(t)

	phase, err := db.GetPhase()
	if err != nil {
		t.Fatalf("GetPhase: %v", err)
	}
	if phase != "onboarding" {
		t.Errorf("initial phase: want onboarding, got %s", phase)
	}

	db.SetPhase("matching")
	phase, _ = db.GetPhase()
	if phase != "matching" {
		t.Errorf("after SetPhase: want matching, got %s", phase)
	}
}

func TestActivityLog(t *testing.T) {
	db := testDB(t)

	db.LogActivity("event one")
	db.LogActivity("event two")
	db.LogActivity("event three")

	// Limit=2: two of the three messages returned
	msgs, err := db.GetRecentActivity(2)
	if err != nil {
		t.Fatalf("GetRecentActivity: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages (limit), got %d", len(msgs))
	}

	// All three accessible with limit=10
	all, _ := db.GetRecentActivity(10)
	if len(all) != 3 {
		t.Errorf("expected 3 total messages, got %d", len(all))
	}
	want := map[string]bool{"event one": true, "event two": true, "event three": true}
	for _, m := range all {
		if !want[m] {
			t.Errorf("unexpected message: %s", m)
		}
	}
}

func TestCounts(t *testing.T) {
	db := testDB(t)

	if n := db.ParticipantCount(); n != 0 {
		t.Errorf("initial count: want 0, got %d", n)
	}

	db.CreateParticipant("id-1", "alice", "")
	db.CreateParticipant("id-2", "bob", "")
	db.UpdatePipelineStep("id-1", "ready")
	db.UpdatePipelineStep("id-2", "matched")

	if n := db.ParticipantCount(); n != 2 {
		t.Errorf("ParticipantCount: want 2, got %d", n)
	}
	if n := db.ReadyCount(); n != 2 {
		t.Errorf("ReadyCount: want 2 (ready+matched), got %d", n)
	}
}
