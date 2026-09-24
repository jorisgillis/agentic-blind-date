package main

import (
	"testing"
)

func testDB(t *testing.T) *DB {
	t.Helper()
	db, err := NewDB(":memory:")
	if err != nil {
		t.Fatalf("NewDB: %v", err)
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

func TestNarrowWrites_EachChangesOnlyItsOwnFields(t *testing.T) {
	db := testDB(t)
	db.CreateParticipant("id-1", "octocat", "")

	db.SetProfile("id-1", &GitHubProfile{Login: "octocat"})
	db.SetQuestions("id-1", []Question{{ID: "q1", Text: "Question 1"}, {ID: "q2", Text: "Question 2"}})
	db.SetPersona("id-1", "The Octo", "Ships things")
	db.SetProfile("id-1", &GitHubProfile{Login: "octocat", Bio: "later"})

	p, _ := db.GetParticipant("id-1")
	if p.PersonaName != "The Octo" || p.PersonaTagline != "Ships things" {
		t.Errorf("persona: got %q / %q", p.PersonaName, p.PersonaTagline)
	}
	if p.Profile == nil || p.Profile.Bio != "later" {
		t.Errorf("profile: got %+v", p.Profile)
	}
	if len(p.Questions) != 2 || p.Questions[0].ID != "q1" {
		t.Errorf("questions should survive the later profile write: %+v", p.Questions)
	}
}

func TestUpdateAnswers(t *testing.T) {
	db := testDB(t)
	db.CreateParticipant("id-1", "octocat", "")

	answers := map[string]string{"0": "Tabs", "1": "Go"}
	if err := db.UpdateAnswers("id-1", answers); err != nil {
		t.Fatalf("UpdateAnswers: %v", err)
	}

	p, _ := db.GetParticipant("id-1")
	if p.Answers == nil {
		t.Fatal("Answers is nil")
	}
	if p.Answers["0"] != "Tabs" || p.Answers["1"] != "Go" {
		t.Errorf("Answers unexpected: %+v", p.Answers)
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

func TestLLMCache(t *testing.T) {
	db := testDB(t)

	if entry, exists := db.GetLLMCache("a:b"); exists || entry != nil {
		t.Error("expected cache miss for non-existent key")
	}

	db.SetLLMCache("a:b", &matchResult{Score: 85, Reason: "Great match!", RedFlags: []string{"tabs, obviously"}})
	entry, exists := db.GetLLMCache("a:b")
	if !exists {
		t.Fatal("expected cache hit for existing key")
	}
	if entry.Score != 85 || entry.Reason != "Great match!" || len(entry.RedFlags) != 1 || entry.RedFlags[0] != "tabs, obviously" {
		t.Errorf("round trip: got %+v", entry)
	}
	if entry.GreenFlags == nil || entry.Icebreakers == nil {
		t.Errorf("missing lists should come back empty, got %+v", entry)
	}

	db.ClearLLMCache()
	if _, exists := db.GetLLMCache("a:b"); exists {
		t.Error("expected cache miss after clear")
	}
}

func TestLLMCache_OldCommaJoinedRowsAreAMiss(t *testing.T) {
	db := testDB(t)
	db.db.Exec(`INSERT INTO llm_cache (pair_key, score, reason, red_flags, green_flags, icebreakers) VALUES ('a:b', 85, 'x', 'red1,red2', 'g', 'i')`)

	if _, exists := db.GetLLMCache("a:b"); exists {
		t.Error("an undecodable legacy row should be re-scored, not served")
	}
}

func TestUnmatchAll(t *testing.T) {
	db := testDB(t)

	// Create participants
	db.CreateParticipant("id-1", "user1", "User 1")
	db.CreateParticipant("id-2", "user2", "User 2")
	db.CreateParticipant("id-3", "user3", "User 3")

	// Match them
	db.SetMatched("id-1", "id-2", 0, "", "", "", "")
	db.SetMatched("id-3", "", 0, "", "", "", "")

	// Unmatch all
	db.UnmatchAll()

	// Verify all are unmatched
	p1, _ := db.GetParticipant("id-1")
	p2, _ := db.GetParticipant("id-2")
	p3, _ := db.GetParticipant("id-3")

	if p1.MatchedWith != "" {
		t.Errorf("expected p1 to be unmatched, got %s", p1.MatchedWith)
	}
	if p2.MatchedWith != "" {
		t.Errorf("expected p2 to be unmatched, got %s", p2.MatchedWith)
	}
	if p3.MatchedWith != "" {
		t.Errorf("expected p3 to be unmatched, got %s", p3.MatchedWith)
	}
}

func TestDeleteParticipant(t *testing.T) {
	db := testDB(t)

	// Create participant
	db.CreateParticipant("id-to-delete", "user", "User")

	// Verify it exists
	_, err := db.GetParticipant("id-to-delete")
	if err != nil {
		t.Fatalf("expected participant to exist: %v", err)
	}

	// Delete it
	db.DeleteParticipant("id-to-delete")

	// Verify it's gone
	_, err = db.GetParticipant("id-to-delete")
	if err == nil {
		t.Error("expected participant to be deleted")
	}
}

func TestUpdateInterests(t *testing.T) {
	db := testDB(t)
	db.CreateParticipant("id-1", "user1", "User 1")

	interests := map[string]interface{}{
		"languages": []string{"Go", "Python"},
		"tools":     []string{"Docker"},
	}

	db.UpdateInterests("id-1", interests)

	p, _ := db.GetParticipant("id-1")
	if p.Interests == nil {
		t.Fatal("expected Interests to be set")
	}
	if len(p.Interests) != 2 {
		t.Errorf("expected 2 interest categories, got %d", len(p.Interests))
	}
}

func TestUnmatchParticipant(t *testing.T) {
	db := testDB(t)

	db.CreateParticipant("id-1", "user1", "User 1")
	db.CreateParticipant("id-2", "user2", "User 2")
	db.SetMatched("id-1", "id-2", 0, "", "", "", "")

	// Verify they are matched
	p1, _ := db.GetParticipant("id-1")
	if p1.MatchedWith != "id-2" {
		t.Fatalf("expected id-1 to be matched with id-2, got %s", p1.MatchedWith)
	}

	// Unmatch id-1
	db.UnmatchParticipant("id-1")

	// Verify both are unmatched
	p1, _ = db.GetParticipant("id-1")
	p2, _ := db.GetParticipant("id-2")

	if p1.MatchedWith != "" {
		t.Errorf("expected id-1 to be unmatched, got %s", p1.MatchedWith)
	}
	if p2.MatchedWith != "" {
		t.Errorf("expected id-2 to be unmatched, got %s", p2.MatchedWith)
	}
}

func TestNewDB_OpensADatabaseWithTheOldExtraAnswersColumn(t *testing.T) {
	path := t.TempDir() + "/old.db"
	old, err := NewDB(path)
	if err != nil {
		t.Fatal(err)
	}
	old.db.Exec(`ALTER TABLE participants ADD COLUMN extra_answers TEXT NOT NULL DEFAULT '{}'`)
	old.Close()

	db, err := NewDB(path)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	defer db.Close()
	if err := db.CreateParticipant("id-1", "octocat", "Octo"); err != nil {
		t.Fatalf("CreateParticipant after migration: %v", err)
	}
	if _, err := db.GetParticipant("id-1"); err != nil {
		t.Fatalf("GetParticipant after migration: %v", err)
	}
	var n int
	db.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('participants') WHERE name = 'extra_answers'`).Scan(&n)
	if n != 0 {
		t.Error("the extra_answers column should be dropped")
	}
}
