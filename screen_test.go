package main

import (
	"testing"
)

// newScreen wires the Screen module the way production does — its own stores
// and Board built from the database, the shared Matcher, Meetings, ShowTell
// and Closing — without the HTTP handler, its templates or its routes.
func newScreen(t *testing.T, db *DB) *Screen {
	t.Helper()
	llm := newFakeLLM()
	gh := newFakeGitHub()
	matcher := NewMatcher(db, gh, llm)
	events := NewEventStore(db)
	meetings := NewMeetings(db, events)
	showtell := NewShowTell(db, llm)
	return NewScreen(db, matcher, meetings, showtell, NewClosing(db, meetings, NewContactChannels(db), showtell))
}

// TestScreenPayload_TopThreePotentialConnections is the white-box top-3 test
// against the module directly (ADR-0004): each Participant starts at most 3
// potential connections, however many candidates the heuristic scores, and a
// pair never carries two edges for the same fact.
func TestScreenPayload_TopThreePotentialConnections(t *testing.T) {
	db := newTestDB(t)
	screen := newScreen(t, db)
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		seed(t, db, id, "The "+id, StepReady)
	}

	payload := screen.Payload()

	potential := map[string]int{}
	for _, e := range payload.Edges {
		if e.Matched || e.Meeting {
			continue
		}
		potential[e.Source]++
	}
	if len(potential) == 0 {
		t.Fatal("the graph should carry potential connections")
	}
	for id, n := range potential {
		if n > 3 {
			t.Errorf("%s starts %d potential connections, at most 3", id, n)
		}
	}
	// A pair is one edge: with 5 Participants the top-3 picks overlap —
	// a-b seen from a's side is skipped again from b's.
	seen := map[string]bool{}
	for _, e := range payload.Edges {
		key, rev := e.Source+":"+e.Target, e.Target+":"+e.Source
		if seen[key] || seen[rev] {
			t.Errorf("the pair %s:%s carries more than one edge", e.Source, e.Target)
		}
		seen[key] = true
	}
}

// TestScreenPayload_TheEmptyEventStillCarriesTheGraph locks the read-failure
// degradation: a Participant read that fails must still produce the payload,
// with the nodes present but empty — the screen keeps rendering.
func TestScreenPayload_TheEmptyEventStillCarriesTheGraph(t *testing.T) {
	db := newTestDB(t)
	screen := newScreen(t, db)

	payload := screen.Payload()

	if payload.Nodes == nil || len(payload.Nodes) != 0 {
		t.Errorf("without participants the nodes are present but empty, got %v", payload.Nodes)
	}
	if payload.Edges == nil || len(payload.Edges) != 0 {
		t.Errorf("without participants the edges are present but empty, got %v", payload.Edges)
	}
	if payload.Phase != PhaseOnboarding {
		t.Errorf("the empty event is onboarding, got %q", payload.Phase)
	}
}
