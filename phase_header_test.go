package main

import (
	"html"
	"strings"
	"testing"
)

// assertPhaseHeader checks the phase header fragment's anchored root, its
// progress strip's done/current/upcoming states, the four step labels in the
// evening's order, the summary line and the Participant's status line. The
// participant under test is "me".
func assertPhaseHeader(t *testing.T, body string, phase Phase, summary, status string) {
	t.Helper()
	// The anchored fragment, refreshed by the page's stream.
	for _, want := range []string{
		`id="phase-header"`,
		`hx-get="/user/phase-header/me"`,
		`hx-trigger="refresh"`,
		`hx-swap="outerHTML"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the phase header should contain %q", want)
		}
	}
	// The progress strip: every earlier step done, the current one current,
	// the rest upcoming.
	for _, want := range []struct {
		state string
		n     int
	}{
		{"done", phase.rank()},
		{"current", 1},
		{"upcoming", len(phases) - 1 - phase.rank()},
	} {
		if n := strings.Count(body, `data-state="`+want.state+`"`); n != want.n {
			t.Errorf("the strip should show %d %s steps, got %d", want.n, want.state, n)
		}
	}
	if n := strings.Count(body, `aria-current="step"`); n != 1 {
		t.Errorf("exactly the current step should carry aria-current, got %d", n)
	}
	// The four step labels, in the evening's order.
	last := -1
	for _, label := range []string{"Onboarding", "Social", "Show &amp; tell", "Closed"} {
		idx := strings.Index(body, label)
		if idx < 0 {
			t.Errorf("the strip should contain the %q step", label)
			continue
		}
		if idx < last {
			t.Errorf("the %q step appears out of order", label)
		}
		last = idx
	}
	// The template HTML-escapes what it renders.
	for _, want := range []string{summary, status} {
		if !strings.Contains(body, html.EscapeString(want)) {
			t.Errorf("the header should contain %q, got: %s", want, body)
		}
	}
}

// TestPhaseHeader_RendersTheStripSummaryAndStatusPerPhase covers #136's
// fragment per phase: the progress strip's done/current/upcoming states, the
// one-line "Tonight: …" summary, and the Participant's own status line. The
// late-joiner case is just this — rendering at the current phase, with the
// earlier steps done.
func TestPhaseHeader_RendersTheStripSummaryAndStatusPerPhase(t *testing.T) {
	t.Run(string(PhaseOnboarding), func(t *testing.T) {
		srv, deps := newTestServer(t, nil, nil)
		if err := deps.db.CreateParticipant("me", "me", "Me", true); err != nil {
			t.Fatal(err)
		}
		forceQuestions(deps.db, "me", []Question{{ID: "q1", Text: "Tabs?"}, {ID: "q2", Text: "Vim?"}})
		updateAnswers(t, deps.db, "me", map[string]string{"q1": "yes"})

		body := readBody(t, get(t, srv, "/user/phase-header/me"))

		assertPhaseHeader(t, body, PhaseOnboarding,
			"Tonight: register, answer the interview, and wait for your match",
			"Interview: 1 of 2 answered")
	})

	t.Run(string(PhaseSocial), func(t *testing.T) {
		srv, deps := newTestServer(t, missionLLM(), nil)
		if err := deps.events.Advance(PhaseSocial); err != nil {
			t.Fatal(err)
		}
		seed(t, deps.db, "me", "The Me", "ready")
		seedReady(t, deps.db, 9)
		if _, err := deps.missions.GenerateFor("me"); err != nil {
			t.Fatal(err)
		}

		body := readBody(t, get(t, srv, "/user/phase-header/me"))
		assertPhaseHeader(t, body, PhaseSocial,
			"Tonight: the matches are revealed — meet the personas on your mission card",
			"Mission: 0 of 9 tiles stamped")

		// A completed meeting stamps the tile: the header's mission progress
		// follows the stamps.
		card, err := deps.missions.Get("me")
		if err != nil || card == nil {
			t.Fatalf("me's card: %+v (err %v)", card, err)
		}
		ms := NewMeetings(deps.db, deps.events)
		me := reload(t, deps.db, "me")
		them := reload(t, deps.db, card.Tiles[0].PersonID)
		if _, _, err := ms.Confirm(me, them, OriginMission); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ms.Confirm(them, me, OriginMission); err != nil {
			t.Fatal(err)
		}

		body = readBody(t, get(t, srv, "/user/phase-header/me"))
		assertPhaseHeader(t, body, PhaseSocial,
			"Tonight: the matches are revealed — meet the personas on your mission card",
			"Mission: 1 of 9 tiles stamped")
	})

	t.Run(string(PhaseShowTell), func(t *testing.T) {
		srv, deps := newTestServer(t, nil, nil)
		seed(t, deps.db, "me", "The Me", "ready")
		if err := deps.events.Advance(PhaseShowTell); err != nil {
			t.Fatal(err)
		}

		body := readBody(t, get(t, srv, "/user/phase-header/me"))

		assertPhaseHeader(t, body, PhaseShowTell,
			"Tonight: show & tell — sign up to present, or ask the one on stage",
			"Show & tell: audience questions open")
	})

	t.Run(string(PhaseClosed), func(t *testing.T) {
		srv, deps := newTestServer(t, nil, nil)
		seed(t, deps.db, "me", "The Me", "ready")
		if err := deps.events.Advance(PhaseClosed); err != nil {
			t.Fatal(err)
		}

		body := readBody(t, get(t, srv, "/user/phase-header/me"))

		assertPhaseHeader(t, body, PhaseClosed,
			"Tonight: the event is closed — your evening is kept in your recap",
			"Your recap is kept — scan the recap QR at the bottom of the page")
	})
}

// TestPhaseHeader_ShowsThePreparingLineWhileTheMissionCardIsGenerated covers
// the social's status line before the Participant's card exists.
func TestPhaseHeader_ShowsThePreparingLineWhileTheMissionCardIsGenerated(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "me", "The Me", "ready")
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}

	body := readBody(t, get(t, srv, "/user/phase-header/me"))

	if !strings.Contains(body, "Mission: your card is being prepared") {
		t.Errorf("want the preparing line while the card is generated, got: %s", body)
	}
}

// TestPhaseHeader_ReportsAnUnreadableMissionCard covers the social's status
// line when the card cannot be read: the header still renders, with the
// preparing line.
func TestPhaseHeader_ReportsAnUnreadableMissionCard(t *testing.T) {
	srv, deps := newTestServer(t, missionLLM(), nil)
	seed(t, deps.db, "me", "The Me", "ready")
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	// The read fault only fires when the card's row is scanned.
	if err := deps.missions.save(&MissionCard{ParticipantID: "me"}); err != nil {
		t.Fatal(err)
	}
	breakOnFault(t, deps.db, "mission_read")

	resp := get(t, srv, "/user/phase-header/me")
	if resp.StatusCode != 200 {
		t.Fatalf("want the header to render, got %d", resp.StatusCode)
	}
	if body := readBody(t, resp); !strings.Contains(body, "Mission: your card is being prepared") {
		t.Errorf("an unreadable card shows the preparing line, got: %s", body)
	}
}

// TestPhaseHeader_Endpoint covers #136's endpoint: the fragment for a known
// Participant, 404 for an unknown one.
func TestPhaseHeader_Endpoint(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "me", "The Me", "ready")

	resp := get(t, srv, "/user/phase-header/me")
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	for _, want := range []string{`id="phase-header"`, `hx-get="/user/phase-header/me"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the endpoint should return the fragment with %q, got: %s", want, body)
		}
	}
	if resp := get(t, srv, "/user/phase-header/nobody"); resp.StatusCode != 404 {
		t.Errorf("unknown: want 404, got %d", resp.StatusCode)
	}
}
