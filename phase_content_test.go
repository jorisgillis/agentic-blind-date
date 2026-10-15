package main

import (
	"strings"
	"testing"
)

// assertPhaseContentRegion checks the phase-content region's anchored root and
// its wiring: swapped on the stream's 'phase' event, never on 'refresh'. The
// participant under test is "me".
func assertPhaseContentRegion(t *testing.T, body string) {
	t.Helper()
	for _, want := range []string{
		`id="phase-content"`,
		`hx-get="/user/phase-content/me"`,
		`hx-trigger="phase"`,
		`hx-swap="outerHTML"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the phase-content region should contain %q", want)
		}
	}
}

// assertOrder checks that the markers appear in the body in the given order.
func assertOrder(t *testing.T, body string, markers ...string) {
	t.Helper()
	last := -1
	for _, m := range markers {
		idx := strings.Index(body, m)
		if idx < 0 {
			t.Errorf("want %q in the body", m)
			continue
		}
		if idx < last {
			t.Errorf("%q appears out of order", m)
		}
		last = idx
	}
}

// seedMatchedPair seeds a matched pair plus enough ready Participants for a
// full mission card — phase_header_test.go's setup, for a matched pair.
func seedMatchedPair(t *testing.T, deps *testDeps) {
	t.Helper()
	seed(t, deps.db, "me", "The Me", "ready")
	seed(t, deps.db, "mate", "The Mate", "ready")
	seedReady(t, deps.db, 8)
	pair(t, deps.db, "me", "mate")
}

// TestPhaseContent_RendersTheCompositionPerPhase covers #137's per-phase
// render: the region shows only what the current phase calls for — the social
// leads with the mission card, then the meet section, then the match reveal,
// then the show & tell sign-up (open from the social on; the question form
// still waits for the show & tell); closed leads with the
// reveal, then the mission card. The evergreen cards (the dossier, the
// settings, the share-profile QR, the recap link) are not part of the region
// — they live in the page's More drawer (#138).
func TestPhaseContent_RendersTheCompositionPerPhase(t *testing.T) {
	t.Run(string(PhaseSocial), func(t *testing.T) {
		srv, deps := newTestServer(t, missionLLM(), nil)
		seedMatchedPair(t, deps)
		if err := deps.events.Advance(PhaseSocial); err != nil {
			t.Fatal(err)
		}
		if _, err := deps.missions.GenerateFor("me"); err != nil {
			t.Fatal(err)
		}

		body := readBody(t, get(t, srv, "/user/phase-content/me"))

		assertPhaseContentRegion(t, body)
		// The social composition: the mission card first, then the meet
		// section, then the match reveal, then the show & tell sign-up —
		// open from the social on, and the matched Participants who live on
		// the Match page during the social must reach it there.
		assertOrder(t, body,
			`id="mission-card"`,
			`id="meet-status"`,
			"It's a match!",
			"Find them in the room",
			`id="showtell"`,
		)
		assertRegionKeepsOutTheEvergreenCards(t, body)
		if !strings.Contains(body, "Sign me up") {
			t.Error("the social's region should carry the sign-up form")
		}
		// The question form still waits for the show & tell phase.
		if strings.Contains(body, "Ask the presenter") {
			t.Error("the social's region should not carry the question form")
		}
	})

	t.Run(string(PhaseShowTell), func(t *testing.T) {
		srv, deps := newTestServer(t, missionLLM(), nil)
		seedMatchedPair(t, deps)
		if err := deps.events.Advance(PhaseShowTell); err != nil {
			t.Fatal(err)
		}
		if _, err := deps.missions.GenerateFor("me"); err != nil {
			t.Fatal(err)
		}

		body := readBody(t, get(t, srv, "/user/phase-content/me"))

		assertPhaseContentRegion(t, body)
		// The social composition plus the show & tell tooling, after the
		// match reveal.
		assertOrder(t, body,
			`id="mission-card"`,
			`id="meet-status"`,
			"It's a match!",
			`id="showtell"`,
		)
		assertRegionKeepsOutTheEvergreenCards(t, body)
		if !strings.Contains(body, "Sign me up") {
			t.Error("the showtell region should carry the sign-up form")
		}
	})

	t.Run(string(PhaseClosed), func(t *testing.T) {
		srv, deps := newTestServer(t, missionLLM(), nil)
		seedMatchedPair(t, deps)
		if err := deps.events.Advance(PhaseSocial); err != nil {
			t.Fatal(err)
		}
		if _, err := deps.missions.GenerateFor("me"); err != nil {
			t.Fatal(err)
		}
		if err := deps.events.Advance(PhaseClosed); err != nil {
			t.Fatal(err)
		}

		body := readBody(t, get(t, srv, "/user/phase-content/me"))

		assertPhaseContentRegion(t, body)
		// Closed: the match reveal leads, then the mission card in its final
		// stamped state.
		assertOrder(t, body,
			"It's a match!",
			`id="mission-card"`,
		)
		// The recap link is an evergreen card — it lives in the More drawer
		// (#138), so the region carries neither it nor the dossier.
		assertRegionKeepsOutTheEvergreenCards(t, body)
		if !strings.Contains(body, "Your mission — meet 8 personas") {
			t.Error("closed keeps the mission card in its final stamped state")
		}
		// No meet section and no show & tell once closed.
		for _, absent := range []string{`id="meet-status"`, `id="showtell"`} {
			if strings.Contains(body, absent) {
				t.Errorf("closed should not carry %s", absent)
			}
		}
	})
}

// assertRegionKeepsOutTheEvergreenCards checks that no evergreen card remains
// in the phase-content region: the dossier, the settings, the share-profile QR
// and the recap link all live in the page's More drawer (#138).
func assertRegionKeepsOutTheEvergreenCards(t *testing.T, region string) {
	t.Helper()
	for _, absent := range []string{`id="dossier"`, `id="settings"`, `id="qr"`, `id="recap"`, `<details`} {
		if strings.Contains(region, absent) {
			t.Errorf("the phase-content region should not carry %s — it lives in the More drawer", absent)
		}
	}
}

// TestPhaseContent_NoMatchStateRenders covers the region without a Match: the
// no-match card replaces the reveal's match parts. A dangling matched_with —
// no store operation produces it — is the only way onto the match page without
// a resolvable partner (the same impossible state handler_errors_test.go
// stages for the match page).
func TestPhaseContent_NoMatchStateRenders(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "me", "The Me", "ready")
	deps.events.Reveal()
	deps.db.db.Exec(`UPDATE participants SET matched_with = 'gone' WHERE id = 'me'`)

	for _, path := range []string{"/user/phase-content/me", "/user/match/me"} {
		body := readBody(t, get(t, srv, path))
		if !strings.Contains(body, "You don't have a match (yet)") {
			t.Errorf("%s: want the no-match state, got %q", path, body)
		}
		if strings.Contains(body, `id="dossier"`) {
			t.Errorf("%s: no dossier without a match", path)
		}
		if strings.Contains(body, "compatibility") {
			t.Errorf("%s: no score without a match", path)
		}
	}
}

// TestPhaseContent_LateJoinerRendersThePreparingPlaceholder covers the late
// joiner: matched from the social on, their mission card generated on
// reaching ready — until it lands, the region shows the preparing placeholder.
func TestPhaseContent_LateJoinerRendersThePreparingPlaceholder(t *testing.T) {
	srv, deps := newTestServer(t, missionLLM(), nil)
	seedMatchedPair(t, deps)
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	// No GenerateFor: the card is on its way, the placeholder shows.

	body := readBody(t, get(t, srv, "/user/phase-content/me"))

	if !strings.Contains(body, "Your mission card is being prepared") {
		t.Errorf("the late joiner's region should show the preparing placeholder, got %q", body)
	}
}

// TestPhaseContent_Endpoint covers the region's endpoint: the fragment for
// the Participant who belongs on the match page, an HX-Redirect for one who
// belongs on another page, a 404 for an unknown one.
func TestPhaseContent_Endpoint(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "me", "The Me", "ready")
	seed(t, deps.db, "mate", "The Mate", "ready")
	seed(t, deps.db, "solo", "The Solo", "ready")
	seed(t, deps.db, "newbie", "The Newbie", "interviewing")
	pair(t, deps.db, "me", "mate")
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}

	if resp := get(t, srv, "/user/phase-content/me"); resp.StatusCode != 200 {
		t.Errorf("matched from the social on: want the region, got %d", resp.StatusCode)
	}
	if loc := get(t, srv, "/user/phase-content/solo").Header.Get("HX-Redirect"); loc != "/user/wait/solo" {
		t.Errorf("unmatched: want HX-Redirect to wait, got %q", loc)
	}
	if loc := get(t, srv, "/user/phase-content/newbie").Header.Get("HX-Redirect"); loc != "/user/onboard/newbie" {
		t.Errorf("mid-interview: want HX-Redirect to onboard, got %q", loc)
	}
	if resp := get(t, srv, "/user/phase-content/nobody"); resp.StatusCode != 404 {
		t.Errorf("unknown: want 404, got %d", resp.StatusCode)
	}
}

// TestMatchStream_PhaseEventSwapsTheRegion covers #137's push half on the
// match page's stream: a phase transition emits the 'phase' event the region
// listens for, and the region the swap then fetches carries the new phase's
// composition — no reload.
func TestMatchStream_PhaseEventSwapsTheRegion(t *testing.T) {
	srv, deps := newTestServer(t, missionLLM(), nil)
	seedMatchedPair(t, deps)
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.missions.GenerateFor("me"); err != nil {
		t.Fatal(err)
	}

	s := open(t, srv, "/user/match-stream/me")
	waitFor(t, s, "event: refresh") // the sync on connect
	before := s.String()
	sentNothingNew(t, s, 1, before, "nothing changed")

	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, "event: phase")

	// The region the 'phase' event swaps to carries the showtell composition.
	body := readBody(t, get(t, srv, "/user/phase-content/me"))
	if !strings.Contains(body, `id="showtell"`) {
		t.Errorf("after the phase event the region should carry the show & tell section, got %q", body)
	}
}

// TestMatchPage_PhaseEventSwapsTheRegion covers the match page's inline
// script: the 'phase' event refreshes the phase header and swaps the region,
// while the 'refresh' event keeps updating the individual cards only — a
// refresh never re-animates the region.
func TestMatchPage_PhaseEventSwapsTheRegion(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "me", "The Me", "ready")
	seed(t, deps.db, "mate", "The Mate", "ready")
	pair(t, deps.db, "me", "mate")
	deps.events.Reveal()

	body := readBody(t, get(t, srv, "/user/match/me"))

	// The region listens for the 'phase' event only — a 'refresh' never
	// re-fetches it.
	if !strings.Contains(body, `hx-trigger="phase"`) {
		t.Error("the region should listen for the 'phase' event only")
	}

	// The 'phase' event refreshes the header and swaps the region.
	phase := body[strings.Index(body, `_mes.addEventListener('phase'`):]
	for _, want := range []string{
		`htmx.trigger('#phase-header', 'refresh')`,
		`htmx.trigger('#phase-content', 'phase')`,
	} {
		if !strings.Contains(phase, want) {
			t.Errorf("the 'phase' listener should contain %q", want)
		}
	}

	// The 'refresh' listener stays per-card: it must not reference the region.
	refresh := body[strings.Index(body, `_mes.addEventListener('refresh'`):strings.Index(body, `_mes.addEventListener('phase'`)]
	if strings.Contains(refresh, "#phase-content") {
		t.Errorf("the 'refresh' listener must not trigger the region, got %q", refresh)
	}
	// The header is on the page in every phase and refreshes unguarded.
	if !strings.Contains(refresh, `htmx.trigger('#phase-header', 'refresh')`) {
		t.Error("the 'refresh' listener should keep refreshing the phase header")
	}
	// The region composes per phase, so the card anchors are guarded: a card
	// absent from the current phase's composition is skipped, not an error.
	if !strings.Contains(refresh, "if (el) htmx.trigger(el, 'refresh')") {
		t.Errorf("the 'refresh' listener should guard the per-card refreshes, got %q", refresh)
	}
	for _, card := range []string{"#meet-status", "#dossier", "#mission-card", "#showtell", "#recap"} {
		if !strings.Contains(refresh, `refreshCard('`+card+`')`) {
			t.Errorf("the 'refresh' listener should keep refreshing %s", card)
		}
	}
}
