package main

import (
	"strings"
	"testing"
)

func TestHostConsoleState_RendersTheFragment(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	if err := deps.db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}

	body := readBody(t, get(t, srv, "/admin/host/state"))

	if !strings.Contains(body, "Lobby") || !strings.Contains(body, "Start the show") {
		t.Errorf("want the Lobby state fragment, got:\n%s", body)
	}
}

func TestShowStatus_UnknownParticipantIs404(t *testing.T) {
	srv, _ := newTestServer(t, nil, nil)

	if resp := get(t, srv, "/user/show/nobody"); resp.StatusCode != 404 {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

func TestShowStatusStream_UnknownParticipantIs404(t *testing.T) {
	srv, _ := newTestServer(t, nil, nil)

	if resp := get(t, srv, "/user/show-stream/nobody"); resp.StatusCode != 404 {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

func TestShowStatusStream_RedirectsImmediatelyWhenAlreadyElsewhere(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready") // Classic flow: belongs on the wait page, not the show status page

	s := open(t, srv, "/user/show-stream/a")

	waitFor(t, s, "data: /user/wait/a")
}

func TestHostConsole_ShowsTheSegmentAndTheNextAction(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	if err := deps.db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}

	body := readBody(t, get(t, srv, "/admin/host"))

	if !strings.Contains(body, "Lobby") {
		t.Errorf("want the Lobby shown, got:\n%s", body)
	}
	if !strings.Contains(body, "Start the show") {
		t.Errorf("want the next action \"Start the show\", got:\n%s", body)
	}
}

func TestHostConsole_InactiveInTheClassicFlow(t *testing.T) {
	srv, _ := newTestServer(t, nil, nil)

	body := readBody(t, get(t, srv, "/admin/host"))

	if !strings.Contains(body, "inactive") {
		t.Errorf("want the console to say the Show is inactive, got:\n%s", body)
	}
}

func TestAdvanceShow_MovesToTheNextSegment(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	if err := deps.db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}

	if loc := post(t, srv, "/admin/host/advance", nil).Header.Get("Location"); loc != "/admin/host" {
		t.Errorf("want redirect to the host console, got %q", loc)
	}

	body := readBody(t, get(t, srv, "/admin/host"))
	if !strings.Contains(body, "Round 1 of 6") {
		t.Errorf("want Round 1 shown after advancing once, got:\n%s", body)
	}
}

func TestAdvanceShow_RejectedInTheClassicFlow(t *testing.T) {
	srv, _ := newTestServer(t, nil, nil)

	resp := post(t, srv, "/admin/host/advance", nil)

	if resp.StatusCode != 400 {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

func TestBigScreen_SnapshotCarriesTheShowSegmentInAGameShow(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	show := NewShow(db)
	if err := show.Advance(); err != nil { // -> Round 0
		t.Fatal(err)
	}
	bs := NewBigScreen(db, NewMatcher(db, newFakeGitHub(), newFakeLLM()))

	snap := bs.Snapshot()

	if snap.Format != GameShow {
		t.Errorf("want Format GameShow, got %q", snap.Format)
	}
	if snap.Segment != SegmentRound || snap.Round != 1 {
		t.Errorf("want Round 1, got segment %q round %d", snap.Segment, snap.Round)
	}
}

// TestShowStatus_ReflectsTheCurrentSegmentAndRedirectsOnceMatched covers
// #78: a ready Game Show Participant sees the show status page, which
// redirects to the match page once they're matched and revealed.
func TestShowStatus_ReflectsTheCurrentSegmentAndRedirectsOnceMatched(t *testing.T) {
	llm := newFakeLLM().
		on("interviewer", `{"questions": ["Why?", "How?", "When?", "Where?", "Who?"]}`).
		on("personality generator", `{"name": "The Gopher", "tagline": "Ships"}`)
	gh := newFakeGitHub().withProfile(&GitHubProfile{Login: "octo", Languages: []string{"Go"}})
	srv, deps := newTestServer(t, llm, gh)
	if err := deps.db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	id, err := deps.onboarding.Register("Octo", "octo", true)
	if err != nil {
		t.Fatal(err)
	}
	answerEverything(t, deps.onboarding, deps.db, id)
	eventually(t, "ready", func() bool { return reload(t, deps.db, id).PipelineStep == StepReady })

	if loc := get(t, srv, "/user/wait/"+id).Header.Get("Location"); loc != "/user/show/"+id {
		t.Errorf("a ready Game Show Participant belongs on the show status page, got redirect %q", loc)
	}

	body := readBody(t, get(t, srv, "/user/show/"+id))
	if !strings.Contains(body, "Waiting for the Host") {
		t.Errorf("want the Lobby status shown, got:\n%s", body)
	}

	seed(t, deps.db, "partner", "The Crab", "ready")
	pair(t, deps.db, id, "partner")
	deps.db.Reveal()

	if loc := get(t, srv, "/user/show/"+id).Header.Get("Location"); loc != "/user/match/"+id {
		t.Errorf("once matched and revealed, want redirect to the match page, got %q", loc)
	}
}

// TestHostConsoleStream_RefreshesWhenTheSegmentChanges and
// TestShowStatusStream_RefreshesWhenTheSegmentChanges cover #78: the host
// console and a Participant's show status page are both pushed the current
// segment through the change feed, not polled.
func TestHostConsoleStream_RefreshesWhenTheSegmentChanges(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	if err := deps.db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}

	s := open(t, srv, "/admin/host/stream")
	sentNothingNew(t, s, 1, "", "nothing changed")

	post(t, srv, "/admin/host/advance", nil)
	waitFor(t, s, "event: refresh")
}

func TestShowStatusStream_RefreshesWhenTheSegmentChanges(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	if err := deps.db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	seed(t, deps.db, "p", "The Gopher", "ready")

	s := open(t, srv, "/user/show-stream/p")
	sentNothingNew(t, s, 1, "", "nothing changed")

	post(t, srv, "/admin/host/advance", nil)
	waitFor(t, s, "event: refresh")
}
