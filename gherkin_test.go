// Gherkin journey bindings (ADR-0025): the seed adopted from the
// wayfinder prototype. The steps reuse the in-package test fakes
// (fakeLLM, fakeGitHub, newTestServer) instead of the Node e2e stubs:
// same seams (LLM persona + matchmaker scripted like the stubs script
// them), in-process and fast. A single bindings file until size demands
// a domain split over the one shared world.
package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cucumber/godog"
)

// gherkinWorld carries one scenario's app: a fresh in-memory server per
// scenario and the participants the steps registered, by first name.
type gherkinWorld struct {
	t      *testing.T
	srv    *testSrv
	deps   *testDeps
	people map[string]string
}

func TestGherkinFeatures(t *testing.T) {
	w := &gherkinWorld{t: t}
	suite := godog.TestSuite{
		ScenarioInitializer: w.initSteps,
		Options: &godog.Options{
			TestingT: t,
			Format:   "pretty",
			Paths:    []string{"features"},
			Strict:   true,
			NoColors: true,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("gherkin scenarios failed")
	}
}

// gherkinSteps lets journey bindings register steps from their own files
// (per ADR-0025's domain split over the one shared world): each
// gherkin_*_test.go appends its registrar in an init() function.
var gherkinSteps []func(*godog.ScenarioContext, *gherkinWorld)

func (w *gherkinWorld) initSteps(ctx *godog.ScenarioContext) {
	w.people = map[string]string{}

	ctx.Given(`^a fresh event$`, w.freshEvent)
	ctx.When(`^(\w+) joins without a GitHub account$`, w.joinsWithout)
	ctx.When(`^(\w+) answers every interview question$`, w.answersEverything)
	ctx.Then(`^(\w+) has the persona "([^"]*)"$`, w.hasPersona)
	ctx.Then(`^(\w+) is ready for matching$`, w.isReady)
	ctx.Then(`^(\w+) waits for a match$`, w.waitsForMatch)

	ctx.Given(`^(\w+) has joined and answered their interview$`, w.hasJoinedAndAnswered)
	ctx.Given(`^(\w+) and (\w+) are matched$`, w.areMatched)
	ctx.When(`^the host advances the event to the Social$`, w.hostAdvancesToSocial)
	ctx.When(`^(\w+) confirms a meeting with (\w+)'s meet code$`, w.confirmsMeeting)
	ctx.Then(`^(\w+) and (\w+) have a completed meeting$`, w.haveCompletedMeeting)
	ctx.Then(`^both (\w+) and (\w+) have one connection$`, w.bothHaveOneConnection)

	for _, register := range gherkinSteps {
		register(ctx, w)
	}
}

// --- Given/When steps -------------------------------------------------

func (w *gherkinWorld) freshEvent() {
	llm := newFakeLLM().
		on("personality generator", `{"name": "The Gopher", "tagline": "Ships fast"}`).
		on("matchmaker", `{"score": 80}`)
	w.srv, w.deps = newTestServer(w.t, llm, nil)
}

func (w *gherkinWorld) joinsWithout(name string) {
	rec := w.post("/user/join", url.Values{"name": {name}, "no_github": {"on"}}, "")
	if rec.Code != http.StatusSeeOther {
		w.t.Fatalf("join %s: status %d, body %s", name, rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	id := strings.TrimPrefix(loc, "/user/onboard/")
	w.people[name] = id
}

func (w *gherkinWorld) answersEverything(name string) {
	// Join kicks off preparation asynchronously: wait for the Interview to start.
	w.eventually(name+"'s interview starts", func() bool {
		p := w.person(name)
		return p.PipelineStep == StepInterviewing && len(p.Questions) > 0
	})
	for _, q := range w.person(name).Questions {
		rec := w.post("/user/answer/"+w.people[name], url.Values{"answer": {gherkinAnswerFor(q)}}, w.people[name])
		if rec.Code != http.StatusOK {
			w.t.Fatalf("%s answering %q: status %d, body %s", name, q.ID, rec.Code, rec.Body.String())
		}
	}
}

func (w *gherkinWorld) hasJoinedAndAnswered(name string) {
	w.joinsWithout(name)
	w.answersEverything(name)
	w.isReady(name) // also waits out the async persona generation
}

func (w *gherkinWorld) areMatched(a, b string) {
	w.eventually(a+" and "+b+" are matched", func() bool {
		pa, pb := w.person(a), w.person(b)
		return pa.MatchedWith == w.people[b] && pb.MatchedWith == w.people[a]
	})
}

func (w *gherkinWorld) hostAdvancesToSocial() {
	rec := w.post("/admin/phase", url.Values{"to": {"social"}}, "")
	if rec.Code >= 400 {
		w.t.Fatalf("advance to social: status %d, body %s", rec.Code, rec.Body.String())
	}
}

func (w *gherkinWorld) confirmsMeeting(confirmer, target string) {
	rec := w.post("/user/met/"+w.people[confirmer], url.Values{"code": {w.person(target).MeetCode}}, w.people[confirmer])
	if rec.Code != http.StatusOK {
		w.t.Fatalf("%s confirming %s: status %d, body %s", confirmer, target, rec.Code, rec.Body.String())
	}
}

// --- Then steps -------------------------------------------------------

func (w *gherkinWorld) hasPersona(name, persona string) {
	w.eventually(name+" has a persona", func() bool {
		return w.person(name).PersonaName == persona
	})
}

func (w *gherkinWorld) isReady(name string) {
	w.eventually(name+" is ready", func() bool {
		p := w.person(name)
		return p.PipelineStep == StepReady && p.PersonaName != ""
	})
}

func (w *gherkinWorld) waitsForMatch(name string) {
	rec := w.get("/user/wait/"+w.people[name], w.people[name])
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `id="wait-status"`) {
		w.t.Fatalf("%s wait page: status %d", name, rec.Code)
	}
}

func (w *gherkinWorld) haveCompletedMeeting(a, b string) {
	rec := w.get("/user/match/"+w.people[a], w.people[a])
	if !strings.Contains(rec.Body.String(), "You met!") {
		w.t.Fatalf("match page after handshake does not show 'You met!': status %d", rec.Code)
	}
}

func (w *gherkinWorld) bothHaveOneConnection(a, b string) {
	board, err := w.deps.handler.meetings.Leaderboard()
	if err != nil {
		w.t.Fatalf("leaderboard: %v", err)
	}
	count := map[string]int{}
	for _, row := range board {
		count[row.ID] = row.Connections
	}
	if count[w.people[a]] != 1 || count[w.people[b]] != 1 {
		w.t.Fatalf("want 1 connection each for %s and %s, leaderboard: %+v", a, b, board)
	}
}

// --- helpers ----------------------------------------------------------

func (w *gherkinWorld) person(name string) *Participant {
	p, err := w.deps.handler.store.Get(w.people[name])
	if err != nil {
		w.t.Fatalf("participant %s: %v", name, err)
	}
	return p
}

func gherkinAnswerFor(q Question) string {
	if len(q.Options) == 0 {
		return "definitely gophers"
	}
	if q.MaxSelections > 1 {
		return `["` + q.Options[0] + `"]`
	}
	return q.Options[0]
}

func (w *gherkinWorld) eventually(what string, cond func() bool) {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	w.t.Fatalf("timed out waiting for: %s", what)
}

func (w *gherkinWorld) get(path, cookie string) *httptest.ResponseRecorder {
	return w.do(http.MethodGet, path, cookie, nil)
}

func (w *gherkinWorld) post(path string, vals url.Values, cookie string) *httptest.ResponseRecorder {
	return w.do(http.MethodPost, path, cookie, vals)
}

func (w *gherkinWorld) do(method, path, cookie string, vals url.Values) *httptest.ResponseRecorder {
	var body io.Reader
	if vals != nil {
		body = strings.NewReader(vals.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	if vals != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: cookieName, Value: cookie})
	}
	rec := httptest.NewRecorder()
	w.srv.h.ServeHTTP(rec, req)
	return rec
}
