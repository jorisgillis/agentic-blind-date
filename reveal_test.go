package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestReveal_MatchedParticipantWaitsForTheReveal(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	pair(t, deps.db, "a", "b")

	resp := get(t, srv, "/user/wait/a")
	if resp.StatusCode != 200 {
		t.Fatalf("wait page before the Reveal: want 200, got %d (%s)", resp.StatusCode, resp.Header.Get("Location"))
	}
	if body := readBody(t, resp); !strings.Contains(body, "reveal at the break") {
		t.Errorf("wait page should tease the Reveal")
	}
	if loc := get(t, srv, "/user/match/a").Header.Get("Location"); loc != "/user/wait/a" {
		t.Errorf("match page before the Reveal: want redirect to wait, got %q", loc)
	}
	status := get(t, srv, "/user/wait-status/a")
	if loc := status.Header.Get("HX-Redirect"); loc != "" {
		t.Errorf("wait status before the Reveal: want no redirect, got %q", loc)
	}
	if body := readBody(t, status); !strings.Contains(body, `hx-get="/user/wait-status/a"`) {
		t.Errorf("wait status must keep polling so the Reveal is noticed")
	}
	for _, path := range []string{"/user/onboard/a", "/user/pipeline/a"} {
		if loc := get(t, srv, path).Header.Get("Location") + get(t, srv, path).Header.Get("HX-Redirect"); strings.Contains(loc, "/user/match/") {
			t.Errorf("%s before the Reveal: must not send to the match page, got %q", path, loc)
		}
	}
}

func TestReveal_OpensEveryMatchPage(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	pair(t, deps.db, "a", "b")

	if loc := post(t, srv, "/admin/reveal", nil).Header.Get("Location"); loc != "/admin" {
		t.Errorf("reveal: want redirect to admin, got %q", loc)
	}

	if phase := deps.events.State(); phase != PhaseSocial {
		t.Errorf("Phase after the Reveal: want social, got %q", phase)
	}
	if loc := get(t, srv, "/user/wait/a").Header.Get("Location"); loc != "/user/match/a" {
		t.Errorf("wait page after the Reveal: want match page, got %q", loc)
	}
	if loc := get(t, srv, "/user/wait-status/b").Header.Get("HX-Redirect"); loc != "/user/match/b" {
		t.Errorf("wait status after the Reveal: want HX-Redirect to match, got %q", loc)
	}
	if resp := get(t, srv, "/user/match/a"); resp.StatusCode != 200 {
		t.Errorf("match page after the Reveal: want 200, got %d", resp.StatusCode)
	}
}

func TestReveal_NewcomerAfterTheRevealIsNeverToldTheyMissedIt(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "late", "The Latecomer", "ready")
	must(t, deps.events.Reveal())

	body := readBody(t, get(t, srv, "/user/wait-status/late"))

	if strings.Contains(body, "missed it") {
		t.Errorf("a ready Participant after the Reveal must not be told they missed it")
	}
}

// TestRematch_KeepsThePhase pins that Rematching never changes the Phase:
// during onboarding it runs, and from the social onward matching is frozen, so
// the endpoint rejects it (#115). Either way the Phase stays put.
func TestRematch_KeepsThePhase(t *testing.T) {
	for _, phase := range phases {
		t.Run(string(phase), func(t *testing.T) {
			llm := newFakeLLM().on("matchmaker", `{"score": 70, "reason": "fine"}`)
			srv, deps := newTestServer(t, llm, nil)
			seed(t, deps.db, "a", "A", "ready")
			seed(t, deps.db, "b", "B", "ready")
			if err := deps.events.Advance(phase); err != nil {
				t.Fatal(err)
			}

			resp := post(t, srv, "/admin/rematch", nil)
			if phase.MatchingFrozen() {
				// The rejection navigates back to the console with a notice (#127).
				if resp.StatusCode != http.StatusSeeOther {
					t.Errorf("from the social onward the endpoint rejects the rematch: want 303, got %d", resp.StatusCode)
				}
			} else {
				if resp.StatusCode != http.StatusSeeOther {
					t.Errorf("during onboarding: want 303, got %d", resp.StatusCode)
				}
				eventually(t, "both matched", func() bool { return reload(t, deps.db, "a").MatchedWith == "b" })
			}

			if got := deps.events.State(); got != phase {
				t.Errorf("Phase after rematch: want %q, got %q", phase, got)
			}
		})
	}
}

// TestBigScreen_ShowsPersonasOnlyInEveryPhase locks in #113's privacy
// baseline: the Big Screen shows persona names only, in every phase — the
// graph payload never carries GitHub handles or real names.
func TestBigScreen_ShowsPersonasOnlyInEveryPhase(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	must(t, deps.db.CreateParticipant("g1", "octocat", "Octo Cat", true))
	setPersona(t, deps.db, "g1", "The Gopher", "")
	forceStep(deps.db, "g1", StepReady)
	must(t, deps.db.CreateParticipant("ada", "no-github-1234abcd", "Ada Lovelace", false))
	setPersona(t, deps.db, "ada", "The Analyst", "")
	forceStep(deps.db, "ada", StepReady)

	identities := []string{"octocat", "Octo Cat", "Ada Lovelace", "no-github-1234abcd"}
	personas := []string{"The Gopher", "The Analyst"}

	stream := open(t, srv, "/bigscreen/stream")
	waitFor(t, stream, "The Gopher")

	for _, phase := range phases {
		t.Run(string(phase), func(t *testing.T) {
			if err := deps.events.Advance(phase); err != nil {
				t.Fatal(err)
			}

			body := readBody(t, get(t, srv, "/bigscreen/graph-data"))
			for _, identity := range identities {
				if strings.Contains(body, identity) {
					t.Errorf("the graph payload leaks the identity %q in the %s phase", identity, phase)
				}
			}
			for _, persona := range personas {
				if !strings.Contains(body, persona) {
					t.Errorf("the graph payload should show the persona %q in the %s phase", persona, phase)
				}
			}
			if !strings.Contains(body, `"phase": "`+string(phase)+`"`) {
				t.Errorf("the graph payload should report the %s phase", phase)
			}
		})
	}

	// Every payload the stream ever pushed is personas-only too.
	eventually(t, "the stream to push the closed phase", func() bool {
		return strings.Contains(stream.String(), `"phase":"closed"`)
	})
	for _, identity := range identities {
		if strings.Contains(stream.String(), identity) {
			t.Errorf("the Big Screen stream leaks the identity %q", identity)
		}
	}
	for _, persona := range personas {
		if !strings.Contains(stream.String(), persona) {
			t.Errorf("the Big Screen stream should show the persona %q", persona)
		}
	}
}
