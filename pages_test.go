package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// seed creates a Participant with a persona, profile and answers in the given pipeline step.
func seed(t *testing.T, db *DB, id, persona string, step Step) *Participant {
	t.Helper()
	if err := db.CreateParticipant(id, id, id, true); err != nil {
		t.Fatal(err)
	}
	questions := []Question{{ID: "fixed_0", Text: "Tabs or spaces?"}}
	setProfile(t, db, id, &GitHubProfile{Login: id, Languages: []string{"Go"}})
	setPersona(t, db, id, persona, "Ships things")
	forceQuestions(db, id, questions)
	if step != "interviewing" && step != "fetching_github" {
		updateAnswers(t, db, id, map[string]string{"fixed_0": "Tabs"})
	}
	forceStep(db, id, step)
	return reload(t, db, id)
}

// awaitingPersona puts a Participant at the persona step, as if their Interview just completed.
func awaitingPersona(t *testing.T, db *DB, id string) {
	t.Helper()
	db.CreateParticipant(id, id, id, true)
	setProfile(t, db, id, &GitHubProfile{Login: id, Languages: []string{"Go"}})
	forceStep(db, id, StepCreatingPersona)
}

func pair(t *testing.T, db *DB, a, b string) {
	t.Helper()
	result := &matchResult{Score: 91, Reason: "Both love tabs", RedFlags: []string{"hogs the whiteboard"}, GreenFlags: []string{"tabs"}, Icebreakers: []string{"Why tabs?"}}
	if _, err := NewRelationships(db).Pair(Match{A: reload(t, db, a), B: reload(t, db, b), Result: result}); err != nil {
		t.Fatal(err)
	}
}

func withCookie(t *testing.T, srv *testSrv, path, id string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", srv.URL+path, nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: id})
	client := srv.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestLanding_ReturningParticipantIsSentToOnboarding(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "p1", "The Gopher", "interviewing")

	resp := withCookie(t, srv, "/user", "p1")

	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/user/onboard/p1" {
		t.Errorf("want redirect to onboarding, got %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestLanding_UnknownCookieIsClearedAndTheFormShown(t *testing.T) {
	srv, _ := newTestServer(t, nil, nil)

	resp := withCookie(t, srv, "/user", "gone")

	if resp.StatusCode != 200 {
		t.Errorf("want the landing page, got %d", resp.StatusCode)
	}
	if c := resp.Header.Get("Set-Cookie"); !strings.Contains(c, cookieName+"=;") {
		t.Errorf("want the stale cookie cleared, got %q", c)
	}
}

func TestOnboard_SendsEachPipelineStepToItsPage(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	deps.events.Reveal() // after the Reveal, matched Participants see their Match
	seed(t, deps.db, "i", "I", "interviewing")
	seed(t, deps.db, "r", "R", "ready")
	seed(t, deps.db, "m", "M", "ready")
	seed(t, deps.db, "m2", "M2", "ready")
	pair(t, deps.db, "m", "m2")

	if resp := get(t, srv, "/user/onboard/i"); resp.StatusCode != 200 {
		t.Errorf("interviewing: want 200, got %d", resp.StatusCode)
	}
	if loc := get(t, srv, "/user/onboard/r").Header.Get("Location"); loc != "/user/wait/r" {
		t.Errorf("ready: want wait page, got %q", loc)
	}
	if loc := get(t, srv, "/user/onboard/m").Header.Get("Location"); loc != "/user/match/m" {
		t.Errorf("matched: want match page, got %q", loc)
	}
	if resp := get(t, srv, "/user/onboard/nobody"); resp.StatusCode != 404 {
		t.Errorf("unknown: want 404, got %d", resp.StatusCode)
	}
}

func TestOnboard_ShowsTheQuestionWhenTheInterviewAlreadyStarted(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "i", "I", "interviewing") // one unanswered question, as after a reload

	body := readBody(t, get(t, srv, "/user/onboard/i"))

	if strings.Contains(body, "Agent pipeline running") {
		t.Errorf("want the interview, got the preparing placeholder")
	}
	if !strings.Contains(body, "Tabs or spaces?") {
		t.Errorf("want the first question on a settled interview")
	}
}

func TestPipelineStatus_RedirectsOrRendersByStep(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	deps.events.Reveal() // after the Reveal, matched Participants see their Match
	seed(t, deps.db, "f", "F", "fetching_github")
	seed(t, deps.db, "r", "R", "ready")
	seed(t, deps.db, "m", "M", "ready")
	seed(t, deps.db, "m2", "M2", "ready")
	pair(t, deps.db, "m", "m2")
	seed(t, deps.db, "done", "D", "interviewing")
	updateAnswers(t, deps.db, "done", map[string]string{"fixed_0": "Tabs"}) // every question answered

	if body := readBody(t, get(t, srv, "/user/pipeline/f")); !strings.Contains(body, "Preparing your interview questions") {
		t.Errorf("fetching_github: want the preparing state")
	}
	if loc := get(t, srv, "/user/pipeline/r").Header.Get("HX-Redirect"); loc != "/user/wait/r" {
		t.Errorf("ready: want HX-Redirect to wait, got %q", loc)
	}
	if loc := get(t, srv, "/user/pipeline/m").Header.Get("HX-Redirect"); loc != "/user/match/m" {
		t.Errorf("matched: want HX-Redirect to match, got %q", loc)
	}
	if loc := get(t, srv, "/user/pipeline/done").Header.Get("HX-Redirect"); loc != "/user/wait/done" {
		t.Errorf("all answered: wait while the persona is crafted, got %q", loc)
	}
	if resp := get(t, srv, "/user/pipeline/nobody"); resp.StatusCode != 404 {
		t.Errorf("unknown: want 404, got %d", resp.StatusCode)
	}
}

func TestWait_ShowsTheAnswersUntilMatched(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	deps.events.Reveal() // after the Reveal, matched Participants see their Match
	seed(t, deps.db, "r", "The Gopher", "ready")
	setProfile(t, deps.db, "r", &GitHubProfile{Login: "r", Languages: []string{"Go"}, AccountAgeDays: 730})
	seed(t, deps.db, "m", "M", "ready")
	seed(t, deps.db, "m2", "M2", "ready")
	pair(t, deps.db, "m", "m2")

	body := readBody(t, get(t, srv, "/user/wait/r"))
	if !strings.Contains(body, "Tabs or spaces?") || !strings.Contains(body, "The Gopher") {
		t.Errorf("wait page should show the persona and answers")
	}
	if !strings.Contains(body, "2 yrs") {
		t.Errorf("wait page should show the GitHub account age")
	}
	if loc := get(t, srv, "/user/wait/m").Header.Get("Location"); loc != "/user/match/m" {
		t.Errorf("matched: want match page, got %q", loc)
	}
	if resp := get(t, srv, "/user/wait/nobody"); resp.StatusCode != 404 {
		t.Errorf("unknown: want 404, got %d", resp.StatusCode)
	}
}

func TestWaitStatus_returnsCorrectStatus(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	deps.events.Reveal() // after the Reveal, matched Participants see their Match
	seed(t, deps.db, "r", "R", "ready")
	seed(t, deps.db, "m", "M", "ready")
	seed(t, deps.db, "m2", "M2", "ready")
	pair(t, deps.db, "m", "m2")

	if resp := get(t, srv, "/user/wait-status/r"); resp.StatusCode != 200 || resp.Header.Get("HX-Redirect") != "" {
		t.Errorf("ready: want the status fragment, got %d", resp.StatusCode)
	}
	if loc := get(t, srv, "/user/wait-status/m").Header.Get("HX-Redirect"); loc != "/user/match/m" {
		t.Errorf("matched: want HX-Redirect to match, got %q", loc)
	}
	if resp := get(t, srv, "/user/wait-status/nobody"); resp.StatusCode != 404 {
		t.Errorf("unknown: want 404, got %d", resp.StatusCode)
	}
}

// TestPersonaCard covers #125: the wait page's persona card is a refreshable
// fragment — the persona is crafted in the background after the last
// interview answer, so the fragment serves a placeholder until it exists.
func TestPersonaCard(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "r", "The Bard", "ready")
	if err := deps.db.CreateParticipant("crafting", "crafting", "Crafting", true); err != nil {
		t.Fatal(err)
	}
	forceStep(deps.db, "crafting", StepCreatingPersona)
	seed(t, deps.db, "m", "M", "ready")
	seed(t, deps.db, "m2", "M2", "ready")
	pair(t, deps.db, "m", "m2")

	// The wait page loads the card as a fragment and refreshes it by push.
	body := readBody(t, get(t, srv, "/user/wait/r"))
	for _, want := range []string{`hx-get="/user/persona/r"`, `htmx.trigger('#persona-card', 'refresh')`} {
		if !strings.Contains(body, want) {
			t.Errorf("the wait page should contain %q", want)
		}
	}

	// A ready Participant sees their persona card.
	body = readBody(t, get(t, srv, "/user/persona/r"))
	for _, want := range []string{`id="persona-card"`, `hx-get="/user/persona/r"`, "The Bard", "Ships things", `src="/qr?data=/user/wait/r&size=120"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the persona card should show %q, got: %s", want, body)
		}
	}

	// A Participant whose persona is still being crafted sees a placeholder.
	body = readBody(t, get(t, srv, "/user/persona/crafting"))
	if !strings.Contains(body, "Crafting your persona") {
		t.Errorf("want the crafting placeholder, got: %s", body)
	}
	if strings.Contains(body, "/qr?data=") {
		t.Error("no profile QR before the persona exists")
	}

	// A matched Participant belongs on the match page once the social begins.
	deps.events.Reveal()
	if loc := get(t, srv, "/user/persona/m").Header.Get("HX-Redirect"); loc != "/user/match/m" {
		t.Errorf("matched: want HX-Redirect to match, got %q", loc)
	}
	if resp := get(t, srv, "/user/persona/nobody"); resp.StatusCode != 404 {
		t.Errorf("unknown: want 404, got %d", resp.StatusCode)
	}
}

// TestMatchPage_ShowsThePartnerAndTheLockedDossier covers the Reveal: the
// Match page shows the partner's full persona card and the "find them in the
// room" panel — personas only, never the partner's handle — while the
// dossier (reason, flags, icebreakers) starts out locked, and Explore's
// "Others at the event" section is gone.
func TestMatchPage_ShowsThePartnerAndTheLockedDossier(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	deps.events.Reveal() // after the Reveal, matched Participants see their Match
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	seed(t, deps.db, "c", "The Snake", "ready")
	pair(t, deps.db, "a", "b")

	body := readBody(t, get(t, srv, "/user/match/a"))
	for _, want := range []string{"The Crab", "Ships things", "91%", "Find them in the room", "🔒", "to unlock"} {
		if !strings.Contains(body, want) {
			t.Errorf("match page should contain %q", want)
		}
	}
	for _, absent := range []string{"@b", "Both love tabs", "hogs the whiteboard", "Why tabs?", "The Snake", "Others at the event", "/user/explore"} {
		if strings.Contains(body, absent) {
			t.Errorf("match page must not contain %q while the dossier is locked", absent)
		}
	}
	if loc := get(t, srv, "/user/match/c").Header.Get("Location"); loc != "/user/wait/c" {
		t.Errorf("unmatched: want wait page, got %q", loc)
	}
	if resp := get(t, srv, "/user/match/nobody"); resp.StatusCode != 404 {
		t.Errorf("unknown: want 404, got %d", resp.StatusCode)
	}
}

func TestBigScreen_GraphHasMatchedAndPotentialEdges(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")
	seed(t, deps.db, "b", "B", "ready")
	seed(t, deps.db, "c", "C", "ready")
	pair(t, deps.db, "a", "b")

	var graph struct {
		Nodes []graphNode `json:"nodes"`
		Edges []graphEdge `json:"edges"`
	}
	if err := json.Unmarshal([]byte(readBody(t, get(t, srv, "/bigscreen/graph-data"))), &graph); err != nil {
		t.Fatal(err)
	}

	if len(graph.Nodes) != 3 {
		t.Errorf("nodes: want 3, got %d", len(graph.Nodes))
	}
	var matched, potential int
	for _, e := range graph.Edges {
		if e.Matched {
			matched++
			if e.Score != 91 {
				t.Errorf("matched edge score: want 91, got %d", e.Score)
			}
		} else {
			potential++
		}
	}
	if matched != 1 || potential != 2 {
		t.Errorf("want 1 matched edge and 2 potential edges (C to A and B), got %d and %d", matched, potential)
	}
	if resp := get(t, srv, "/bigscreen"); resp.StatusCode != 200 {
		t.Errorf("big screen: want 200, got %d", resp.StatusCode)
	}
}

func TestAdminRematch_PairsTheReadyParticipants(t *testing.T) {
	llm := newFakeLLM().on("matchmaker", `{"score": 70, "reason": "fine"}`)
	srv, deps := newTestServer(t, llm, nil)
	seed(t, deps.db, "a", "A", "ready")
	seed(t, deps.db, "b", "B", "ready")

	resp := post(t, srv, "/admin/rematch", nil)

	// The host lands back on the console with a confirmation notice (#127).
	loc := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(loc, "/admin?notice=") {
		t.Errorf("want a redirect to the admin console with a notice, got %d %q", resp.StatusCode, loc)
	}
	eventually(t, "both matched", func() bool {
		return reload(t, deps.db, "a").MatchedWith == "b" && reload(t, deps.db, "b").MatchedWith == "a"
	})
}

// TestAdminRematch_IsRejectedOnceMatchingIsFrozen covers #115 and #127: from
// the social onward the endpoint rejects the rematch — the host lands back on
// the console with a notice explaining why, no matching runs and the frozen
// pairs are left exactly as they were.
func TestAdminRematch_IsRejectedOnceMatchingIsFrozen(t *testing.T) {
	llm := newFakeLLM().on("matchmaker", `{"score": 70, "reason": "fine"}`)
	srv, deps := newTestServer(t, llm, nil)
	seed(t, deps.db, "a", "A", "ready")
	seed(t, deps.db, "b", "B", "ready")
	seed(t, deps.db, "c", "C", "ready")
	seed(t, deps.db, "d", "D", "ready")
	pair(t, deps.db, "a", "b")
	pair(t, deps.db, "c", "d")
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}

	resp := post(t, srv, "/admin/rematch", nil)

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("from the social onward: want a redirect, got %d", resp.StatusCode)
	}
	if loc, _ := url.QueryUnescape(resp.Header.Get("Location")); !strings.Contains(loc, "Matching is frozen") {
		t.Errorf("the rejection should say why, got %q", loc)
	}
	// No matching ran: the pairs are final and nothing was logged.
	if n := llm.callsMatching("matchmaker"); n != 0 {
		t.Errorf("a rejected rematch must not assess any pair, got %d calls", n)
	}
	for id, want := range map[string]string{"a": "b", "b": "a", "c": "d", "d": "c"} {
		if got := reload(t, deps.db, id).MatchedWith; got != want {
			t.Errorf("%s matched with %q, want %q", id, got, want)
		}
	}
	if msgs, _ := deps.db.GetRecentActivity(10); strings.Contains(strings.Join(msgs, "\n"), "ematch") {
		t.Errorf("a rejected rematch should log nothing about rematching, got %v", msgs)
	}
}

// TestAdmin_NoticeIsRendered covers #127: a notice query parameter (set by
// host actions like the rematch) is shown on the console, HTML-escaped.
func TestAdmin_NoticeIsRendered(t *testing.T) {
	srv, _ := newTestServer(t, nil, nil)

	if body := readBody(t, get(t, srv, "/admin")); strings.Contains(body, "border-indigo-700") {
		t.Error("no notice box without the query parameter")
	}

	notice := "🔄 Rematch started — <b>the matchmaker</b>"
	body := readBody(t, get(t, srv, "/admin?notice="+url.QueryEscape(notice)))
	if !strings.Contains(body, "🔄 Rematch started — &lt;b&gt;the matchmaker&lt;/b&gt;") {
		t.Errorf("the notice should be rendered HTML-escaped, got: %s", body)
	}
}

func TestRematch_NeedsTwoReadyParticipants(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")

	if err := deps.matchmaking.Rematch(); err == nil {
		t.Error("want error with a single ready participant")
	}
}

func TestAdminRevealAndDelete(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")

	if loc := post(t, srv, "/admin/reveal", nil).Header.Get("Location"); loc != "/admin" {
		t.Errorf("reveal: want redirect to admin, got %q", loc)
	}

	req, _ := http.NewRequest("DELETE", srv.URL+"/data/participant/a", nil)
	resp, err := srv.Client().Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("delete: %v %v", resp, err)
	}
	if _, err := deps.db.GetParticipant("a"); err == nil {
		t.Error("participant should be gone after delete")
	}
}

func TestDeletingAMatchedParticipant_LeavesNoTraceOnTheirPartnerOrTheBigScreen(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	deps.events.Reveal()
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	pair(t, deps.db, "a", "b")

	req, _ := http.NewRequest("DELETE", srv.URL+"/data/participant/a", nil)
	if resp, err := srv.Client().Do(req); err != nil || resp.StatusCode != 200 {
		t.Fatalf("delete: %v %v", resp, err)
	}

	if loc := get(t, srv, "/user/match/b").Header.Get("Location"); loc != "/user/wait/b" {
		t.Errorf("the partner's Match page should be gone, got redirect %q", loc)
	}
	var graph struct {
		Edges []graphEdge `json:"edges"`
	}
	json.Unmarshal([]byte(readBody(t, get(t, srv, "/bigscreen/graph-data"))), &graph)
	for _, e := range graph.Edges {
		if e.Source == "a" || e.Target == "a" {
			t.Errorf("Big Screen still has an edge to the deleted Participant: %+v", e)
		}
	}
}

func TestNonGitHubUser_NoScreenShowsTheGeneratedHandle(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	deps.events.Reveal()
	deps.db.CreateParticipant("ada", "no-github-1234abcd", "Ada Lovelace", false)
	setProfile(t, deps.db, "ada", &GitHubProfile{})
	setPersona(t, deps.db, "ada", "The Analyst", "Computes")
	forceStep(deps.db, "ada", "ready")
	seed(t, deps.db, "octo", "The Gopher", "ready")
	pair(t, deps.db, "ada", "octo")
	deps.db.CreateParticipant("zed", "no-github-5678efgh", "Zed", false)
	forceQuestions(deps.db, "zed", ExtraQuestions)
	forceStep(deps.db, "zed", "interviewing")

	for _, path := range []string{"/user/onboard/zed", "/user/match/ada", "/user/match/octo", "/data", "/bigscreen/graph-data"} {
		resp := get(t, srv, path)
		if resp.StatusCode != 200 {
			t.Errorf("%s: status %d", path, resp.StatusCode)
			continue
		}
		if body := readBody(t, resp); strings.Contains(body, "no-github-") {
			t.Errorf("%s shows the generated handle", path)
		}
	}
	// The Big Screen shows persona names only, in every phase — never the
	// Non-GitHub User's real name or generated handle (#113's privacy baseline).
	if body := readBody(t, get(t, srv, "/bigscreen/graph-data")); strings.Contains(body, "Ada Lovelace") || !strings.Contains(body, "The Analyst") {
		t.Errorf("the Big Screen should show the persona, not the name:\n%s", body)
	}
}
