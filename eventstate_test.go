package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestEventState_OnlyAdvanceAndResetChangeIt(t *testing.T) {
	srv, deps := newTestServer(t, newFakeLLM().on("matchmaker", `{"score": 70}`), nil)
	seed(t, deps.db, "a", "A", "ready")
	seed(t, deps.db, "b", "B", "ready")

	if got := deps.events.State(); got != PhaseOnboarding {
		t.Fatalf("a new event starts in onboarding, got %q", got)
	}
	post(t, srv, "/admin/rematch", nil)
	eventually(t, "rematched", func() bool { return reload(t, deps.db, "a").IsMatched() })
	if got := deps.events.State(); got != PhaseOnboarding {
		t.Errorf("a rematch must not change the Phase, got %q", got)
	}

	post(t, srv, "/admin/reveal", nil)
	if got := deps.events.State(); got != PhaseSocial || !got.AtLeast(PhaseSocial) {
		t.Errorf("after the Reveal: got %q", got)
	}
	var graph struct {
		Revealed bool `json:"revealed"`
	}
	json.Unmarshal([]byte(readBody(t, get(t, srv, "/bigscreen/graph-data"))), &graph)
	if !graph.Revealed {
		t.Error("the Big Screen should be told the Matches are revealed")
	}

	post(t, srv, "/admin/reset", nil)
	if got := deps.events.State(); got != PhaseOnboarding {
		t.Errorf("Reset returns the event to onboarding, got %q", got)
	}
}

func TestEventState_ExistingDatabasesKeepTheirState(t *testing.T) {
	path := t.TempDir() + "/event.db"
	db, err := NewDB(path)
	if err != nil {
		t.Fatal(err)
	}
	NewEventStore(db).Reveal()
	db.Close()

	reopened, err := NewDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got := NewEventStore(reopened).State(); got != PhaseSocial {
		t.Errorf("want social after reopening, got %q", got)
	}
}

// TestNewDB_MapsTheLegacyRevealedValueToSocial covers #113's migration: a
// database holding the old 'revealed' value reads as the Social phase after
// startup. Schema-migration test: no store operation writes 'revealed'
// anymore — the Reveal became the Social phase — so the legacy row is
// written directly.
func TestNewDB_MapsTheLegacyRevealedValueToSocial(t *testing.T) {
	path := t.TempDir() + "/event.db"
	db, err := NewDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`INSERT OR REPLACE INTO event_state (key, value) VALUES ('phase', 'revealed')`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	reopened, err := NewDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got := NewEventStore(reopened).State(); got != PhaseSocial {
		t.Errorf("the legacy 'revealed' value should read as social, got %q", got)
	}
}

// TestEventStore_PublicTheme covers the Public theme's storage (#172): an
// absent key reads as the default, the set/get roundtrip works, and the
// Host's choice survives a Reset — Reset rewrites only the phase key, and
// the Public theme is a console preference about the venue, not event data.
func TestEventStore_PublicTheme(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)

	if got := deps.events.PublicTheme(); got != themeDefault {
		t.Errorf("an absent public_theme: want %q, got %q", themeDefault, got)
	}

	if err := deps.events.SetPublicTheme("daylight"); err != nil {
		t.Fatalf("SetPublicTheme: %v", err)
	}
	if got := deps.events.PublicTheme(); got != "daylight" {
		t.Errorf("set/get roundtrip: want daylight, got %q", got)
	}

	if resp := post(t, srv, "/admin/reset", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /admin/reset: want 303, got %d", resp.StatusCode)
	}
	if got := deps.events.PublicTheme(); got != "daylight" {
		t.Errorf("the public theme must survive a Reset, got %q", got)
	}
}
