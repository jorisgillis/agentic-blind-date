package main

import (
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

// TestPhase_AdvanceOnlyMovesForward covers #113's phase machine rules: phases
// only move forward, the host may jump ahead, advancing to the current phase
// is a no-op, and backward transitions and unknown phases are rejected.
func TestPhase_AdvanceOnlyMovesForward(t *testing.T) {
	for _, from := range phases {
		for _, to := range append(append([]Phase{}, phases...), Phase("banquet"), Phase("")) {
			t.Run(fmt.Sprintf("%s→%s", from, to), func(t *testing.T) {
				db := newTestDB(t)
				events := NewEventStore(db)
				if err := events.Advance(from); err != nil {
					t.Fatal(err)
				}

				err := events.Advance(to)

				rejected := !to.Valid() || to.rank() < from.rank()
				if rejected && !errors.Is(err, ErrIllegalPhaseTransition) {
					t.Errorf("a rejected transition must report ErrIllegalPhaseTransition, got %v", err)
				}
				if !rejected && err != nil {
					t.Errorf("an allowed transition must succeed, got %v", err)
				}
				want := from
				if to.Valid() && to.rank() > from.rank() {
					want = to
				}
				if got := events.State(); got != want {
					t.Errorf("state: want %s, got %s", want, got)
				}
			})
		}
	}
}

// TestPhase_EveryPhaseHasItsOwnWords pins the vocabulary each phase shows on
// the Big Screen, in the activity ticker and on the host console.
func TestPhase_EveryPhaseHasItsOwnWords(t *testing.T) {
	for _, tc := range []struct {
		phase          Phase
		label          string
		activity       string
		advanceLabel   string
		advanceConfirm string
		buttonClass    string
	}{
		{PhaseOnboarding, "Waiting for participants", "", "", "", "bg-gray-700 hover:bg-gray-600"},
		{PhaseSocial, "Matches revealed! 🎉", "🎉 The matches are revealed!", "🎉 Start the social — reveal every match", "Advance to the social? Every match becomes visible to everyone.", "bg-pink-600 hover:bg-pink-500"},
		{PhaseShowTell, "Show & tell time! 🎤", "🎤 Show & tell time!", "🎤 Start show & tell", "Advance to show & tell? Sign-up and the question queue go live.", "bg-purple-600 hover:bg-purple-500"},
		{PhaseClosed, "The event is closed. See you next time! 👋", "👋 The event is closed. Thanks for playing!", "🔒 Close the event", "Close the event? The evening ends — recap pages persist.", "bg-gray-700 hover:bg-gray-600"},
	} {
		t.Run(string(tc.phase), func(t *testing.T) {
			if got := tc.phase.Label(); got != tc.label {
				t.Errorf("Label: want %q, got %q", tc.label, got)
			}
			if got := tc.phase.Activity(); got != tc.activity {
				t.Errorf("Activity: want %q, got %q", tc.activity, got)
			}
			if got := tc.phase.AdvanceLabel(); got != tc.advanceLabel {
				t.Errorf("AdvanceLabel: want %q, got %q", tc.advanceLabel, got)
			}
			if got := tc.phase.AdvanceConfirm(); got != tc.advanceConfirm {
				t.Errorf("AdvanceConfirm: want %q, got %q", tc.advanceConfirm, got)
			}
			if got := tc.phase.ButtonClass(); got != tc.buttonClass {
				t.Errorf("ButtonClass: want %q, got %q", tc.buttonClass, got)
			}
		})
	}
}

func TestPhase_AtLeast(t *testing.T) {
	for _, tc := range []struct {
		phase, other Phase
		want         bool
	}{
		{PhaseOnboarding, PhaseOnboarding, true},
		{PhaseOnboarding, PhaseSocial, false},
		{PhaseSocial, PhaseOnboarding, true},
		{PhaseSocial, PhaseSocial, true},
		{PhaseShowTell, PhaseSocial, true},
		{PhaseClosed, PhaseShowTell, true},
		{Phase("banquet"), PhaseOnboarding, false}, // an unknown phase never counts as reached
	} {
		if got := tc.phase.AtLeast(tc.other); got != tc.want {
			t.Errorf("%s.AtLeast(%s): want %v, got %v", tc.phase, tc.other, tc.want, got)
		}
	}
}

func TestPhase_NextListsTheReachableForwardPhases(t *testing.T) {
	for _, tc := range []struct {
		phase Phase
		want  []Phase
	}{
		{PhaseOnboarding, []Phase{PhaseSocial, PhaseShowTell, PhaseClosed}},
		{PhaseSocial, []Phase{PhaseShowTell, PhaseClosed}},
		{PhaseShowTell, []Phase{PhaseClosed}},
		{PhaseClosed, nil},
		{Phase("banquet"), []Phase{PhaseOnboarding, PhaseSocial, PhaseShowTell, PhaseClosed}},
	} {
		if got := tc.phase.Next(); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s.Next(): want %v, got %v", tc.phase, tc.want, got)
		}
	}
}

func TestEventStore_StateCountsAnythingUnreadableAsOnboarding(t *testing.T) {
	t.Run("a broken database", func(t *testing.T) {
		db := newTestDB(t)
		events := NewEventStore(db)
		breakDB(db)

		if got := events.State(); got != PhaseOnboarding {
			t.Errorf("an unreadable phase counts as onboarding, got %q", got)
		}
	})
	t.Run("a value outside the phases", func(t *testing.T) {
		db := newTestDB(t)
		events := NewEventStore(db)
		// Deliberately invalid state: no store operation writes a value
		// outside the four phases — the startup migration rewrites the
		// legacy 'revealed' — so the impossible value is written directly.
		db.db.Exec(`INSERT OR REPLACE INTO event_state (key, value) VALUES ('phase', 'banquet')`)

		if got := events.State(); got != PhaseOnboarding {
			t.Errorf("an unknown phase value counts as onboarding, got %q", got)
		}
	})
}

func TestEventStore_AdvancingToTheCurrentPhaseWritesNothing(t *testing.T) {
	db := newTestDB(t)
	events := NewEventStore(db)
	failWrites(t, db) // every write fails

	if err := events.Advance(PhaseOnboarding); err != nil {
		t.Errorf("advancing to the current phase is a no-op, got %v", err)
	}
	if got := events.State(); got != PhaseOnboarding {
		t.Errorf("the phase should be unchanged, got %q", got)
	}
}

func TestEventStore_AdvanceReportsAWriteFailure(t *testing.T) {
	db := newTestDB(t)
	events := NewEventStore(db)
	failWrites(t, db)

	if err := events.Advance(PhaseSocial); err == nil {
		t.Error("want the write failure reported")
	}
	if got := events.State(); got != PhaseOnboarding {
		t.Errorf("a failed advance leaves the phase unchanged, got %q", got)
	}
}

func TestPhase_PostAdvancesForwardWithJumpsAndRejectsBackward(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	advance := func(to string) *http.Response {
		return post(t, srv, "/admin/phase", url.Values{"to": {to}})
	}

	if resp := advance("social"); resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin" {
		t.Errorf("advance to social: want 303 to /admin, got %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if got := deps.events.State(); got != PhaseSocial {
		t.Errorf("after advancing to social: got %q", got)
	}

	advance("closed") // a forward jump, skipping show & tell
	if got := deps.events.State(); got != PhaseClosed {
		t.Errorf("a forward jump social → closed should work, got %q", got)
	}

	for _, to := range []string{"social", "showtell", "onboarding"} {
		if resp := advance(to); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("backward transition to %s: want 400, got %d", to, resp.StatusCode)
		}
	}
	if got := deps.events.State(); got != PhaseClosed {
		t.Errorf("a rejected transition must not change the phase, got %q", got)
	}

	if resp := advance("banquet"); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("an unknown phase: want 400, got %d", resp.StatusCode)
	}
	if got := deps.events.State(); got != PhaseClosed {
		t.Errorf("an unknown phase must not change the phase, got %q", got)
	}
}

func TestPhase_EveryAdvanceIsLogged(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)

	for _, to := range []Phase{PhaseSocial, PhaseShowTell, PhaseClosed} {
		post(t, srv, "/admin/phase", url.Values{"to": {string(to)}})
	}

	msgs, err := deps.db.GetRecentActivity(10)
	if err != nil {
		t.Fatal(err)
	}
	logged := strings.Join(msgs, "\n")
	for _, phase := range []Phase{PhaseSocial, PhaseShowTell, PhaseClosed} {
		if !strings.Contains(logged, phase.Activity()) {
			t.Errorf("advancing to %s should log %q, got %v", phase, phase.Activity(), msgs)
		}
	}
}

func TestPhase_AdvancingToTheCurrentPhaseIsANoOp(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)

	if resp := post(t, srv, "/admin/phase", url.Values{"to": {"onboarding"}}); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("same-phase advance: want 303, got %d", resp.StatusCode)
	}
	if got := deps.events.State(); got != PhaseOnboarding {
		t.Errorf("a same-phase advance must not change the phase, got %q", got)
	}
	if msgs, _ := deps.db.GetRecentActivity(10); len(msgs) != 0 {
		t.Errorf("a no-op should log nothing, got %v", msgs)
	}

	post(t, srv, "/admin/phase", url.Values{"to": {"social"}})
	post(t, srv, "/admin/phase", url.Values{"to": {"social"}})
	if msgs, _ := deps.db.GetRecentActivity(10); len(msgs) != 1 {
		t.Errorf("advancing twice to social should log once, got %v", msgs)
	}
}

func TestPhase_TheRevealEndpointStillAdvancesToTheSocial(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)

	resp := post(t, srv, "/admin/reveal", nil)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin" {
		t.Errorf("reveal: want 303 to /admin, got %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if got := deps.events.State(); got != PhaseSocial {
		t.Errorf("the Reveal is an alias for advancing to the social phase, got %q", got)
	}
}

func TestPhase_AdvanceReportsADatabaseFailure(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	breakDB(deps.db)

	if resp := post(t, srv, "/admin/phase", url.Values{"to": {"social"}}); resp.StatusCode != 500 {
		t.Errorf("a database failure: want 500, got %d", resp.StatusCode)
	}
}

// TestPhase_TheHostConsoleShowsThePhaseCountsAndTransitionButtons covers
// #113's acceptance criterion for /admin: the current phase with its label,
// the per-phase participant counts, and one confirmed transition button per
// reachable forward phase.
func TestPhase_TheHostConsoleShowsThePhaseCountsAndTransitionButtons(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	pair(t, deps.db, "a", "b")

	for _, tc := range []struct {
		phase   Phase
		caption string
		next    []Phase
	}{
		{PhaseOnboarding, "Onboarding: register, match, wait", []Phase{PhaseSocial, PhaseShowTell, PhaseClosed}},
		{PhaseSocial, "Social: every match is revealed", []Phase{PhaseShowTell, PhaseClosed}},
		{PhaseShowTell, "Show & tell: everything in the social stays", []Phase{PhaseClosed}},
		{PhaseClosed, "Closed: final stats", nil},
	} {
		t.Run(string(tc.phase), func(t *testing.T) {
			if err := deps.events.Advance(tc.phase); err != nil {
				t.Fatal(err)
			}

			body := readBody(t, get(t, srv, "/admin"))

			// The template HTML-escapes what it renders.
			if !strings.Contains(body, html.EscapeString(string(tc.phase)+" · "+tc.phase.Label())) {
				t.Errorf("the console should show the phase and its label %q", tc.phase.Label())
			}
			if !strings.Contains(body, html.EscapeString(tc.caption)) {
				t.Errorf("the console should show the per-phase caption %q", tc.caption)
			}
			for _, row := range []string{"Registered", "Ready", "Matched"} {
				if !strings.Contains(body, row) {
					t.Errorf("the console should show the %s count", row)
				}
			}
			// Two Participants, both ready, both matched.
			if n := strings.Count(body, ">2</span>"); n != 3 {
				t.Errorf("the three counts should each be 2, got %d rows of 2", n)
			}

			for _, to := range tc.next {
				if !strings.Contains(body, `name="to" value="`+string(to)+`"`) {
					t.Errorf("the console in %s should offer advancing to %s", tc.phase, to)
				}
				if !strings.Contains(body, html.EscapeString(to.AdvanceLabel())) {
					t.Errorf("the %s button should carry its label", to)
				}
			}
			// One transition form per reachable phase, each with a browser
			// confirm (the confirm texts themselves are pinned in
			// TestPhase_EveryPhaseHasItsOwnWords).
			forms := strings.Split(body, `<form action="/admin/phase" method="POST">`)
			if len(forms)-1 != len(tc.next) {
				t.Errorf("want %d transition buttons, got %d", len(tc.next), len(forms)-1)
			}
			for _, form := range forms[1:] {
				if !strings.Contains(form, `onclick="return confirm(`) {
					t.Errorf("a transition button must ask for a browser confirm: %s", form)
				}
			}
			for _, to := range phases {
				if to.rank() <= tc.phase.rank() && strings.Contains(body, `value="`+string(to)+`"`) {
					t.Errorf("the console in %s should not offer advancing to %s", tc.phase, to)
				}
			}
		})
	}
}

// TestPhase_TheWaitStreamRedirectsToTheMatchPageWhenTheSocialBegins covers
// #113's navigation criterion: a matched Participant moves from the wait page
// to their match page when the social phase begins — the wait-page push
// stream redirects them.
func TestPhase_TheWaitStreamRedirectsToTheMatchPageWhenTheSocialBegins(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")
	seed(t, deps.db, "b", "B", "ready")
	pair(t, deps.db, "a", "b")

	s := open(t, srv, "/user/wait-stream/a")
	sentNothingNew(t, s, 1, "", "nothing changed")

	post(t, srv, "/admin/phase", url.Values{"to": {"social"}})
	waitFor(t, s, "event: redirect\ndata: /user/match/a")
}

func TestPhase_ResetReturnsTheEventToOnboarding(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")

	post(t, srv, "/admin/phase", url.Values{"to": {"closed"}})
	if got := deps.events.State(); got != PhaseClosed {
		t.Fatalf("setup: want closed, got %q", got)
	}

	post(t, srv, "/admin/reset", nil)
	if got := deps.events.State(); got != PhaseOnboarding {
		t.Errorf("Reset returns the event to onboarding, got %q", got)
	}
}
