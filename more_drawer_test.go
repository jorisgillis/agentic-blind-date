package main

import (
	"strings"
	"testing"
)

// drawerOf slices the page's "More" drawer (ADR 0019, step 3, #138) out of a
// rendered body: the details element and everything inside it.
func drawerOf(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, "<details")
	if start < 0 {
		t.Fatal("the page should carry a More drawer (a details element)")
	}
	end := strings.Index(body[start:], "</details>")
	if end < 0 {
		t.Fatal("the More drawer should close")
	}
	return body[start : start+end+len("</details>")]
}

// TestMatchPage_MoreDrawer covers #138 on the match page: in every phase the
// bottom "More" drawer renders after the phase-content region and holds the
// evergreen cards — the dossier (when matched), the settings, the share-profile
// QR and the recap link — while the region keeps none of them, so the drawer
// survives the region's phase swaps. Each fragment lives in exactly one place.
func TestMatchPage_MoreDrawer(t *testing.T) {
	for _, phase := range []Phase{PhaseSocial, PhaseShowTell, PhaseClosed} {
		t.Run(string(phase), func(t *testing.T) {
			srv, deps := newTestServer(t, nil, nil)
			seed(t, deps.db, "me", "The Me", "ready")
			seed(t, deps.db, "mate", "The Mate", "ready")
			pair(t, deps.db, "me", "mate")
			if err := deps.events.Advance(phase); err != nil {
				t.Fatal(err)
			}

			body := readBody(t, get(t, srv, "/user/match/me"))
			region := readBody(t, get(t, srv, "/user/phase-content/me"))
			drawer := drawerOf(t, body)

			// The drawer renders after the region, in every phase.
			assertOrder(t, body, `id="phase-content"`, "<details", "<summary", "More")

			// The drawer holds the evergreen cards, in order: the dossier,
			// the settings, the share-profile QR and the recap link.
			assertOrder(t, drawer,
				`id="dossier"`,
				`id="settings"`,
				`id="qr"`,
				`id="recap"`,
			)

			// The dossier keeps its live-unlock wiring inside the drawer: the
			// anchor the match stream's 'refresh' event swaps, so the pair's
			// Meeting completing unlocks it without a manual refresh — and the
			// unlock animates while the drawer is open.
			for _, want := range []string{
				`hx-get="/user/dossier/me"`,
				`hx-trigger="refresh"`,
				`hx-swap="outerHTML"`,
			} {
				if !strings.Contains(drawer, want) {
					t.Errorf("the drawer's dossier should carry %q, got:\n%s", want, drawer)
				}
			}

			// The drawer is a browser-native toggle: no JS, and the chevron's
			// transition respects prefers-reduced-motion (ADR-0009).
			for _, want := range []string{"motion-reduce:", "group-open:"} {
				if !strings.Contains(drawer, want) {
					t.Errorf("the drawer should carry %q, got:\n%s", want, drawer)
				}
			}

			// No evergreen card remains in the region: the dossier is removed
			// from the region step 2 leaves it in, and so is the recap link.
			assertRegionKeepsOutTheEvergreenCards(t, region)

			// Exactly one anchor per fragment id on the rendered page: the
			// phase cards in the region, the evergreen cards in the drawer.
			for _, id := range []string{"phase-header", "phase-content", "dossier", "settings", "qr", "recap"} {
				if n := strings.Count(body, `id="`+id+`"`); n != 1 {
					t.Errorf("want exactly one %q anchor on the match page, got %d", id, n)
				}
			}
			// The phase cards come and go with the phase; the drawer's cards
			// stay. The show & tell sign-up rides the region from the social
			// on — sign-up is open during the social and the show & tell.
			for id, present := range map[string]bool{
				"mission-card": true,
				"meet-status":  phase != PhaseClosed,
				"showtell":     phase != PhaseClosed,
			} {
				if n := strings.Count(body, `id="`+id+`"`); (n == 1) != present {
					t.Errorf("phase %s: the %q anchor should be present=%v, got %d", phase, id, present, n)
				}
			}
		})
	}
}

// TestMatchPage_MoreDrawerWithoutAMatch covers the no-match state: the drawer
// is still fine — the settings, the share-profile QR and the recap link render
// — only the dossier is absent, and the guarded refresh wiring tolerates that.
func TestMatchPage_MoreDrawerWithoutAMatch(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "me", "The Me", "ready")
	must(t, deps.events.Reveal())
	// Deliberately invalid state: a partner that does not exist — the same
	// impossible state TestPhaseContent_NoMatchStateRenders stages.
	mustExec(t, deps.db, `UPDATE participants SET matched_with = 'gone' WHERE id = 'me'`)

	body := readBody(t, get(t, srv, "/user/match/me"))
	drawer := drawerOf(t, body)

	if !strings.Contains(body, "You don't have a match (yet)") {
		t.Errorf("want the no-match state, got %q", body)
	}
	// No dossier anchor without a match.
	if strings.Contains(body, `id="dossier"`) {
		t.Error("no dossier without a match")
	}
	// The drawer still holds the other evergreen cards.
	assertOrder(t, drawer, `id="settings"`, `id="qr"`, `id="recap"`)
	for _, id := range []string{"settings", "qr", "recap"} {
		if n := strings.Count(body, `id="`+id+`"`); n != 1 {
			t.Errorf("want exactly one %q anchor on the match page, got %d", id, n)
		}
	}
}

// TestWaitPage_MoreDrawer covers #138 on the wait page: the body keeps its
// cards — the persona card, the slides, the wait status, the mission card,
// the meet section and the show & tell section — and the bottom "More" drawer
// holds the evergreen content: the Q&A recap, the GitHub summary, the settings
// and the recap link. The Q&A recap rides the drawer so the live event's
// cards keep the page's attention. Each fragment lives in exactly one place.
func TestWaitPage_MoreDrawer(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "r", "The Gopher", "ready")
	setProfile(t, deps.db, "r", &GitHubProfile{Login: "r", Languages: []string{"Go"}, AccountAgeDays: 730})

	body := readBody(t, get(t, srv, "/user/wait/r"))
	drawer := drawerOf(t, body)

	// The body keeps its cards, in order, before the drawer: the slides sit
	// high so the room finds them, and the Q&A recap is not among them.
	assertOrder(t, body,
		`id="phase-header"`,
		`id="persona-card"`,
		`id="slides"`,
		`id="wait-status"`,
		`id="mission-card"`,
		`id="meet-status"`,
		`id="showtell"`,
		"<details",
	)
	if strings.Index(body, "Your answers") < strings.Index(body, "<details") {
		t.Error("the Q&A recap should live in the More drawer, not the body")
	}

	// The drawer holds the evergreen content, in order: the Q&A recap, the
	// GitHub summary, the settings and the recap link.
	assertOrder(t, drawer,
		"Your answers",
		"Your GitHub",
		`id="settings"`,
		`id="recap"`,
	)

	// Exactly one anchor per fragment id, and the GitHub summary lives in
	// exactly one place: the drawer.
	for _, id := range []string{"phase-header", "persona-card", "slides", "wait-status", "mission-card", "meet-status", "showtell", "settings", "recap"} {
		if n := strings.Count(body, `id="`+id+`"`); n != 1 {
			t.Errorf("want exactly one %q anchor on the wait page, got %d", id, n)
		}
	}
	if n := strings.Count(body, "Your GitHub"); n != 1 {
		t.Errorf("want the GitHub summary exactly once (in the drawer), got %d", n)
	}

	// The drawer's recap link keeps its refresh wiring, and the wait stream's
	// unguarded triggers keep finding their anchors — the drawer's included.
	for _, want := range []string{
		`hx-get="/user/recap-link/r"`,
		`hx-trigger="refresh"`,
		`htmx.trigger('#recap', 'refresh')`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the wait page should contain %q", want)
		}
	}
}
