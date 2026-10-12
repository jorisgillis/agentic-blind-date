package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestDeriveMeetCode_IsSixUnambiguousCharacters(t *testing.T) {
	for _, id := range []string{"a", "b", "550e8400-e29b-41d4-a716-446655440000", "no-github-1234abcd"} {
		code := deriveMeetCode(id, 0)
		if len(code) != meetCodeLength {
			t.Errorf("deriveMeetCode(%q): want %d characters, got %d (%q)", id, meetCodeLength, len(code), code)
		}
		for _, c := range code {
			if !strings.ContainsRune(meetCodeAlphabet, c) {
				t.Errorf("deriveMeetCode(%q): %q draws from outside the unambiguous alphabet", id, code)
			}
		}
	}
}

func TestDeriveMeetCode_IsDeterministicAndSalted(t *testing.T) {
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("participant-%d", i)
		if got, want := deriveMeetCode(id, 0), deriveMeetCode(id, 0); got != want {
			t.Fatalf("deriveMeetCode(%q) is not deterministic: %q vs %q", id, got, want)
		}
		if deriveMeetCode(id, 0) == deriveMeetCode(id, 1) {
			t.Errorf("deriveMeetCode(%q): the next salt should re-derive a different code", id)
		}
	}
}

func TestNormalizeMeetCode(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"abc123", "ABC123"},
		{"  ABC123 ", "ABC123"},
		{"\txyz789\n", "XYZ789"},
		{"", ""},
	} {
		if got := normalizeMeetCode(tc.in); got != tc.want {
			t.Errorf("normalizeMeetCode(%q): want %q, got %q", tc.in, tc.want, got)
		}
	}
}

func TestCreateParticipant_AssignsAMeetCodeAtRegistration(t *testing.T) {
	db := newTestDB(t)

	if err := db.CreateParticipant("p", "p", "P", true); err != nil {
		t.Fatal(err)
	}

	p := reload(t, db, "p")
	if p.MeetCode == "" {
		t.Fatal("a Participant should get a meet code at registration")
	}
	if want := deriveMeetCode("p", 0); p.MeetCode != want {
		t.Errorf("meet code: want the code derived from the id %q, got %q", want, p.MeetCode)
	}
	if got := reload(t, db, "p").MeetCode; got != p.MeetCode {
		t.Errorf("the meet code should be stable across reads: %q vs %q", got, p.MeetCode)
	}
}

func TestEnsureMeetCode_IsIdempotent(t *testing.T) {
	db := newTestDB(t)
	db.CreateParticipant("p", "p", "P", true)

	first := reload(t, db, "p").MeetCode
	again, err := ensureMeetCode(db.db, "p")
	if err != nil {
		t.Fatal(err)
	}
	if again != first {
		t.Errorf("an existing code should be returned as-is: %q vs %q", again, first)
	}
}

// TestEnsureMeetCode_RederivesOnCollision covers the collision rule: a code
// that is already taken re-derives deterministically with the next salt.
// Test setup that deliberately writes a colliding meet code — no store
// operation produces a collision (codes derive from unique ids), so the
// colliding row is written directly.
func TestEnsureMeetCode_RederivesOnCollision(t *testing.T) {
	db := newTestDB(t)
	blocking := deriveMeetCode("victim", 0)
	if _, err := db.db.Exec(`INSERT INTO participants (id, github_handle, name, meet_code) VALUES ('blocker', 'blocker', 'Blocker', ?)`, blocking); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`INSERT INTO participants (id, github_handle, name) VALUES ('victim', 'victim', 'Victim')`); err != nil {
		t.Fatal(err)
	}

	code, err := ensureMeetCode(db.db, "victim")
	if err != nil {
		t.Fatal(err)
	}
	if want := deriveMeetCode("victim", 1); code != want {
		t.Errorf("a collision should re-derive with the next salt: want %q, got %q", want, code)
	}
	if got := reload(t, db, "victim").MeetCode; got != code {
		t.Errorf("the re-derived code should be stored: want %q, got %q", code, got)
	}
}

func TestEnsureMeetCode_UnknownParticipant(t *testing.T) {
	db := newTestDB(t)
	if _, err := ensureMeetCode(db.db, "nobody"); err == nil {
		t.Error("want an error for an unknown Participant")
	}
}

func TestEnsureMeetCode_ReportsAWriteFailure(t *testing.T) {
	db := newTestDB(t)
	db.db.Exec(`INSERT INTO participants (id, github_handle, name) VALUES ('p', 'p', 'P')`)
	failWrites(t, db, "meet_code")

	if _, err := ensureMeetCode(db.db, "p"); err == nil || err.Error() != "injected write failure" {
		t.Errorf("want an injected failure, got %v", err)
	}
}

func TestCreateParticipant_ReportsAMeetCodeFailure(t *testing.T) {
	db := newTestDB(t)
	failWrites(t, db, "meet_code")

	if err := db.CreateParticipant("p", "p", "P", true); err == nil {
		t.Error("want a failure when the meet code cannot be assigned")
	}
	if n := db.ParticipantCount(); n != 0 {
		t.Errorf("the failed registration should roll back, %d participants left", n)
	}
}

// TestNewDB_BackfillsMeetCodes covers the backfill: Participants who
// registered before meet codes existed get one at startup. Schema-migration
// test: the codeless row is written directly — before the backfill existed,
// registration wrote no meet_code.
func TestNewDB_BackfillsMeetCodes(t *testing.T) {
	path := t.TempDir() + "/meet.db"
	db, err := NewDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`INSERT INTO participants (id, github_handle, name) VALUES ('old', 'old', 'Old')`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	reopened, err := NewDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	p, err := reopened.GetParticipant("old")
	if err != nil {
		t.Fatal(err)
	}
	if p.MeetCode == "" {
		t.Fatal("an existing Participant should get a meet code backfilled")
	}
	if want := deriveMeetCode("old", 0); p.MeetCode != want {
		t.Errorf("backfilled code: want the code derived from the id %q, got %q", want, p.MeetCode)
	}
}

// TestNewDB_LogsABackfillFailure covers NewDB's backfill error path: a
// persistent trigger fails the backfill's write, and NewDB logs it rather
// than failing startup.
func TestNewDB_LogsABackfillFailure(t *testing.T) {
	path := t.TempDir() + "/meet.db"
	db, err := NewDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`INSERT INTO participants (id, github_handle, name) VALUES ('old', 'old', 'Old')`); err != nil {
		t.Fatal(err)
	}
	persistMeetCodeFault(t, db)
	db.Close()

	reopened, err := NewDB(path)
	if err != nil {
		t.Fatalf("NewDB should log a backfill failure, not fail: %v", err)
	}
	defer reopened.Close()
	if p, _ := reopened.GetParticipant("old"); p.MeetCode != "" {
		t.Errorf("the failed backfill should leave the code empty, got %q", p.MeetCode)
	}
}

func TestBackfillMeetCodes_ReportsAReadFailure(t *testing.T) {
	db := newTestDB(t)
	db.db.Exec(`INSERT INTO participants (id, github_handle, name) VALUES ('p', 'p', 'P')`)
	breakOnFault(t, db, "meet_backfill_read")

	if err := backfillMeetCodes(db); err == nil {
		t.Error("want a failure when the backfill cannot read the Participants")
	}
}

func TestBackfillMeetCodes_ReportsAWriteFailure(t *testing.T) {
	db := newTestDB(t)
	db.db.Exec(`INSERT INTO participants (id, github_handle, name) VALUES ('p', 'p', 'P')`)
	failWrites(t, db, "meet_code")

	if err := backfillMeetCodes(db); err == nil {
		t.Error("want a failure when the backfill cannot assign a code")
	}
}

func TestBackfillMeetCodes_ReportsABrokenDatabase(t *testing.T) {
	db := newTestDB(t)
	breakDB(db)

	if err := backfillMeetCodes(db); err == nil {
		t.Error("want a failure when the database is broken")
	}
}

// TestNewDB_UpgradesADatabaseFromBeforeMeetCodes covers the schema upgrade:
// a database whose Participants predate meet codes gains the column, the
// unique index and backfilled codes at startup. Migration-test setup: the
// column and index are dropped to recreate the schema from before they
// existed.
func TestNewDB_UpgradesADatabaseFromBeforeMeetCodes(t *testing.T) {
	path := t.TempDir() + "/legacy.db"
	db, err := NewDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateParticipant("old", "old", "Old", true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`DROP INDEX idx_participants_meet_code`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`ALTER TABLE participants DROP COLUMN meet_code`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	reopened, err := NewDB(path)
	if err != nil {
		t.Fatalf("a database from before meet codes should upgrade, not fail: %v", err)
	}
	defer reopened.Close()
	p := reload(t, reopened, "old")
	if want := deriveMeetCode("old", 0); p.MeetCode != want {
		t.Errorf("the upgrade should backfill the meet code: want %q, got %q", want, p.MeetCode)
	}
}

// TestNewDB_ReportsADuplicateMeetCode covers the unique index's failure:
// existing codes that collide fail startup loudly rather than silently
// breaking the uniqueness meet codes rely on. Fault-test setup: the index is
// dropped and a colliding code is written directly, bypassing ensureMeetCode.
func TestNewDB_ReportsADuplicateMeetCode(t *testing.T) {
	path := t.TempDir() + "/dupe.db"
	db, err := NewDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`DROP INDEX idx_participants_meet_code`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`INSERT INTO participants (id, github_handle, name, meet_code) VALUES ('a', 'a', 'A', 'AAAAAA'), ('b', 'b', 'B', 'AAAAAA')`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	if _, err := NewDB(path); err == nil {
		t.Error("want a failure when existing meet codes collide")
	}
}
