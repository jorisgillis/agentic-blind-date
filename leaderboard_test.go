package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// This file covers ticket #116: the connection leaderboard's derivation and
// its deterministic tie-break, the Champion (the Merge Trophy winner), the
// meeting edges in the Big Screen's graph payload, the phase-aware scoreboard
// modes, the celebration of a completion on the screen's push stream, and the
// /data/leaderboard debug endpoint.

// completeMeeting confirms the meeting between a and b from both sides.
func completeMeeting(t *testing.T, db *DB, ms *Meetings, a, b string) {
	t.Helper()
	x, y := reload(t, db, a), reload(t, db, b)
	if _, _, err := ms.Confirm(x, y, OriginOther); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ms.Confirm(y, x, OriginOther); err != nil {
		t.Fatal(err)
	}
}

// screenPayload is the Big Screen's graph payload as the tests read it.
type screenPayload struct {
	Phase       Phase                `json:"phase"`
	Edges       []graphEdge          `json:"edges"`
	Leaderboard []ConnectionStanding `json:"leaderboard"`
	Champion    *ConnectionStanding  `json:"champion"`
	Activity    []string             `json:"activity"`
}

// graphPayloadBody fetches the graph payload's raw JSON.
func graphPayloadBody(t *testing.T, srv *testSrv) string {
	t.Helper()
	return readBody(t, get(t, srv, "/bigscreen/graph-data"))
}

// graphPayload fetches and decodes the graph payload.
func graphPayload(t *testing.T, srv *testSrv) screenPayload {
	t.Helper()
	var p screenPayload
	if err := json.Unmarshal([]byte(graphPayloadBody(t, srv)), &p); err != nil {
		t.Fatalf("decoding the graph payload: %v", err)
	}
	return p
}

// edgesBetween lists the payload's edges joining the pair, in either direction.
func edgesBetween(p screenPayload, a, b string) []graphEdge {
	var out []graphEdge
	for _, e := range p.Edges {
		if (e.Source == a && e.Target == b) || (e.Source == b && e.Target == a) {
			out = append(out, e)
		}
	}
	return out
}

// ── the champion ───────────────────────────────────────────────────────────

func TestMeetings_Champion(t *testing.T) {
	t.Run("an empty event has no champion", func(t *testing.T) {
		db := newTestDB(t)
		ms := NewMeetings(db, NewEventStore(db))

		champion, err := ms.Champion()
		if err != nil {
			t.Fatal(err)
		}
		if champion != nil {
			t.Errorf("want no champion, got %+v", champion)
		}
	})
	t.Run("the most connections wins", func(t *testing.T) {
		db, _, ms, _, _ := meetingSetup(t)
		seed(t, db, "c", "The Aardvark", "ready")
		seed(t, db, "d", "The Zebra", "ready")
		completeMeeting(t, db, ms, "a", "b")
		completeMeeting(t, db, ms, "a", "c")
		completeMeeting(t, db, ms, "a", "d")

		champion, err := ms.Champion()
		if err != nil {
			t.Fatal(err)
		}
		if champion == nil || champion.ID != "a" || champion.Name != "The Gopher" || champion.Connections != 3 {
			t.Errorf("the champion: want The Gopher with 3 connections, got %+v", champion)
		}
	})
	t.Run("ties break by persona name", func(t *testing.T) {
		db, _, ms, _, _ := meetingSetup(t)
		// a and b tie at one connection; The Crab sorts before The Gopher.
		completeMeeting(t, db, ms, "a", "b")

		champion, err := ms.Champion()
		if err != nil {
			t.Fatal(err)
		}
		if champion == nil || champion.ID != "b" || champion.Name != "The Crab" || champion.Connections != 1 {
			t.Errorf("the champion: want The Crab with 1 connection, got %+v", champion)
		}
	})
	t.Run("a read failure is reported", func(t *testing.T) {
		db, _, ms, _, _ := meetingSetup(t)
		breakOnFault(t, db, "persona_lookup")
		if _, err := ms.Champion(); err == nil {
			t.Error("want a failure when the leaderboard cannot be read")
		}
	})
}

// ── meeting edges in the graph payload ─────────────────────────────────────

func TestGraphPayload_MeetingEdges(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	seed(t, deps.db, "c", "The Hermit", "ready")
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	ms := NewMeetings(deps.db, deps.events)

	// No meetings: no meeting edges.
	for _, e := range graphPayload(t, srv).Edges {
		if e.Meeting {
			t.Errorf("without meetings no edge is a meeting edge: %+v", e)
		}
	}

	// One side only: the meeting is pending, still no meeting edge.
	if _, _, err := ms.Confirm(reload(t, deps.db, "a"), reload(t, deps.db, "b"), OriginOther); err != nil {
		t.Fatal(err)
	}
	for _, e := range edgesBetween(graphPayload(t, srv), "a", "b") {
		if e.Meeting {
			t.Errorf("a pending meeting is not a meeting edge: %+v", e)
		}
	}

	// Both sides: the completed meeting joins the graph as a meeting edge.
	completeMeeting(t, deps.db, ms, "a", "b")
	between := edgesBetween(graphPayload(t, srv), "a", "b")
	if len(between) != 1 {
		t.Fatalf("a completed meeting is one edge, got %d: %+v", len(between), between)
	}
	if !between[0].Meeting || between[0].Matched {
		t.Errorf("the meeting edge is a meeting, not a match: %+v", between[0])
	}

	// A pair that are matched and met keeps one edge, flagged as both — the
	// meeting is the stronger fact.
	pair(t, deps.db, "b", "c")
	completeMeeting(t, deps.db, ms, "b", "c")
	between = edgesBetween(graphPayload(t, srv), "b", "c")
	if len(between) != 1 {
		t.Fatalf("a matched pair that met keeps one edge, got %d: %+v", len(between), between)
	}
	if !between[0].Matched || !between[0].Meeting {
		t.Errorf("want one edge flagged as both match and meeting, got %+v", between[0])
	}
}

// ── the phase-aware scoreboard ─────────────────────────────────────────────

func TestGraphPayload_TheScoreboardIsPhaseAware(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		seed(t, deps.db, id, "The "+id, "ready")
	}
	ms := NewMeetings(deps.db, deps.events)

	// Onboarding: the graph and the ticker only, no scoreboard.
	if body := graphPayloadBody(t, srv); strings.Contains(body, `"leaderboard"`) {
		t.Error("during onboarding the payload carries no leaderboard")
	}

	// The social: the scoreboard arrives, capped at the top 5.
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	completeMeeting(t, deps.db, ms, "a", "b")
	completeMeeting(t, deps.db, ms, "a", "c")
	p := graphPayload(t, srv)
	if len(p.Leaderboard) != 5 {
		t.Fatalf("the scoreboard shows the top 5, got %d", len(p.Leaderboard))
	}
	wantOrder := []struct {
		id          string
		connections int
	}{
		{"a", 2}, {"b", 1}, {"c", 1}, {"d", 0}, {"e", 0},
	}
	for i, want := range wantOrder {
		if p.Leaderboard[i].ID != want.id || p.Leaderboard[i].Connections != want.connections {
			t.Errorf("leaderboard[%d]: want %s with %d connections, got %+v", i, want.id, want.connections, p.Leaderboard[i])
		}
	}
	if p.Champion != nil {
		t.Errorf("the champion is named at the close, not during the social, got %+v", p.Champion)
	}

	// Show & tell: the scoreboard stays.
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	if p := graphPayload(t, srv); len(p.Leaderboard) != 5 {
		t.Errorf("during show & tell the scoreboard stays, got %d entries", len(p.Leaderboard))
	}

	// Closed: the final leaderboard, and the champion named.
	if err := deps.events.Advance(PhaseClosed); err != nil {
		t.Fatal(err)
	}
	p = graphPayload(t, srv)
	if len(p.Leaderboard) != 5 {
		t.Errorf("the final leaderboard is on the payload, got %d entries", len(p.Leaderboard))
	}
	if p.Champion == nil || p.Champion.ID != "a" || p.Champion.Connections != 2 {
		t.Errorf("the champion is the top of the final leaderboard, got %+v", p.Champion)
	}
}

func TestGraphPayload_TheScoreboardSurvivesReadFailures(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The A", "ready")
	if err := deps.events.Advance(PhaseClosed); err != nil {
		t.Fatal(err)
	}
	breakOnFault(t, deps.db, "persona_lookup")

	// The graph still renders; the scoreboard is simply absent.
	body := graphPayloadBody(t, srv)
	if strings.Contains(body, `"leaderboard"`) || strings.Contains(body, `"champion"`) {
		t.Error("without a readable leaderboard the payload carries no scoreboard")
	}
	if !strings.Contains(body, `"nodes"`) {
		t.Error("the graph should still render")
	}
}

// ── the celebration: the completion arrives by push ────────────────────────

func TestScreenStream_PushesTheCompletion(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}

	s := open(t, srv, "/bigscreen/stream")
	waitFor(t, s, "The Gopher")

	// The completion: the meeting edge, the scoreboard and the ticker
	// announcement all arrive by push — the screen celebrates them. The
	// stream marshals compactly, without the indented fallback's spaces.
	completeMeeting(t, deps.db, NewMeetings(deps.db, deps.events), "a", "b")
	waitFor(t, s, `"meeting":true`)
	waitFor(t, s, `"leaderboard"`)
	waitFor(t, s, "🤝 The Gopher and The Crab met!")
}

// ── the champion is named at the close ─────────────────────────────────────

func TestPhase_TheChampionIsNamedAtTheClose(t *testing.T) {
	t.Run("the Merge Trophy winner is named on the ticker", func(t *testing.T) {
		srv, deps := newTestServer(t, nil, nil)
		seed(t, deps.db, "a", "The Gopher", "ready")
		seed(t, deps.db, "b", "The Crab", "ready")
		seed(t, deps.db, "c", "The Hermit", "ready")
		if err := deps.events.Advance(PhaseSocial); err != nil {
			t.Fatal(err)
		}
		ms := NewMeetings(deps.db, deps.events)
		completeMeeting(t, deps.db, ms, "a", "b")
		completeMeeting(t, deps.db, ms, "a", "c")

		if resp := post(t, srv, "/admin/phase", url.Values{"to": {"closed"}}); resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("closing the event: want 303, got %d", resp.StatusCode)
		}
		msgs, err := deps.db.GetRecentActivity(10)
		if err != nil {
			t.Fatal(err)
		}
		logged := strings.Join(msgs, "\n")
		if !strings.Contains(logged, "The Gopher") || !strings.Contains(logged, "takes the Merge Trophy with 2 connections!") {
			t.Errorf("the ticker should name the champion, got %v", msgs)
		}
	})
	t.Run("one connection is singular", func(t *testing.T) {
		srv, deps := newTestServer(t, nil, nil)
		seed(t, deps.db, "a", "The Gopher", "ready")
		seed(t, deps.db, "b", "The Crab", "ready")
		if err := deps.events.Advance(PhaseSocial); err != nil {
			t.Fatal(err)
		}
		completeMeeting(t, deps.db, NewMeetings(deps.db, deps.events), "a", "b")

		post(t, srv, "/admin/phase", url.Values{"to": {"closed"}})
		msgs, err := deps.db.GetRecentActivity(10)
		if err != nil {
			t.Fatal(err)
		}
		if logged := strings.Join(msgs, "\n"); !strings.Contains(logged, "takes the Merge Trophy with 1 connection!") {
			t.Errorf("one connection is singular, got %v", msgs)
		}
	})
	t.Run("no participants, no champion", func(t *testing.T) {
		srv, deps := newTestServer(t, nil, nil)

		post(t, srv, "/admin/phase", url.Values{"to": {"closed"}})
		msgs, err := deps.db.GetRecentActivity(10)
		if err != nil {
			t.Fatal(err)
		}
		if logged := strings.Join(msgs, "\n"); strings.Contains(logged, "Merge Trophy") {
			t.Errorf("without participants no champion is named, got %v", msgs)
		}
	})
	t.Run("a champion that cannot be read is not named", func(t *testing.T) {
		srv, deps := newTestServer(t, nil, nil)
		seed(t, deps.db, "a", "The Gopher", "ready")
		breakOnFault(t, deps.db, "persona_lookup")

		if resp := post(t, srv, "/admin/phase", url.Values{"to": {"closed"}}); resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("closing the event: want 303, got %d", resp.StatusCode)
		}
		msgs, err := deps.db.GetRecentActivity(10)
		if err != nil {
			t.Fatal(err)
		}
		if logged := strings.Join(msgs, "\n"); strings.Contains(logged, "Merge Trophy") {
			t.Errorf("an unreadable champion is not named, got %v", msgs)
		}
	})
}

// ── /data/leaderboard ──────────────────────────────────────────────────────

func TestDataLeaderboard_ShowsTheRanking(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)

	body := readBody(t, get(t, srv, "/data/leaderboard"))
	if trimmed := strings.TrimSpace(body); trimmed != "null" && trimmed != "[]" {
		t.Errorf("expected an empty JSON list, got %s", trimmed)
	}

	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	completeMeeting(t, deps.db, NewMeetings(deps.db, deps.events), "a", "b")

	var board []ConnectionStanding
	body = readBody(t, get(t, srv, "/data/leaderboard"))
	if err := json.Unmarshal([]byte(body), &board); err != nil {
		t.Fatalf("decoding the leaderboard: %v", err)
	}
	if len(board) != 2 {
		t.Fatalf("want 2 standings, got %d", len(board))
	}
	// Ties break by persona name: The Crab ranks above The Gopher.
	if board[0].ID != "b" || board[0].Connections != 1 {
		t.Errorf("board[0]: want The Crab with 1 connection, got %+v", board[0])
	}
	if board[1].ID != "a" || board[1].Connections != 1 {
		t.Errorf("board[1]: want The Gopher with 1 connection, got %+v", board[1])
	}
}

func TestDataLeaderboard_ReportsABrokenDatabase(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	breakDB(deps.db)
	if resp := get(t, srv, "/data/leaderboard"); resp.StatusCode != 500 {
		t.Errorf("a broken database: want 500, got %d", resp.StatusCode)
	}
}
