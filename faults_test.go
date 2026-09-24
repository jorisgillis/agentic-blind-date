package main

import (
	"strings"
	"testing"
)

// failWrites makes writes to Participants fail until the returned function is
// called (or the test ends): every insert, update and delete, or, when columns
// are given, only updates of those columns. It uses SQLite triggers created
// only here, so production code needs no fault-injection hooks.
func failWrites(t *testing.T, db *DB, columns ...string) (restore func()) {
	t.Helper()
	triggers := map[string]string{
		"fail_insert": "INSERT",
		"fail_update": "UPDATE",
		"fail_delete": "DELETE",
	}
	if len(columns) > 0 {
		triggers = map[string]string{"fail_update_of": "UPDATE OF " + strings.Join(columns, ", ")}
	}
	for name, event := range triggers {
		stmt := `CREATE TRIGGER ` + name + ` BEFORE ` + event +
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
		for name := range triggers {
			db.db.Exec(`DROP TRIGGER IF EXISTS ` + name)
		}
	}
	t.Cleanup(restore)
	return restore
}

// breakDB makes every database call fail, reads included.
func breakDB(db *DB) {
	db.db.Close()
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

func TestFailWrites_CanTargetColumns(t *testing.T) {
	db := newTestDB(t)
	db.CreateParticipant("p", "p", "P", true)
	failWrites(t, db, "persona_name")

	if err := db.SetPersona("p", "The Gopher", ""); err == nil {
		t.Error("updating the targeted column should fail")
	}
	if err := db.SetQuestions("p", nil); err != nil {
		t.Errorf("other columns stay writable: %v", err)
	}
}
