package main

import (
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// This file covers ticket #117's personal recap: the recaps store (the
// unguessable, persistent token), the recap page (persona card, the match
// dossier always unlocked, connections with revealed channels, personal
// stats), the recap link as a QR on the personal page during closed, and the
// recap gating.

// recapToken reads the recap token out of the recap-link fragment, the way
// the personal page's QR carries it.
func recapToken(t *testing.T, srv *testSrv, id string) string {
	t.Helper()
	body := readBody(t, get(t, srv, "/user/recap-link/"+id))
	m := regexp.MustCompile(`/user/recap/([0-9a-f-]{36})`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no recap token in the recap link for %s: %s", id, body)
	}
	return m[1]
}

// recapSetup seeds a matched pair and a third Participant, opens the social
// and completes the meeting between aaa and ccc — aaa's Match (bbb) never
// completes theirs, so the recap is the one place aaa's dossier is unlocked.
func recapSetup(t *testing.T) (*testSrv, *testDeps) {
	t.Helper()
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "aaa", "The Gopher", "ready")
	seed(t, deps.db, "bbb", "The YAML Wrangler", "ready")
	seed(t, deps.db, "ccc", "The Kernel Panicker", "ready")
	setProfile(t, deps.db, "aaa", &GitHubProfile{Login: "octocat-aaa"})
	setProfile(t, deps.db, "bbb", &GitHubProfile{Login: "octocat-bbb"})
	setProfile(t, deps.db, "ccc", &GitHubProfile{Login: "octocat-ccc"})
	pair(t, deps.db, "aaa", "bbb")
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	ms := NewMeetings(deps.db, deps.events)
	x, y := reload(t, deps.db, "aaa"), reload(t, deps.db, "ccc")
	if _, _, err := ms.Confirm(x, y, OriginOther); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ms.Confirm(y, x, OriginOther); err != nil {
		t.Fatal(err)
	}
	return srv, deps
}

func TestRecaps_TokenForCreatesOneTokenAndPersists(t *testing.T) {
	path := t.TempDir() + "/recaps.db"
	db, err := NewDB(path)
	if err != nil {
		t.Fatal(err)
	}
	seed(t, db, "a", "The Gopher", "ready")
	recaps := NewRecaps(db)

	token, err := recaps.TokenFor("a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(token); err != nil {
		t.Errorf("the recap token is a UUID: %v", err)
	}
	again, err := recaps.TokenFor("a")
	if err != nil {
		t.Fatal(err)
	}
	if again != token {
		t.Error("one token per Participant: the second call returns the same token")
	}
	db.Close()

	// A restart: the same file, reopened — the token persists (SQLite).
	reopened, err := NewDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	after, err := NewRecaps(reopened).TokenFor("a")
	if err != nil {
		t.Fatal(err)
	}
	if after != token {
		t.Errorf("the token persists across a restart: want %q, got %q", token, after)
	}
}

// TestRecaps_TokenForReportsFailures covers the token creation's error paths.
func TestRecaps_TokenForReportsFailures(t *testing.T) {
	t.Run("a read failure", func(t *testing.T) {
		db := newTestDB(t)
		seed(t, db, "a", "The Gopher", "ready")
		recaps := NewRecaps(db)
		// A token to find: the fault's view fails while its row is read.
		if _, err := recaps.TokenFor("a"); err != nil {
			t.Fatal(err)
		}
		breakOnFault(t, db, "recap_token_read")
		if _, err := recaps.TokenFor("a"); err == nil {
			t.Error("want an error")
		}
	})
	t.Run("a write failure", func(t *testing.T) {
		db := newTestDB(t)
		seed(t, db, "a", "The Gopher", "ready")
		recaps := NewRecaps(db)
		failWrites(t, db, "recap_insert")
		if _, err := recaps.TokenFor("a"); err == nil {
			t.Error("want an error")
		}
	})
	t.Run("a broken database", func(t *testing.T) {
		db := newTestDB(t)
		breakDB(db)
		if _, err := NewRecaps(db).TokenFor("a"); err == nil {
			t.Error("want an error")
		}
	})
}

// TestRecaps_ByToken covers the recap gating: an unknown token resolves to
// nothing, a valid token to its Participant, and a token whose Participant
// left is dead.
func TestRecaps_ByToken(t *testing.T) {
	t.Run("an unknown token", func(t *testing.T) {
		db := newTestDB(t)
		seed(t, db, "a", "The Gopher", "ready")
		p, err := NewRecaps(db).ByToken("00000000-0000-0000-0000-000000000000")
		if err != nil || p != nil {
			t.Errorf("an unknown token resolves to nothing, got %v (err %v)", p, err)
		}
	})
	t.Run("resolves the participant", func(t *testing.T) {
		db := newTestDB(t)
		seed(t, db, "a", "The Gopher", "ready")
		recaps := NewRecaps(db)
		token, err := recaps.TokenFor("a")
		if err != nil {
			t.Fatal(err)
		}
		p, err := recaps.ByToken(token)
		if err != nil {
			t.Fatal(err)
		}
		if p == nil || p.ID != "a" {
			t.Errorf("the token resolves to its Participant, got %v", p)
		}
	})
	t.Run("a dead token", func(t *testing.T) {
		db := newTestDB(t)
		seed(t, db, "a", "The Gopher", "ready")
		recaps := NewRecaps(db)
		token, err := recaps.TokenFor("a")
		if err != nil {
			t.Fatal(err)
		}
		// Deliberately invalid state: the Participant row is deleted without
		// the cleanup the delete endpoint runs, orphaning the token.
		db.db.Exec(`DELETE FROM participants WHERE id = 'a'`)
		if p, err := recaps.ByToken(token); err != nil || p != nil {
			t.Errorf("a token whose Participant left is dead, got %v (err %v)", p, err)
		}
	})
	t.Run("a read failure", func(t *testing.T) {
		db := newTestDB(t)
		seed(t, db, "a", "The Gopher", "ready")
		recaps := NewRecaps(db)
		token, err := recaps.TokenFor("a")
		if err != nil {
			t.Fatal(err)
		}
		breakOnFault(t, db, "recap_read")
		if _, err := recaps.ByToken(token); err == nil {
			t.Error("want an error")
		}
	})
	t.Run("a participant read failure", func(t *testing.T) {
		db := newTestDB(t)
		seed(t, db, "a", "The Gopher", "ready")
		recaps := NewRecaps(db)
		token, err := recaps.TokenFor("a")
		if err != nil {
			t.Fatal(err)
		}
		breakOnFault(t, db, "participant_read")
		if _, err := recaps.ByToken(token); err == nil {
			t.Error("want an error")
		}
	})
	t.Run("a broken database", func(t *testing.T) {
		db := newTestDB(t)
		breakDB(db)
		if _, err := NewRecaps(db).ByToken("whatever"); err == nil {
			t.Error("want an error")
		}
	})
}

// TestRecaps_RemoveParticipant covers the cleanup when a Participant leaves.
func TestRecaps_RemoveParticipant(t *testing.T) {
	db := newTestDB(t)
	seed(t, db, "a", "The Gopher", "ready")
	recaps := NewRecaps(db)
	token, err := recaps.TokenFor("a")
	if err != nil {
		t.Fatal(err)
	}

	if err := recaps.RemoveParticipant("a"); err != nil {
		t.Fatal(err)
	}
	if p, err := recaps.ByToken(token); err != nil || p != nil {
		t.Errorf("the token is gone with the Participant, got %v (err %v)", p, err)
	}

	// A token to delete: the fault's trigger cannot fire on an empty table.
	if _, err := recaps.TokenFor("a"); err != nil {
		t.Fatal(err)
	}
	failWrites(t, db, "recap_delete")
	if err := recaps.RemoveParticipant("a"); err == nil {
		t.Error("want an error")
	}
}

// TestRecapPage_ShowsTheEvening covers the recap page's content: the persona
// card, the match dossier — always unlocked here, even though the pair never
// completed their meeting — every connection with the channels they shared,
// the Participant's own shared channels, and their stats.
func TestRecapPage_ShowsTheEvening(t *testing.T) {
	srv, deps := recapSetup(t)
	seed(t, deps.db, "ddd", "The Weird One", "ready")
	ms := NewMeetings(deps.db, deps.events)
	x, y := reload(t, deps.db, "aaa"), reload(t, deps.db, "ddd")
	if _, _, err := ms.Confirm(x, y, OriginOther); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ms.Confirm(y, x, OriginOther); err != nil {
		t.Fatal(err)
	}
	// ccc entered an email but never opted in; ddd opted in on email.
	saveChannels(t, srv, "ccc", url.Values{"email": {"ccc@example.com"}})
	saveChannels(t, srv, "ddd", url.Values{"email": {"ddd@example.com"}, "email_on": {"on"}})
	// aaa opted in on github only.
	saveChannels(t, srv, "aaa", url.Values{"github": {"octocat-aaa"}, "github_on": {"on"}})

	// The contrast that makes "always unlocked here" mean something: on the
	// Match page the dossier is still locked — the pair never met.
	matchBody := readBody(t, get(t, srv, "/user/match/aaa"))
	if !strings.Contains(matchBody, "🔒") || strings.Contains(matchBody, "Both love tabs") {
		t.Fatal("the setup: on the Match page the dossier is locked before the meeting")
	}

	if err := deps.events.Advance(PhaseClosed); err != nil {
		t.Fatal(err)
	}
	me := reload(t, deps.db, "aaa")
	body := readBody(t, get(t, srv, "/user/recap/"+recapToken(t, srv, "aaa")))

	// The persona card.
	for _, want := range []string{me.PersonaSymbol, "The Gopher", "Ships things"} {
		if !strings.Contains(body, want) {
			t.Errorf("the recap shows the persona card (%q), got: %s", want, body)
		}
	}
	// The match dossier — always unlocked here, even without a completed meeting.
	for _, want := range []string{"The YAML Wrangler", "91% compatibility", "Both love tabs", "hogs the whiteboard", "Why tabs?"} {
		if !strings.Contains(body, want) {
			t.Errorf("the recap shows the unlocked dossier (%q), got: %s", want, body)
		}
	}
	if strings.Contains(body, "🔒") {
		t.Error("the recap's dossier is always unlocked")
	}
	// The connections, with the channels they shared.
	for _, want := range []string{"The Kernel Panicker", "shared nothing", "The Weird One", "ddd@example.com"} {
		if !strings.Contains(body, want) {
			t.Errorf("the recap shows the connections (%q), got: %s", want, body)
		}
	}
	// The personal stats: two connections, the top of the leaderboard, the champion.
	for _, want := range []string{"❤️ 2 connections", "#1 on the leaderboard", "Merge Trophy"} {
		if !strings.Contains(body, want) {
			t.Errorf("the recap shows the stats (%q), got: %s", want, body)
		}
	}
	// Their own contact details — the github handle only because aaa opted in.
	if !strings.Contains(body, "octocat-aaa") {
		t.Error("the recap shows what aaa shared, including their opted-in github channel")
	}

	// The mutual rule holds on the recap too: ccc's opted-out email and the
	// handles of Participants aaa never completed a meeting with stay hidden.
	for _, leak := range []string{"ccc@example.com", "octocat-bbb", "octocat-ccc"} {
		if strings.Contains(body, leak) {
			t.Errorf("the recap leaks %q", leak)
		}
	}
}

// TestRecapPage_ForAnUnmatchedParticipant covers the recap's empty states.
func TestRecapPage_ForAnUnmatchedParticipant(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "solo", "The Loner", "ready")
	if err := deps.events.Advance(PhaseClosed); err != nil {
		t.Fatal(err)
	}

	body := readBody(t, get(t, srv, "/user/recap/"+recapToken(t, srv, "solo")))
	for _, want := range []string{"The Loner", "You weren't matched this time", "❤️ 0 connection", "No completed meetings", "You didn't share any contact details."} {
		if !strings.Contains(body, want) {
			t.Errorf("the unmatched recap should show %q, got: %s", want, body)
		}
	}
}

// TestRecapPage_PersistsAcrossARestart covers the acceptance rule: the recap
// page — token and all — survives a server restart (reopening the database).
func TestRecapPage_PersistsAcrossARestart(t *testing.T) {
	path := t.TempDir() + "/recap.db"
	db, err := NewDB(path)
	if err != nil {
		t.Fatal(err)
	}
	srv, deps := newTestServerOnDB(t, db, nil, nil)
	seed(t, deps.db, "aaa", "The Gopher", "ready")
	seed(t, deps.db, "bbb", "The YAML Wrangler", "ready")
	pair(t, deps.db, "aaa", "bbb")
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	ms := NewMeetings(deps.db, deps.events)
	x, y := reload(t, deps.db, "aaa"), reload(t, deps.db, "bbb")
	if _, _, err := ms.Confirm(x, y, OriginOther); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ms.Confirm(y, x, OriginOther); err != nil {
		t.Fatal(err)
	}
	saveChannels(t, srv, "bbb", url.Values{"email": {"bbb@example.com"}, "email_on": {"on"}})
	if err := deps.events.Advance(PhaseClosed); err != nil {
		t.Fatal(err)
	}

	token := recapToken(t, srv, "aaa")
	body := readBody(t, get(t, srv, "/user/recap/"+token))
	for _, want := range []string{"The Gopher", "Both love tabs", "The YAML Wrangler", "bbb@example.com", "❤️ 1 connection"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the recap should show %q, got: %s", want, body)
		}
	}

	// The restart: the same file, reopened and served again.
	db.Close()
	reopened, err := NewDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	srv2, _ := newTestServerOnDB(t, reopened, nil, nil)

	body2 := readBody(t, get(t, srv2, "/user/recap/"+token))
	if body2 != body {
		t.Error("the recap page is the same after a restart")
	}
	// The token itself is stable across the restart.
	token2, err := NewRecaps(reopened).TokenFor("aaa")
	if err != nil {
		t.Fatal(err)
	}
	if token2 != token {
		t.Errorf("the token persists across a restart: want %q, got %q", token, token2)
	}
}

// TestRecapPage_UnknownTokenIs404 covers the recap gating: an unguessable
// UUID is the only way in.
func TestRecapPage_UnknownTokenIs404(t *testing.T) {
	srv, _ := newTestServer(t, nil, nil)
	if resp := get(t, srv, "/user/recap/00000000-0000-0000-0000-000000000000"); resp.StatusCode != 404 {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

// TestRecapPage_AReadFailureIs500 covers the recap page's error path.
func TestRecapPage_AReadFailureIs500(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	// A real token: the fault's view fails while its row is read.
	token, err := NewRecaps(deps.db).TokenFor("a")
	if err != nil {
		t.Fatal(err)
	}
	breakOnFault(t, deps.db, "recap_read")
	if resp := get(t, srv, "/user/recap/"+token); resp.StatusCode != 500 {
		t.Errorf("want 500, got %d", resp.StatusCode)
	}
}

// TestRecapPage_APendingMeetingIsNotAConnection covers the recap's
// connection rule: only completed meetings are connections.
func TestRecapPage_APendingMeetingIsNotAConnection(t *testing.T) {
	srv, deps := contactSetup(t)
	saveChannels(t, srv, "b", url.Values{"email": {"b@example.com"}, "email_on": {"on"}})
	// One side only: the meeting is pending.
	ms := NewMeetings(deps.db, deps.events)
	if _, _, err := ms.Confirm(reload(t, deps.db, "a"), reload(t, deps.db, "b"), OriginOther); err != nil {
		t.Fatal(err)
	}

	token, err := NewRecaps(deps.db).TokenFor("a")
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, get(t, srv, "/user/recap/"+token))
	if !strings.Contains(body, "No completed meetings") {
		t.Error("a pending meeting is not a connection")
	}
	if strings.Contains(body, "b@example.com") {
		t.Error("no contact data without the completed meeting")
	}
}

// TestRecapPage_SkipsConnectionsWhoseCounterpartLeft covers a connection
// whose other side was deleted: the row stays (it is debug data) but the
// recap skips it.
func TestRecapPage_SkipsConnectionsWhoseCounterpartLeft(t *testing.T) {
	srv, deps := contactSetup(t)
	completeAB(t, deps)
	saveChannels(t, srv, "b", url.Values{"email": {"b@example.com"}, "email_on": {"on"}})
	// Deliberately invalid state: the Participant row is deleted without the
	// cleanup the delete endpoint runs, leaving the meeting row debug-only.
	deps.db.db.Exec(`DELETE FROM participants WHERE id = 'b'`)

	token, err := NewRecaps(deps.db).TokenFor("a")
	if err != nil {
		t.Fatal(err)
	}
	resp := get(t, srv, "/user/recap/"+token)
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	if body := readBody(t, resp); strings.Contains(body, "The YAML Wrangler") || strings.Contains(body, "b@example.com") {
		t.Errorf("a connection whose other side left is skipped, got: %s", body)
	}
}

// TestRecapPage_AReadFailureStillRenders covers the recap against reads that
// fail: the meetings or the channels cannot be read, so the page renders
// without them.
func TestRecapPage_AReadFailureStillRenders(t *testing.T) {
	t.Run("the meetings cannot be read", func(t *testing.T) {
		srv, deps := recapSetup(t)
		saveChannels(t, srv, "ccc", url.Values{"email": {"ccc@example.com"}, "email_on": {"on"}})
		token, err := NewRecaps(deps.db).TokenFor("aaa")
		if err != nil {
			t.Fatal(err)
		}
		breakOnFault(t, deps.db, "meet_read")

		body := readBody(t, get(t, srv, "/user/recap/"+token))
		if !strings.Contains(body, "No completed meetings") {
			t.Error("without the meetings read there are no connections")
		}
		if strings.Contains(body, "ccc@example.com") {
			t.Error("without the meetings read no channels are revealed")
		}
	})
	t.Run("the channels cannot be read", func(t *testing.T) {
		srv, deps := recapSetup(t)
		saveChannels(t, srv, "ccc", url.Values{"email": {"ccc@example.com"}, "email_on": {"on"}})
		saveChannels(t, srv, "aaa", url.Values{"github": {"octocat-aaa"}, "github_on": {"on"}})
		token, err := NewRecaps(deps.db).TokenFor("aaa")
		if err != nil {
			t.Fatal(err)
		}
		breakOnFault(t, deps.db, "channel_read")

		body := readBody(t, get(t, srv, "/user/recap/"+token))
		if !strings.Contains(body, "The Kernel Panicker") {
			t.Error("the connection still lists")
		}
		if strings.Contains(body, "ccc@example.com") || strings.Contains(body, "octocat-aaa") {
			t.Error("the failed read reveals nothing")
		}
		if !strings.Contains(body, "You didn't share any contact details.") {
			t.Error("without the channels read nothing is shared")
		}
	})
}

// TestRecapData_TheLeaderboardFailureIsLogged covers the recap's stats
// against a leaderboard read that fails: no rank, no champion — the rest of
// the page still renders. (A Participant read fails with it, so this branch
// is exercised directly.)
func TestRecapData_TheLeaderboardFailureIsLogged(t *testing.T) {
	_, deps := recapSetup(t)
	me := reload(t, deps.db, "aaa")
	breakOnFault(t, deps.db, "connection_read")

	data := deps.handler.recapData(me)
	if data["Rank"] != 0 {
		t.Errorf("without the leaderboard there is no rank, got %v", data["Rank"])
	}
	if data["Champion"] != false {
		t.Errorf("without the leaderboard there is no champion, got %v", data["Champion"])
	}
}

// TestRecap_GitHubHandlesAppearOnlyAsRevealedChannels covers the handle rule:
// on the owner's own recap a GitHub handle appears only as a revealed github
// contact channel — the owner opted in — and nowhere else outside stage.
func TestRecap_GitHubHandlesAppearOnlyAsRevealedChannels(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "aaa", "The Gopher", "ready")
	seed(t, deps.db, "bbb", "The YAML Wrangler", "ready")
	seed(t, deps.db, "ccc", "The Kernel Panicker", "ready")
	setProfile(t, deps.db, "aaa", &GitHubProfile{Login: "octocat-aaa"})
	setProfile(t, deps.db, "bbb", &GitHubProfile{Login: "octocat-bbb"})
	setProfile(t, deps.db, "ccc", &GitHubProfile{Login: "octocat-ccc"})
	pair(t, deps.db, "aaa", "bbb")
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	// The meeting with bbb — aaa's Match — completes; the one with ccc
	// never happens.
	ms := NewMeetings(deps.db, deps.events)
	x, y := reload(t, deps.db, "aaa"), reload(t, deps.db, "bbb")
	if _, _, err := ms.Confirm(x, y, OriginOther); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ms.Confirm(y, x, OriginOther); err != nil {
		t.Fatal(err)
	}
	// bbb opts in on github; aaa opts in on email only; ccc shares nothing.
	saveChannels(t, srv, "bbb", url.Values{"github": {"octocat-bbb"}, "github_on": {"on"}})
	saveChannels(t, srv, "aaa", url.Values{"email": {"aaa@example.com"}, "email_on": {"on"}})
	if err := deps.events.Advance(PhaseClosed); err != nil {
		t.Fatal(err)
	}

	body := readBody(t, get(t, srv, "/user/recap/"+recapToken(t, srv, "aaa")))
	if !strings.Contains(body, "octocat-bbb") {
		t.Error("bbb opted in after a completed meeting: their handle appears as their revealed github channel")
	}
	if strings.Contains(body, "octocat-aaa") {
		t.Error("aaa never opted in on github: their own handle does not appear on the recap")
	}
	if strings.Contains(body, "octocat-ccc") {
		t.Error("ccc shared nothing: their handle does not appear on the recap")
	}

	// Nowhere else outside stage: the Match page carries no partner handle.
	matchBody := readBody(t, get(t, srv, "/user/match/aaa"))
	if strings.Contains(matchBody, "octocat-bbb") || strings.Contains(matchBody, "octocat-ccc") {
		t.Error("the Match page carries no handles")
	}
	// The debug endpoint shows counts only, never handles as channel values.
	if body := readBody(t, get(t, srv, "/data/channels")); strings.Contains(body, "octocat") {
		t.Error("/data/channels shows counts only, never values")
	}
}

// TestRecapLink_ShowsTheQrOnlyWhenClosed covers the personal page's recap
// link: during closed, the recap page as a QR; before that, nothing — but
// the refresh hook stays, so the QR arrives by push when the event closes.
func TestRecapLink_ShowsTheQrOnlyWhenClosed(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")

	// Onboarding: no recap link, but the refresh hook stays.
	body := readBody(t, get(t, srv, "/user/recap-link/a"))
	if !strings.Contains(body, `id="recap"`) || !strings.Contains(body, `hx-get="/user/recap-link/a"`) {
		t.Errorf("the gated-off section should keep its refresh hook, got: %s", body)
	}
	if strings.Contains(body, "recap-qr") {
		t.Error("during onboarding there is no recap link")
	}

	// The social: still nothing.
	deps.events.Reveal()
	if body := readBody(t, get(t, srv, "/user/recap-link/a")); strings.Contains(body, "recap-qr") {
		t.Error("during the social there is no recap link yet")
	}

	// Closed: the recap link as a QR.
	if err := deps.events.Advance(PhaseClosed); err != nil {
		t.Fatal(err)
	}
	body = readBody(t, get(t, srv, "/user/recap-link/a"))
	token := recapToken(t, srv, "a")
	if !strings.Contains(body, `id="recap-qr"`) || !strings.Contains(body, `src="/qr?data=/user/recap/`+token+`&size=150"`) {
		t.Errorf("during closed the recap link shows as a QR, got: %s", body)
	}
	if again := recapToken(t, srv, "a"); again != token {
		t.Error("the recap token is stable")
	}

	if resp := get(t, srv, "/user/recap-link/nobody"); resp.StatusCode != 404 {
		t.Errorf("unknown participant: want 404, got %d", resp.StatusCode)
	}
}

// TestRecapLink_ATokenFailureShowsNoQr covers the recap link against a token
// creation that fails: the section renders without the QR.
func TestRecapLink_ATokenFailureShowsNoQr(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	if err := deps.events.Advance(PhaseClosed); err != nil {
		t.Fatal(err)
	}
	failWrites(t, deps.db, "recap_insert")

	body := readBody(t, get(t, srv, "/user/recap-link/a"))
	if strings.Contains(body, "recap-qr") {
		t.Error("a failed token creation shows no QR")
	}
	if !strings.Contains(body, `id="recap"`) {
		t.Error("the section keeps its refresh hook")
	}
}

// TestPersonalPages_ShowTheRecapQrWhenClosed covers the recap link on both
// personal pages: the fragment is there from the start, the QR arrives with
// the close.
func TestPersonalPages_ShowTheRecapQrWhenClosed(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "aaa", "The Gopher", "ready")
	seed(t, deps.db, "bbb", "The YAML Wrangler", "ready")
	seed(t, deps.db, "solo", "The Loner", "ready")
	pair(t, deps.db, "aaa", "bbb")
	deps.events.Reveal()

	for _, path := range []string{"/user/match/aaa", "/user/wait/solo"} {
		body := readBody(t, get(t, srv, path))
		if !strings.Contains(body, `id="recap"`) || !strings.Contains(body, `hx-get="/user/recap-link/`) {
			t.Errorf("%s should include the recap link section, got: %s", path, body)
		}
		if strings.Contains(body, `id="recap-qr"`) {
			t.Errorf("%s: no QR before the event closes", path)
		}
	}

	if err := deps.events.Advance(PhaseClosed); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/user/match/aaa", "/user/wait/solo"} {
		body := readBody(t, get(t, srv, path))
		if !strings.Contains(body, `id="recap-qr"`) || !strings.Contains(body, `src="/qr?data=/user/recap/`) {
			t.Errorf("%s: the recap QR shows during closed, got: %s", path, body)
		}
	}
}

// TestMatchStream_PushesTheRecapLinkWhenTheEventCloses covers the push: the
// match page's stream fires when the phase changes, and the recap link it
// then fetches carries the QR.
func TestMatchStream_PushesTheRecapLinkWhenTheEventCloses(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "aaa", "The Gopher", "ready")
	seed(t, deps.db, "bbb", "The YAML Wrangler", "ready")
	pair(t, deps.db, "aaa", "bbb")
	deps.events.Reveal()

	s := open(t, srv, "/user/match-stream/aaa")
	sentNothingNew(t, s, 1, "", "before the close")

	if err := deps.events.Advance(PhaseClosed); err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, "event: refresh")

	// The refresh the stream pushed re-renders the recap link — with the QR.
	if body := readBody(t, get(t, srv, "/user/recap-link/aaa")); !strings.Contains(body, "recap-qr") {
		t.Errorf("after the push the recap link shows the QR, got: %s", body)
	}
}

// TestWaitStream_PushesTheRecapLinkWhenTheEventCloses covers the wait page's
// stream the same way.
func TestWaitStream_PushesTheRecapLinkWhenTheEventCloses(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "solo", "The Loner", "ready")
	deps.events.Reveal()

	s := open(t, srv, "/user/wait-stream/solo")
	sentNothingNew(t, s, 1, "", "before the close")

	if err := deps.events.Advance(PhaseClosed); err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, "event: refresh")

	if body := readBody(t, get(t, srv, "/user/recap-link/solo")); !strings.Contains(body, "recap-qr") {
		t.Errorf("after the push the recap link shows the QR, got: %s", body)
	}
}

// TestDeleteParticipant_RemovesTheirRecapToken covers the cleanup when a
// Participant leaves: their recap token goes with them.
func TestDeleteParticipant_RemovesTheirRecapToken(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	recaps := NewRecaps(deps.db)
	token, err := recaps.TokenFor("a")
	if err != nil {
		t.Fatal(err)
	}

	if resp := del(t, srv, "/data/participant/a"); resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	if p, err := recaps.ByToken(token); err != nil || p != nil {
		t.Errorf("the token is gone with the Participant, got %v (err %v)", p, err)
	}
}

// TestDeleteParticipant_ReportsARecapsFailure covers the cleanup's error path.
func TestDeleteParticipant_ReportsARecapsFailure(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	// A token to delete: the fault's trigger cannot fire on an empty table.
	if _, err := NewRecaps(deps.db).TokenFor("a"); err != nil {
		t.Fatal(err)
	}
	failWrites(t, deps.db, "recap_delete")

	if resp := del(t, srv, "/data/participant/a"); resp.StatusCode != 500 {
		t.Errorf("want 500, got %d", resp.StatusCode)
	}
}

// TestReset_ClearsTheRecapTokens covers the reset: the recap tokens go with
// the event.
func TestReset_ClearsTheRecapTokens(t *testing.T) {
	db := newTestDB(t)
	events := NewEventStore(db)
	seed(t, db, "a", "The Gopher", "ready")
	recaps := NewRecaps(db)
	token, err := recaps.TokenFor("a")
	if err != nil {
		t.Fatal(err)
	}

	if err := events.Reset(); err != nil {
		t.Fatal(err)
	}
	if p, err := recaps.ByToken(token); err != nil || p != nil {
		t.Errorf("after a reset the tokens are cleared, got %v (err %v)", p, err)
	}
}
