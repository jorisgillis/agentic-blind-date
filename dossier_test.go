package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestTeaser_AMatchedParticipantSeesOnlyEmojiColourAndHint covers the
// curiosity gap on the wait page during onboarding: the teaser shows the
// future Match's emoji and colour and a hint at what the pair disagreed on —
// never the partner's identity, score or dossier.
func TestTeaser_AMatchedParticipantSeesOnlyEmojiColourAndHint(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "aaa", "The Gopher", "ready")
	seed(t, deps.db, "bbb", "The Crab", "ready")
	updateAnswers(t, deps.db, "aaa", map[string]string{"fixed_0": "Spaces"}) // the pair disagrees
	pair(t, deps.db, "aaa", "bbb")
	partner := reload(t, deps.db, "bbb")

	for _, path := range []string{"/user/wait/aaa", "/user/wait-status/aaa"} {
		body := readBody(t, get(t, srv, path))
		for _, want := range []string{partner.PersonaSymbol, partner.PersonaColor, "You disagreed on", "reveal at the break"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: teaser should contain %q", path, want)
			}
		}
		for _, leak := range []string{"The Crab", "@bbb", "91%", "Both love tabs", "hogs the whiteboard", "Why tabs?"} {
			if strings.Contains(body, leak) {
				t.Errorf("%s: teaser leaked %q", path, leak)
			}
		}
	}
}

// TestTeaser_WhenThePairThinksAlike covers the teaser's fallback hint when
// the pair has no disagreement to tease.
func TestTeaser_WhenThePairThinksAlike(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "aaa", "The Gopher", "ready")
	seed(t, deps.db, "bbb", "The Crab", "ready")
	pair(t, deps.db, "aaa", "bbb") // both answered "Tabs" alike

	body := readBody(t, get(t, srv, "/user/wait-status/aaa"))

	if !strings.Contains(body, "think alike") || !strings.Contains(body, "reveal at the break") {
		t.Errorf("want the think-alike hint, got %q", body)
	}
}

// TestWaitStatus_NoMatchStateOnceTheSocialHasBegun covers the acceptance
// rule: an unmatched Participant sees a clear no-match state once the Social
// has begun, instead of the ready count.
func TestWaitStatus_NoMatchStateOnceTheSocialHasBegun(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "solo", "The Loner", "ready")
	deps.events.Reveal()

	for _, path := range []string{"/user/wait/solo", "/user/wait-status/solo"} {
		body := readBody(t, get(t, srv, path))
		if !strings.Contains(body, "No match") {
			t.Errorf("%s: want the no-match state, got %q", path, body)
		}
		if strings.Contains(body, "ready in the room") {
			t.Errorf("%s: the ready count is replaced by the no-match state once the Social has begun", path)
		}
	}
}

// TestWaitStatus_WithAPartnerWhoIsGoneStillRenders covers the impossible
// state of a dangling matched_with on the wait page: the teaser simply does
// not render.
func TestWaitStatus_WithAPartnerWhoIsGoneStillRenders(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "aaa", "The Gopher", "ready")
	// Deliberately invalid state: a partner that does not exist. No store
	// operation produces it — pairing only ever names an existing Participant
	// and matched_with's single writer is the Relationships module — so the
	// impossible state is written directly.
	deps.db.db.Exec(`UPDATE participants SET matched_with = 'gone' WHERE id = 'aaa'`)

	resp := get(t, srv, "/user/wait-status/aaa")
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	if body := readBody(t, resp); strings.Contains(body, "You've been matched!") {
		t.Errorf("a partner who is gone is no teaser, got %q", body)
	}
}

// TestDossier_StaysLockedUntilTheMeetingCompletes covers the dossier's lock:
// before the pair's Meeting completes — not even with a one-sided
// confirmation — the payload is in no page and no fragment.
func TestDossier_StaysLockedUntilTheMeetingCompletes(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	deps.events.Reveal()
	seed(t, deps.db, "aaa", "The Gopher", "ready")
	seed(t, deps.db, "bbb", "The Crab", "ready")
	pair(t, deps.db, "aaa", "bbb")
	partner := reload(t, deps.db, "bbb")
	ms := NewMeetings(deps.db, deps.events)

	for _, path := range []string{"/user/match/aaa", "/user/dossier/aaa"} {
		body := readBody(t, get(t, srv, path))
		if !strings.Contains(body, "🔒") || !strings.Contains(body, partner.PersonaSymbol) {
			t.Errorf("%s: want the locked card naming the partner's emoji, got %q", path, body)
		}
		for _, absent := range []string{"Both love tabs", "hogs the whiteboard", "Why tabs?"} {
			if strings.Contains(body, absent) {
				t.Errorf("%s must not contain %q while the dossier is locked", path, absent)
			}
		}
	}

	// A one-sided confirmation is a pending Meeting, not a completed one.
	if _, _, err := ms.Confirm(reload(t, deps.db, "aaa"), reload(t, deps.db, "bbb"), OriginMatch); err != nil {
		t.Fatal(err)
	}
	if body := readBody(t, get(t, srv, "/user/dossier/aaa")); !strings.Contains(body, "🔒") {
		t.Errorf("a pending Meeting must not unlock the dossier, got %q", body)
	}
}

// TestDossier_UnlocksWhenThePairCompletesTheirMeeting covers the unlock: once
// both sides confirm, the dossier shows the payload on both phones.
func TestDossier_UnlocksWhenThePairCompletesTheirMeeting(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	deps.events.Reveal()
	seed(t, deps.db, "aaa", "The Gopher", "ready")
	seed(t, deps.db, "bbb", "The Crab", "ready")
	pair(t, deps.db, "aaa", "bbb")
	ms := NewMeetings(deps.db, deps.events)
	if _, _, err := ms.Confirm(reload(t, deps.db, "aaa"), reload(t, deps.db, "bbb"), OriginMatch); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ms.Confirm(reload(t, deps.db, "bbb"), reload(t, deps.db, "aaa"), OriginMatch); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/user/match/aaa", "/user/dossier/aaa", "/user/match/bbb"} {
		body := readBody(t, get(t, srv, path))
		for _, want := range []string{"Both love tabs", "hogs the whiteboard", "Why tabs?"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: the unlocked dossier should contain %q", path, want)
			}
		}
		if strings.Contains(body, "🔒") {
			t.Errorf("%s: the unlocked dossier must not show the lock", path)
		}
	}
}

// TestDossier_UnlocksWhenTheEventCloses covers the unlock's second half: after
// closed the dossier is visible again even without a completed Meeting — the
// evening is over and the recap takes over.
func TestDossier_UnlocksWhenTheEventCloses(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	deps.events.Reveal()
	seed(t, deps.db, "aaa", "The Gopher", "ready")
	seed(t, deps.db, "bbb", "The Crab", "ready")
	pair(t, deps.db, "aaa", "bbb")
	if err := deps.events.Advance(PhaseClosed); err != nil {
		t.Fatal(err)
	}

	body := readBody(t, get(t, srv, "/user/match/aaa"))
	if !strings.Contains(body, "Both love tabs") {
		t.Errorf("after closed the dossier is visible again, got %q", body)
	}
}

// TestDossier_WithoutAMatchThereIsNoDossier covers the fragment handler's
// edges: an unknown Participant and an unmatched Participant are 404.
func TestDossier_WithoutAMatchThereIsNoDossier(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	deps.events.Reveal()
	seed(t, deps.db, "solo", "The Loner", "ready")

	if resp := get(t, srv, "/user/dossier/nobody"); resp.StatusCode != 404 {
		t.Errorf("unknown: want 404, got %d", resp.StatusCode)
	}
	if resp := get(t, srv, "/user/dossier/solo"); resp.StatusCode != 404 {
		t.Errorf("unmatched: want 404, got %d", resp.StatusCode)
	}
}

// TestMatchStream_PushesTheDossierUnlock covers the no-manual-refresh rule:
// the match page's stream fires when the pair's Meeting completes, and the
// dossier it then fetches is unlocked.
func TestMatchStream_PushesTheDossierUnlock(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	deps.events.Reveal()
	seed(t, deps.db, "aaa", "The Gopher", "ready")
	seed(t, deps.db, "bbb", "The Crab", "ready")
	pair(t, deps.db, "aaa", "bbb")
	ms := NewMeetings(deps.db, deps.events)

	s := open(t, srv, "/user/match-stream/aaa")
	waitFor(t, s, "event: refresh") // the sync on connect
	before := s.String()
	sentNothingNew(t, s, 1, before, "before the pair meets")

	if _, _, err := ms.Confirm(reload(t, deps.db, "aaa"), reload(t, deps.db, "bbb"), OriginMatch); err != nil {
		t.Fatal(err)
	}
	waitForCount(t, s, "event: refresh", strings.Count(before, "event: refresh")+1) // the first side: the meet section changes
	before = s.String()
	if _, _, err := ms.Confirm(reload(t, deps.db, "bbb"), reload(t, deps.db, "aaa"), OriginMatch); err != nil {
		t.Fatal(err)
	}
	waitForCount(t, s, "event: refresh", strings.Count(before, "event: refresh")+1)

	// The refresh the stream pushed re-renders the dossier — unlocked.
	if body := readBody(t, get(t, srv, "/user/dossier/aaa")); !strings.Contains(body, "Both love tabs") {
		t.Errorf("after the push the dossier is unlocked, got %q", body)
	}
}

// TestDataEndpoints_ScrubTheAssessmentUntilTheMeetingCompletes covers the
// JSON rule: flags, icebreakers and the reason are absent from every JSON
// endpoint a participant view could fetch until the pair's Meeting completes.
func TestDataEndpoints_ScrubTheAssessmentUntilTheMeetingCompletes(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	deps.events.Reveal()
	seed(t, deps.db, "aaa", "The Gopher", "ready")
	seed(t, deps.db, "bbb", "The Crab", "ready")
	seed(t, deps.db, "solo", "The Loner", "ready")
	pair(t, deps.db, "aaa", "bbb")

	// Before the pair meets: scrubbed everywhere.
	for _, path := range []string{"/data/participants", "/data/participant/aaa", "/data/participant/bbb"} {
		body := readBody(t, get(t, srv, path))
		for _, leak := range []string{"Both love tabs", "hogs the whiteboard", "Why tabs?", `"red_flags"`, `"green_flags"`, `"icebreakers"`} {
			if strings.Contains(body, leak) {
				t.Errorf("%s leaks the locked assessment (%q)", path, leak)
			}
		}
	}
	var list []Participant
	if err := json.Unmarshal([]byte(readBody(t, get(t, srv, "/data/participants"))), &list); err != nil {
		t.Fatal(err)
	}
	for _, p := range list {
		if p.Assessment != nil {
			t.Errorf("%s: the assessment is scrubbed until the pair's Meeting completes, got %+v", p.ID, p.Assessment)
		}
	}

	// After the pair completes their Meeting: visible again.
	ms := NewMeetings(deps.db, deps.events)
	if _, _, err := ms.Confirm(reload(t, deps.db, "aaa"), reload(t, deps.db, "bbb"), OriginMatch); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ms.Confirm(reload(t, deps.db, "bbb"), reload(t, deps.db, "aaa"), OriginMatch); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/data/participants", "/data/participant/aaa"} {
		body := readBody(t, get(t, srv, path))
		for _, want := range []string{"Both love tabs", "hogs the whiteboard", "Why tabs?"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s should show the assessment after the Meeting completed", path)
			}
		}
	}
}

// TestDataEndpoints_ShowTheAssessmentAfterClosed covers the scrub rule's
// second half: after closed the assessment is visible again even without a
// completed Meeting.
func TestDataEndpoints_ShowTheAssessmentAfterClosed(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	deps.events.Reveal()
	seed(t, deps.db, "aaa", "The Gopher", "ready")
	seed(t, deps.db, "bbb", "The Crab", "ready")
	pair(t, deps.db, "aaa", "bbb")
	if err := deps.events.Advance(PhaseClosed); err != nil {
		t.Fatal(err)
	}

	body := readBody(t, get(t, srv, "/data/participant/aaa"))
	if !strings.Contains(body, "Both love tabs") {
		t.Errorf("after closed the assessment is visible again, got %q", body)
	}
}

// TestExplore_IsGone covers the removal: the routes return 404 and no
// template links to them.
func TestExplore_IsGone(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	deps.events.Reveal()
	seed(t, deps.db, "aaa", "The Gopher", "ready")
	seed(t, deps.db, "bbb", "The Crab", "ready")
	pair(t, deps.db, "aaa", "bbb")

	for _, path := range []string{"/user/explore/aaa/bbb", "/user/explore/bbb/aaa", "/user/explore/aaa/aaa", "/user/explore/nobody/bbb"} {
		if resp := get(t, srv, path); resp.StatusCode != 404 {
			t.Errorf("%s: want 404, got %d", path, resp.StatusCode)
		}
	}
	body := readBody(t, get(t, srv, "/user/match/aaa"))
	for _, gone := range []string{"/user/explore", "Others at the event"} {
		if strings.Contains(body, gone) {
			t.Errorf("the match page must not link to Explore (%q)", gone)
		}
	}
}
