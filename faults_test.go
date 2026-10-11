package main

import (
	"testing"
)

// Fault injection for the error paths (ADR-0008) lives entirely in this
// file: failWrites and breakOnFault install SQLite triggers — or, for the
// two read tags, a temporary view — that make the targeted write or read
// fail inside the database itself, so production code needs no hooks.
//
// Tags name what a write (or, for "persona_lookup", "activity_read",
// "meet_backfill_read", "meet_read", "mission_read" and "connection_read", a
// read) is, not which SQL column it touches: "create", "persona_lookup",
// "delete", "interests", "persona", "profile", "answers", "pipeline_step",
// "start_interview", "pair", "unpair", "activity_read", "meet_code",
// "meet_insert", "meet_update", "meet_complete", "meet_delete",
// "meet_backfill_read", "meet_read", "mission_insert", "mission_delete",
// "mission_read", "connection_read", "reset_participants", "reset_activity",
// "reset_event_state", "reset_meetings" and "reset_mission_cards". Each tag
// maps to exactly the write it names, so a test can fail one write and leave
// the others alone.

// failWrites makes writes fail until the returned function is called (or
// the test ends): every write, or, when tags are given, only the writes
// tagged with one of those. Each failure is raised by a SQL trigger, so it
// surfaces from the database as "injected write failure".
func failWrites(t *testing.T, db *DB, tags ...string) (restore func()) {
	t.Helper()
	return installFault(t, db, "injected write failure", tags)
}

// breakOnFault makes the write tagged tag fail as a genuine database error
// — a SQL trigger, so the write really reaches the database and fails there,
// not with an error the test made up — for the branch that handles a write
// which passed every guard but then failed in the database itself. The read
// tags fail the same way: a temporary view shadows the table and fails while
// its rows are being read.
func breakOnFault(t *testing.T, db *DB, tag string) {
	t.Helper()
	installFault(t, db, "database fault", []string{tag})
}

// breakDB makes every database call fail, reads included.
func breakDB(db *DB) {
	db.db.Close()
}

// installFault installs the SQL that fails the writes named by tags — every
// write when tags is empty — raising the given message, and returns a
// function that removes it again.
func installFault(t *testing.T, db *DB, message string, tags []string) (restore func()) {
	t.Helper()
	var faults [][2]string // each fault: the SQL that installs it, the SQL that undoes it
	if len(tags) == 0 {
		faults = allWriteFaults(message)
	} else {
		byTag := writeFaults(message)
		for _, tag := range tags {
			fault, ok := byTag[tag]
			if !ok {
				t.Fatalf("unknown fault tag %q", tag)
			}
			faults = append(faults, fault)
		}
	}
	for _, fault := range faults {
		if _, err := db.db.Exec(fault[0]); err != nil {
			t.Fatalf("installing fault: %v", err)
		}
	}
	restored := false
	restore = func() {
		if restored {
			return
		}
		restored = true
		for _, fault := range faults {
			db.db.Exec(fault[1])
		}
	}
	t.Cleanup(restore)
	return restore
}

// writeFaults maps each tag to the SQL that fails exactly that write (or
// read). A write fails through a trigger whose WHEN clause targets it alone;
// a read fails through a temporary view shadowing the table, whose json()
// call rejects the stored text while the rows are read, so the failure
// surfaces mid-iteration through rows.Err().
func writeFaults(message string) map[string][2]string {
	trigger := func(tag, event, table, when string) [2]string {
		stmt := `CREATE TRIGGER fault_` + tag + ` BEFORE ` + event + ` ON ` + table
		if when != "" {
			stmt += ` WHEN ` + when
		}
		stmt += ` BEGIN SELECT RAISE(ABORT, '` + message + `'); END`
		return [2]string{stmt, `DROP TRIGGER IF EXISTS fault_` + tag}
	}
	return map[string][2]string{
		"create":              trigger("create", "INSERT", "participants", ""),
		"delete":              trigger("delete", "DELETE", "participants", ""),
		"interests":           trigger("interests", "UPDATE", "participants", "NEW.interests <> OLD.interests"),
		"persona":             trigger("persona", "UPDATE", "participants", "NEW.persona_name <> OLD.persona_name OR NEW.persona_tagline <> OLD.persona_tagline"),
		"profile":             trigger("profile", "UPDATE", "participants", "NEW.profile_json <> OLD.profile_json"),
		"answers":             trigger("answers", "UPDATE", "participants", "NEW.answers_json <> OLD.answers_json"),
		"pipeline_step":       trigger("pipeline_step", "UPDATE", "participants", "NEW.pipeline_step <> OLD.pipeline_step"),
		"start_interview":     trigger("start_interview", "UPDATE", "participants", "NEW.questions <> OLD.questions OR NEW.profile_json <> OLD.profile_json"),
		"pair":                trigger("pair", "UPDATE OF matched_with", "participants", "NEW.matched_with <> ''"),
		"unpair":              trigger("unpair", "UPDATE OF matched_with", "participants", "NEW.matched_with = ''"),
		"meet_code":           trigger("meet_code", "UPDATE", "participants", "NEW.meet_code IS NOT OLD.meet_code"),
		"meet_insert":         trigger("meet_insert", "INSERT", "meetings", ""),
		"meet_update":         trigger("meet_update", "UPDATE", "meetings", "NEW.a_confirmed_at IS NOT OLD.a_confirmed_at OR NEW.b_confirmed_at IS NOT OLD.b_confirmed_at"),
		"meet_complete":       trigger("meet_complete", "UPDATE", "meetings", "NEW.completed_at IS NOT OLD.completed_at"),
		"meet_delete":         trigger("meet_delete", "DELETE", "meetings", ""),
		"mission_insert":      trigger("mission_insert", "INSERT", "mission_cards", ""),
		"mission_delete":      trigger("mission_delete", "DELETE", "mission_cards", ""),
		"reset_participants":  trigger("reset_participants", "DELETE", "participants", ""),
		"reset_activity":      trigger("reset_activity", "DELETE", "activity_log", ""),
		"reset_event_state":   trigger("reset_event_state", "INSERT", "event_state", ""),
		"reset_meetings":      trigger("reset_meetings", "DELETE", "meetings", ""),
		"reset_mission_cards": trigger("reset_mission_cards", "DELETE", "mission_cards", ""),
		"persona_lookup": {
			`CREATE TEMP VIEW participants AS SELECT json(persona_color) AS persona_color, persona_symbol FROM main.participants`,
			`DROP VIEW IF EXISTS temp.participants`,
		},
		"activity_read": {
			`CREATE TEMP VIEW activity_log AS SELECT json(message) AS message, created_at FROM main.activity_log`,
			`DROP VIEW IF EXISTS temp.activity_log`,
		},
		"meet_backfill_read": {
			`CREATE TEMP VIEW participants AS SELECT id, json(name) AS meet_code FROM main.participants`,
			`DROP VIEW IF EXISTS temp.participants`,
		},
		// The view keeps the key columns, so the pair's WHERE clause still
		// matches, but NULLs a scanned column, so scanning the row fails.
		"meet_read": {
			`CREATE TEMP VIEW meetings AS SELECT participant_a, participant_b, a_confirmed_at, b_confirmed_at, completed_at, NULL AS origin, NULL AS created_at FROM main.meetings`,
			`DROP VIEW IF EXISTS temp.meetings`,
		},
		// The card's row still matches, but NULLs created_at, so scanning it fails.
		"mission_read": {
			`CREATE TEMP VIEW mission_cards AS SELECT participant_id, tiles, NULL AS created_at FROM main.mission_cards`,
			`DROP VIEW IF EXISTS temp.mission_cards`,
		},
		// The leaderboard's query still prepares, but NULLs persona_color, so
		// scanning a row fails.
		"connection_read": {
			`CREATE TEMP VIEW participants AS SELECT id, persona_name, persona_symbol, NULL AS persona_color FROM main.participants`,
			`DROP VIEW IF EXISTS temp.participants`,
		},
	}
}

// persistMeetCodeFault installs a trigger that fails meet-code writes and
// leaves it installed — unlike installFault's triggers, which a reopen would
// not see (temp views do not survive a new connection, and installFault drops
// its triggers at cleanup). The backfill runs inside NewDB, on a fresh
// connection, so its failure has to come from a persistent trigger.
func persistMeetCodeFault(t *testing.T, db *DB) {
	t.Helper()
	_, err := db.db.Exec(`CREATE TRIGGER fault_meet_code BEFORE UPDATE ON participants WHEN NEW.meet_code IS NOT OLD.meet_code BEGIN SELECT RAISE(ABORT, 'injected write failure'); END`)
	if err != nil {
		t.Fatalf("installing fault: %v", err)
	}
}

// allWriteFaults fails every write path in the database.
func allWriteFaults(message string) [][2]string {
	trigger := func(name, event, table string) [2]string {
		return [2]string{
			`CREATE TRIGGER ` + name + ` BEFORE ` + event + ` ON ` + table + ` BEGIN SELECT RAISE(ABORT, '` + message + `'); END`,
			`DROP TRIGGER IF EXISTS ` + name,
		}
	}
	return [][2]string{
		trigger("fault_insert_participants", "INSERT", "participants"),
		trigger("fault_update_participants", "UPDATE", "participants"),
		trigger("fault_delete_participants", "DELETE", "participants"),
		trigger("fault_insert_activity", "INSERT", "activity_log"),
		trigger("fault_delete_activity", "DELETE", "activity_log"),
		trigger("fault_insert_event_state", "INSERT", "event_state"),
		trigger("fault_insert_cache", "INSERT", "llm_cache"),
		trigger("fault_delete_cache", "DELETE", "llm_cache"),
		trigger("fault_insert_meetings", "INSERT", "meetings"),
		trigger("fault_update_meetings", "UPDATE", "meetings"),
		trigger("fault_delete_meetings", "DELETE", "meetings"),
		trigger("fault_insert_mission_cards", "INSERT", "mission_cards"),
		trigger("fault_delete_mission_cards", "DELETE", "mission_cards"),
	}
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
