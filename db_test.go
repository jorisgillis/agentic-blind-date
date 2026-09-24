package main

import (
	"database/sql"
	"os"
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

	if err := db.CreateParticipant("id-1", "octocat", "Octo Cat", true); err != nil {
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
	db.CreateParticipant("id-2", "torvalds", "Linus", true)

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
	db.CreateParticipant("id-1", "octocat", "", true)
	err := db.CreateParticipant("id-2", "octocat", "", true)
	if err == nil {
		t.Error("expected error for duplicate github_handle, got nil")
	}
}

func TestNarrowWrites_EachChangesOnlyItsOwnFields(t *testing.T) {
	db := testDB(t)
	db.CreateParticipant("id-1", "octocat", "", true)

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
	db.CreateParticipant("id-1", "octocat", "", true)

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

func TestGetAllByStep(t *testing.T) {
	db := testDB(t)
	db.CreateParticipant("id-1", "alice", "", true)
	db.CreateParticipant("id-2", "bob", "", true)
	forceStep(db, "id-1", "ready")

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

	db.CreateParticipant("id-1", "alice", "", true)
	db.CreateParticipant("id-2", "bob", "", true)
	db.CreateParticipant("id-3", "carol", "", true)
	forceStep(db, "id-1", "ready")
	forceStep(db, "id-2", "ready")
	NewRelationships(db).Pair(Match{A: &Participant{ID: "id-1"}, B: &Participant{ID: "id-2"}, Result: &matchResult{}})

	if n := db.ParticipantCount(); n != 3 {
		t.Errorf("ParticipantCount: want 3, got %d", n)
	}
	if n := db.ReadyCount(); n != 2 {
		t.Errorf("ReadyCount: want 2 (matched Participants are ready too), got %d", n)
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

func TestUpdateInterests(t *testing.T) {
	db := testDB(t)
	db.CreateParticipant("id-1", "user1", "User 1", true)

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
	if err := db.CreateParticipant("id-1", "octocat", "Octo", true); err != nil {
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

func TestNewDB_BackfillsHasGitHubOnceForOldDatabases(t *testing.T) {
	path := t.TempDir() + "/old.db"
	old, err := NewDB(path)
	if err != nil {
		t.Fatal(err)
	}
	old.CreateParticipant("gh", "octocat", "Octo", true)
	old.CreateParticipant("ng", "no-github-1234abcd", "Ada", true)
	if _, err := old.db.Exec(`ALTER TABLE participants DROP COLUMN has_github`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	db, err := NewDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reload(t, db, "gh").HasGitHub || reload(t, db, "ng").HasGitHub {
		t.Error("backfill: the generated-handle Participant has no GitHub account, the other does")
	}
	db.db.Exec(`UPDATE participants SET has_github = 1 WHERE id = 'ng'`)
	db.Close()

	reopened, _ := NewDB(path)
	defer reopened.Close()
	if !reload(t, reopened, "ng").HasGitHub {
		t.Error("the backfill must only run once, when the column is introduced")
	}
}

func TestInTx_ReportsAFailedCommit(t *testing.T) {
	db := testDB(t)

	err := db.inTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`ROLLBACK`) // the transaction ends early, so the commit fails
		return err
	})

	if err == nil {
		t.Error("want the failed commit reported")
	}
}

func TestNewDB_ReportsADatabaseThatCannotBeOpened(t *testing.T) {
	if _, err := NewDB(t.TempDir() + "/missing-dir/x.db"); err == nil {
		t.Error("a database in a missing directory should not open")
	}
	garbage := t.TempDir() + "/garbage.db"
	os.WriteFile(garbage, []byte("this is not a SQLite database, just a long enough line of text to fill a header"), 0644)
	if _, err := NewDB(garbage); err == nil {
		t.Error("a file that is not a database should not open")
	}
}

func TestParticipants_CorruptRowsAreErrorsNotPanics(t *testing.T) {
	for _, column := range []string{"profile_json", "questions", "answers_json", "interests"} {
		t.Run(column, func(t *testing.T) {
			db := testDB(t)
			db.CreateParticipant("p", "p", "P", true)
			db.db.Exec(`UPDATE participants SET ` + column + ` = '{broken' WHERE id = 'p'`)

			if _, err := db.GetParticipant("p"); err == nil {
				t.Error("a corrupt row should be an error")
			}
			if _, err := db.GetAllParticipants(); err == nil {
				t.Error("listing should report the corrupt row")
			}
		})
	}
}

func TestDB_ReadsAndWritesAgainstABrokenDatabase(t *testing.T) {
	db := testDB(t)
	db.SetLLMCache("a:b", &matchResult{Score: 1})
	db.db.Close()

	if _, ok := db.GetLLMCache("a:b"); ok {
		t.Error("nothing can be read from a broken database")
	}
	db.SetLLMCache("a:b", &matchResult{Score: 2}) // logged, not fatal
	db.ClearLLMCache()                            // logged, not fatal
	if _, err := db.GetAllParticipants(); err == nil {
		t.Error("listing should fail")
	}
	if _, err := db.GetRecentActivity(5); err == nil {
		t.Error("reading activity should fail")
	}
}

func TestLLMCache_NullListsComeBackEmpty(t *testing.T) {
	db := testDB(t)
	db.db.Exec(`INSERT INTO llm_cache (pair_key, score, reason, red_flags, green_flags, icebreakers) VALUES ('a:b', 5, 'x', 'null', '[]', '[]')`)

	r, ok := db.GetLLMCache("a:b")

	if !ok || r.RedFlags == nil || len(r.RedFlags) != 0 {
		t.Errorf("want an empty list, got %+v", r)
	}
}

func TestReset_ReportsWhenActivityCannotBeCleared(t *testing.T) {
	db := testDB(t)
	db.LogActivity("hello")
	db.db.Exec(`CREATE TRIGGER keep_activity BEFORE DELETE ON activity_log BEGIN SELECT RAISE(ABORT, 'injected'); END`)

	if err := db.Reset(); err == nil {
		t.Error("want the failure reported")
	}
}

func TestCreateParticipant_ReportsADamagedSchema(t *testing.T) {
	db := testDB(t)
	db.db.Exec(`ALTER TABLE participants DROP COLUMN persona_symbol`)

	if err := db.CreateParticipant("p", "p", "P", true); err == nil {
		t.Error("want an error when Persona looks cannot be read")
	}
}

func TestNewDB_ReportsADatabaseThatIsReadOnly(t *testing.T) {
	path := t.TempDir() + "/readonly.db"
	os.WriteFile(path, nil, 0644)

	// mode=ro opens the (empty) file read-only, so creating the schema fails.
	if _, err := NewDB("file:" + path + "?mode=ro&"); err == nil {
		t.Error("a read-only database cannot get its schema")
	}
}
