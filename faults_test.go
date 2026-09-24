package main

import (
	"strings"
	"testing"
)

// failWrites makes every write to Participants fail until the returned
// function is called (or the test ends). It uses SQLite triggers created only
// here, so production code needs no fault-injection hooks.
func failWrites(t *testing.T, db *DB) (restore func()) {
	t.Helper()
	for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
		stmt := `CREATE TRIGGER IF NOT EXISTS fail_` + strings.ToLower(op) + ` BEFORE ` + op +
			` ON participants BEGIN SELECT RAISE(ABORT, 'injected write failure'); END`
		if _, err := db.db.Exec(stmt); err != nil {
			t.Fatalf("installing write failure: %v", err)
		}
	}
	restored := false
	restore = func() {
		if restored {
			return
		}
		restored = true
		for _, op := range []string{"insert", "update", "delete"} {
			db.db.Exec(`DROP TRIGGER IF EXISTS fail_` + op)
		}
	}
	t.Cleanup(restore)
	return restore
}

func TestFailWrites_MakesParticipantWritesFailUntilRestored(t *testing.T) {
	db := newTestDB(t)
	db.CreateParticipant("p", "p", "P", true)

	restore := failWrites(t, db)
	if err := db.SetPersona("p", "The Gopher", ""); err == nil || !strings.Contains(err.Error(), "injected write failure") {
		t.Fatalf("want an injected failure, got %v", err)
	}
	if err := db.CreateParticipant("q", "q", "Q", true); err == nil {
		t.Error("inserts should fail too")
	}

	restore()
	if err := db.SetPersona("p", "The Gopher", ""); err != nil {
		t.Errorf("after restoring, writes succeed: %v", err)
	}
	if got := reload(t, db, "p").PersonaName; got != "The Gopher" {
		t.Errorf("persona: got %q", got)
	}
}
