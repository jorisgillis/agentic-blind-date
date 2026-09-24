package main

import (
	"encoding/json"
	"testing"
)

func TestEventState_OnlyTheRevealAndResetChangeIt(t *testing.T) {
	srv, deps := newTestServer(t, newFakeLLM().on("matchmaker", `{"score": 70}`), nil)
	seed(t, deps.db, "a", "A", "ready")
	seed(t, deps.db, "b", "B", "ready")

	if got := deps.db.EventState(); got != BeforeReveal {
		t.Fatalf("a new event starts before the Reveal, got %q", got)
	}
	post(t, srv, "/admin/rematch", nil)
	eventually(t, "rematched", func() bool { return reload(t, deps.db, "a").IsMatched() })
	if got := deps.db.EventState(); got != BeforeReveal {
		t.Errorf("a rematch must not change the Event State, got %q", got)
	}

	post(t, srv, "/admin/reveal", nil)
	if got := deps.db.EventState(); got != Revealed || !got.IsRevealed() {
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
	if got := deps.db.EventState(); got != BeforeReveal {
		t.Errorf("Reset returns the event to before the Reveal, got %q", got)
	}
}

func TestEventState_ExistingDatabasesKeepTheirState(t *testing.T) {
	path := t.TempDir() + "/event.db"
	db, err := NewDB(path)
	if err != nil {
		t.Fatal(err)
	}
	db.Reveal()
	db.Close()

	reopened, err := NewDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got := reopened.EventState(); got != Revealed {
		t.Errorf("want revealed after reopening, got %q", got)
	}
}
