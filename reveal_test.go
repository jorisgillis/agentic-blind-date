package main

import (
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
	if body := readBody(t, resp); !strings.Contains(body, "waiting for the Reveal") {
		t.Errorf("wait page should say the Match is waiting for the Reveal")
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

	if phase, _ := deps.db.GetPhase(); phase != "revealed" {
		t.Errorf("Event State after the Reveal: want revealed, got %q", phase)
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
	deps.db.SetPhase("revealed")

	body := readBody(t, get(t, srv, "/user/wait-status/late"))

	if strings.Contains(body, "missed it") {
		t.Errorf("a ready Participant after the Reveal must not be told they missed it")
	}
}

func TestRematch_KeepsTheEventState(t *testing.T) {
	for _, phase := range []string{"onboarding", "revealed"} {
		t.Run(phase, func(t *testing.T) {
			llm := newFakeLLM().on("matchmaker", `{"score": 70, "reason": "fine"}`)
			srv, deps := newTestServer(t, llm, nil)
			seed(t, deps.db, "a", "A", "ready")
			seed(t, deps.db, "b", "B", "ready")
			deps.db.SetPhase(phase)

			post(t, srv, "/admin/rematch", nil)
			eventually(t, "both matched", func() bool { return reload(t, deps.db, "a").MatchedWith == "b" })

			if got, _ := deps.db.GetPhase(); got != phase {
				t.Errorf("Event State after rematch: want %q, got %q", phase, got)
			}
		})
	}
}

func TestReveal_TheBigScreenSendsNoIdentitiesBeforeTheReveal(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	deps.db.CreateParticipant("g1", "octocat", "Octo Cat", true)
	deps.db.SetPersona("g1", "The Gopher", "")
	forceStep(deps.db, "g1", StepReady)
	deps.db.CreateParticipant("ada", "no-github-1234abcd", "Ada Lovelace", false)
	deps.db.SetPersona("ada", "The Analyst", "")
	forceStep(deps.db, "ada", StepReady)

	for _, path := range []string{"/bigscreen/graph-data", "/bigscreen/state"} {
		body := readBody(t, get(t, srv, path))
		if strings.Contains(body, "octocat") || strings.Contains(body, "Ada Lovelace") {
			t.Errorf("%s reveals identities before the Reveal", path)
		}
	}
	if body := readBody(t, open(t, srv, "/bigscreen/stream").waitForAny(t)); strings.Contains(body, "octocat") {
		t.Errorf("the Big Screen stream reveals identities before the Reveal")
	}

	post(t, srv, "/admin/reveal", nil)
	if body := readBody(t, get(t, srv, "/bigscreen/graph-data")); !strings.Contains(body, "@octocat") || !strings.Contains(body, "Ada Lovelace") {
		t.Errorf("after the Reveal the Big Screen identifies everyone")
	}
}
