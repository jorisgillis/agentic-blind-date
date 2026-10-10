package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// seed creates a Participant with a persona, profile and answers in the given pipeline step.
func seed(t *testing.T, db *DB, id, persona string, step Step) *Participant {
	t.Helper()
	if err := db.CreateParticipant(id, id, id, true); err != nil {
		t.Fatal(err)
	}
	questions := []Question{{ID: "fixed_0", Text: "Tabs or spaces?"}}
	db.SetProfile(id, &GitHubProfile{Login: id, Languages: []string{"Go"}})
	db.SetPersona(id, persona, "Ships things")
	db.SetQuestions(id, questions)
	if step != "interviewing" && step != "fetching_github" {
		db.UpdateAnswers(id, map[string]string{"fixed_0": "Tabs"})
	}
	forceStep(db, id, step)
	return reload(t, db, id)
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
	deps.db.Reveal() // after the Reveal, matched Participants see their Match
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

func TestPipelineStatus_RedirectsOrRendersByStep(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	deps.db.Reveal() // after the Reveal, matched Participants see their Match
	seed(t, deps.db, "f", "F", "fetching_github")
	seed(t, deps.db, "r", "R", "ready")
	seed(t, deps.db, "m", "M", "ready")
	seed(t, deps.db, "m2", "M2", "ready")
	pair(t, deps.db, "m", "m2")
	seed(t, deps.db, "done", "D", "interviewing")
	deps.db.UpdateAnswers("done", map[string]string{"fixed_0": "Tabs"}) // every question answered

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
	deps.db.Reveal() // after the Reveal, matched Participants see their Match
	seed(t, deps.db, "r", "The Gopher", "ready")
	seed(t, deps.db, "m", "M", "ready")
	seed(t, deps.db, "m2", "M2", "ready")
	pair(t, deps.db, "m", "m2")

	body := readBody(t, get(t, srv, "/user/wait/r"))
	if !strings.Contains(body, "Tabs or spaces?") || !strings.Contains(body, "The Gopher") {
		t.Errorf("wait page should show the persona and answers")
	}
	if loc := get(t, srv, "/user/wait/m").Header.Get("Location"); loc != "/user/match/m" {
		t.Errorf("matched: want match page, got %q", loc)
	}
	if resp := get(t, srv, "/user/wait/nobody"); resp.StatusCode != 404 {
		t.Errorf("unknown: want 404, got %d", resp.StatusCode)
	}
}

func TestWaitStatus(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	deps.db.Reveal() // after the Reveal, matched Participants see their Match
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

func TestMatchPage_ShowsThePartnerAndTheAssessment(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	deps.db.Reveal() // after the Reveal, matched Participants see their Match
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	seed(t, deps.db, "c", "The Snake", "ready")
	pair(t, deps.db, "a", "b")

	body := readBody(t, get(t, srv, "/user/match/a"))
	for _, want := range []string{"The Crab", "Both love tabs", "hogs the whiteboard", "Why tabs?", "The Snake"} {
		if !strings.Contains(body, want) {
			t.Errorf("match page should contain %q", want)
		}
	}
	if loc := get(t, srv, "/user/match/c").Header.Get("Location"); loc != "/user/wait/c" {
		t.Errorf("unmatched: want wait page, got %q", loc)
	}
	if resp := get(t, srv, "/user/match/nobody"); resp.StatusCode != 404 {
		t.Errorf("unknown: want 404, got %d", resp.StatusCode)
	}
}

func TestExplore_UnknownParticipantsAndFailedAssessments(t *testing.T) {
	llm := newFakeLLM().onErr("matchmaker", fakeError("mistral HTTP 500"))
	srv, deps := newTestServer(t, llm, nil)
	seed(t, deps.db, "a", "A", "ready")
	seed(t, deps.db, "b", "B", "ready")

	if resp := get(t, srv, "/user/explore/nobody/b"); resp.StatusCode != 404 {
		t.Errorf("unknown me: want 404, got %d", resp.StatusCode)
	}
	if resp := get(t, srv, "/user/explore/a/nobody"); resp.StatusCode != 404 {
		t.Errorf("unknown other: want 404, got %d", resp.StatusCode)
	}
	if resp := get(t, srv, "/user/explore/a/b"); resp.StatusCode != 500 {
		t.Errorf("failed assessment: want 500, got %d", resp.StatusCode)
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
	if body := readBody(t, get(t, srv, "/bigscreen/state")); !strings.Contains(body, "A") {
		t.Errorf("screen state should list the participants")
	}
}

func TestAdminRematch_PairsTheReadyParticipants(t *testing.T) {
	llm := newFakeLLM().on("matchmaker", `{"score": 70, "reason": "fine"}`)
	srv, deps := newTestServer(t, llm, nil)
	seed(t, deps.db, "a", "A", "ready")
	seed(t, deps.db, "b", "B", "ready")

	resp := post(t, srv, "/admin/rematch", nil)

	if resp.Header.Get("Location") != "/admin" {
		t.Errorf("want redirect back to admin, got %q", resp.Header.Get("Location"))
	}
	eventually(t, "both matched", func() bool {
		return reload(t, deps.db, "a").MatchedWith == "b" && reload(t, deps.db, "b").MatchedWith == "a"
	})
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

// stream runs an SSE handler until it returns or the timeout passes, and returns what it wrote.
func stream(t *testing.T, srv *testSrv, path string, timeout time.Duration) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req := httptest.NewRequest("GET", path, nil).WithContext(ctx)
	w := &flusherRecorder{httptest.NewRecorder()}
	srv.h.ServeHTTP(w, req)
	return w.Body.String()
}

func TestDeletingAMatchedParticipant_LeavesNoTraceOnTheirPartnerOrTheBigScreen(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	deps.db.Reveal()
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
	deps.db.Reveal()
	deps.db.CreateParticipant("ada", "no-github-1234abcd", "Ada Lovelace", false)
	deps.db.SetProfile("ada", &GitHubProfile{})
	deps.db.SetPersona("ada", "The Analyst", "Computes")
	forceStep(deps.db, "ada", "ready")
	seed(t, deps.db, "octo", "The Gopher", "ready")
	pair(t, deps.db, "ada", "octo")
	deps.db.CreateParticipant("zed", "no-github-5678efgh", "Zed", false)
	deps.db.SetQuestions("zed", ExtraQuestions)
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
	if body := readBody(t, get(t, srv, "/bigscreen/graph-data")); !strings.Contains(body, `"handle": "Ada Lovelace"`) {
		t.Errorf("after the Reveal, the Big Screen identifies a Non-GitHub User by name:\n%s", body)
	}
}
