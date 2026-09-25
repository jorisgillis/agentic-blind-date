package main

import (
	"testing"
)

// failWrites makes Participant writes fail until the returned function is
// called (or the test ends): every write, or, when tags are given, only
// writes tagged with one of those. Tags name what a write is, not which SQL
// column it touches: "create", "delete", "interests", "persona", "profile",
// "questions", "answers", "pipeline_step", "start_interview", "pair" and
// "unpair" (see ParticipantChange.tag and DB.checkFault's call sites). This
// is a test-only hook on DB (see DB.failWrite), not a SQL trigger, so
// production code needs no fault-injection hooks.
func failWrites(t *testing.T, db *DB, tags ...string) (restore func()) {
	t.Helper()
	db.setFailWrite(func(tag string) error {
		if len(tags) == 0 {
			return fakeError("injected write failure")
		}
		for _, want := range tags {
			if tag == want {
				return fakeError("injected write failure")
			}
		}
		return nil
	})
	restored := false
	restore = func() {
		if restored {
			return
		}
		restored = true
		db.setFailWrite(nil)
	}
	t.Cleanup(restore)
	return restore
}

// breakDB makes every database call fail, reads included.
func breakDB(db *DB) {
	db.db.Close()
}

// breakOnFault cancels the write tagged tag the moment it is about to
// happen, so that write fails as a genuine database error, not just the
// fault hook's own check — for the branch that handles a write which passed
// every guard but then failed in the database itself. Not a SQL trigger:
// DB.execCtx's context is cancelled for that one write, then replaced.
func breakOnFault(t *testing.T, db *DB, tag string) {
	t.Helper()
	db.setFailWrite(func(got string) error {
		if got == tag {
			db.cancelCurrentWrite()
		}
		return nil
	})
	t.Cleanup(func() { db.setFailWrite(nil) })
}

func TestFailWrites_MakesParticipantWritesFailUntilRestored(t *testing.T) {
	db := newTestDB(t)
	db.CreateParticipant("p", "p", "P", true)
	store := NewParticipantStore(db)

	restore := failWrites(t, db)
	err := store.Change(ParticipantChange{ID: "p", Persona: &Persona{Name: "The Gopher"}})
	if err == nil || err.Error() != "injected write failure" {
		t.Fatalf("want an injected failure, got %v", err)
	}
	if err := db.CreateParticipant("q", "q", "Q", true); err == nil {
		t.Error("inserts should fail too")
	}

	restore()
	if err := store.Change(ParticipantChange{ID: "p", Persona: &Persona{Name: "The Gopher"}}); err != nil {
		t.Errorf("after restoring, writes succeed: %v", err)
	}
	if got := reload(t, db, "p").PersonaName; got != "The Gopher" {
		t.Errorf("persona: got %q", got)
	}
}

func TestFailWrites_CanTargetATag(t *testing.T) {
	db := newTestDB(t)
	db.CreateParticipant("p", "p", "P", true)
	store := NewParticipantStore(db)
	failWrites(t, db, "persona")

	if err := store.Change(ParticipantChange{ID: "p", Persona: &Persona{Name: "The Gopher"}}); err == nil {
		t.Error("the targeted tag should fail")
	}
	if err := store.Change(ParticipantChange{ID: "p", Interests: &Interests{Languages: []string{"Go"}}}); err != nil {
		t.Errorf("other tags stay writable: %v", err)
	}
}
