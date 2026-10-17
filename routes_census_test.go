package main

// The route census (ADR-0022): every route in routes.go is asserted to exist
// by its kind — pages and fragments by their marker, JSON by its top-level
// shape, streams by an event payload, binaries by content type and size,
// actions by their status and redirect target. The table is the single
// registration site: the mux must register exactly the table's patterns, and
// every row must carry kind, marker, journey and a plan here.

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// ── the fixture: one staged evening ─────────────────────────────────────────

// censusFixture is one staged evening on a fresh in-process server: a matched
// pair (alpha, beta), extra ready participants (g1..g4), one mid-interview
// participant (iv), a slide deck, a board post and a recap token. Plans add
// phase-specific state on top.
type censusFixture struct {
	srv      *testSrv
	deps     *testDeps
	alpha    *Participant
	beta     *Participant
	token    string
	deck     int64
	post     int64
	signup   int64
	question int64
}

func newCensusFixture(t *testing.T) *censusFixture {
	t.Helper()
	srv, deps := newTestServer(t, nil, nil)
	f := &censusFixture{srv: srv, deps: deps}

	f.alpha = seed(t, deps.db, "alpha", "The Alpha", "ready")
	f.beta = seed(t, deps.db, "beta", "The Beta", "ready")
	pair(t, deps.db, "alpha", "beta")
	for _, id := range []string{"g1", "g2", "g3", "g4"} {
		seed(t, deps.db, id, "Guest "+id, "ready")
	}
	seed(t, deps.db, "iv", "The Interviewee", "interviewing")
	forceQuestions(deps.db, "iv", []Question{{ID: "q1", Text: "One?"}, {ID: "q2", Text: "Two?"}})
	deps.db.LogActivity("census fixture activity")

	// A deck padded past the binary size bound; the upload helper's sample
	// PDF is tiny.
	pdf := append(append([]byte(nil), samplePDF...), bytes.Repeat([]byte("% census padding\n"), 64)...)
	deck, err := deps.slides.Add("Census deck", "The Speaker", "deck.pdf", bytes.NewReader(pdf))
	if err != nil {
		t.Fatal(err)
	}
	f.deck = deck.ID

	if err := deps.handler.board.Post(f.alpha, "a census post"); err != nil {
		t.Fatal(err)
	}
	posts, err := deps.handler.board.Visible("alpha")
	if err != nil || len(posts) == 0 {
		t.Fatalf("board post: %v (%d posts)", err, len(posts))
	}
	f.post = posts[0].ID

	token, err := deps.handler.recaps.TokenFor("alpha")
	if err != nil {
		t.Fatal(err)
	}
	f.token = token
	return f
}

// ── the plans: how to drive each route ──────────────────────────────────────

// censusPlan is how the census drives one table row: the phase to stage, any
// extra state, and the request to make. who names the participant filling
// {id} ("alpha" by default); path/query/cookie/form build the request.
type censusPlan struct {
	phase  Phase
	stage  func(*testing.T, *censusFixture)
	who    string
	path   string
	query  string
	form   func(*censusFixture) url.Values
	cookie string
	raw    func(*testing.T, *censusFixture) *http.Request
	want   int                                           // kind action: the expected status
	nudge  func(*testing.T, *censusFixture, *liveStream) // kind sse: make the stream say something
}

func staticForm(v url.Values) func(*censusFixture) url.Values {
	return func(*censusFixture) url.Values { return v }
}

// idForm is a form of one dynamic numeric field.
func idForm(field string, value func(*censusFixture) int64, extra url.Values) func(*censusFixture) url.Values {
	return func(f *censusFixture) url.Values {
		v := url.Values{}
		for k, vs := range extra {
			v[k] = vs
		}
		v.Set(field, strconv.FormatInt(value(f), 10))
		return v
	}
}

// stageMission generates alpha's mission card (social or show & tell only).
func stageMission(t *testing.T, f *censusFixture) {
	t.Helper()
	if _, err := f.deps.missions.GenerateFor("alpha"); err != nil {
		t.Fatal(err)
	}
}

// stageQueuedSignup adds a queued sign-up by g1 for the host queue routes.
func stageQueuedSignup(t *testing.T, f *censusFixture) {
	t.Helper()
	if err := f.deps.showtell.Signup(reload(t, f.deps.db, "g1"), "Census talk", "About testing", ""); err != nil {
		t.Fatal(err)
	}
	f.signup = newestSignupID(t, f)
}

// stagePresenterQuestion puts g1 on stage and asks a question as alpha,
// already on the big screen — so the host's "answered" action is the next
// legal transition.
func stagePresenterQuestion(t *testing.T, f *censusFixture) {
	t.Helper()
	stageQueuedSignup(t, f)
	if _, err := f.deps.showtell.Stage(f.signup); err != nil {
		t.Fatal(err)
	}
	if err := f.deps.showtell.Ask(f.alpha, "Why test?"); err != nil {
		t.Fatal(err)
	}
	questions, err := f.deps.showtell.Questions()
	if err != nil || len(questions) == 0 {
		t.Fatalf("show & tell questions: %v (%d)", err, len(questions))
	}
	f.question = questions[len(questions)-1].ID
	if _, err := f.deps.showtell.TransitionQuestion(f.question, QuestionAsked); err != nil {
		t.Fatal(err)
	}
}

func newestSignupID(t *testing.T, f *censusFixture) int64 {
	t.Helper()
	queue, err := f.deps.showtell.Queue()
	if err != nil || len(queue) == 0 {
		t.Fatalf("show & tell queue: %v (%d)", err, len(queue))
	}
	return queue[len(queue)-1].ID
}

// censusPlans maps every table pattern to its driving recipe. A table row
// without a plan fails the census.
var censusPlans = map[string]censusPlan{
	// The redirect and the participant spine.
	"GET /":                      {},
	"GET /user":                  {},
	"POST /user/join":            {want: 303, form: staticForm(url.Values{"name": {"Census User"}, "github": {"censususer"}})},
	"GET /user/onboard/{id}":     {who: "iv"},
	"GET /user/pipeline/{id}":    {who: "iv"},
	"POST /user/answer/{id}":     {who: "iv", want: 200, form: staticForm(url.Values{"answer": {"Tabs"}})},
	"GET /user/wait/{id}":        {},
	"GET /user/wait-status/{id}": {},
	"GET /user/persona/{id}":     {},

	// The match page and its swapped cards.
	"GET /user/phase-header/{id}":  {phase: PhaseSocial},
	"GET /user/phase-content/{id}": {phase: PhaseSocial},
	"GET /user/match/{id}":         {phase: PhaseSocial},
	"GET /user/dossier/{id}":       {},
	"GET /user/mission/{id}":       {phase: PhaseSocial, stage: stageMission},

	// The meet handshake.
	"GET /user/m/{id}": {who: "beta"},
	"POST /user/met/{id}": {phase: PhaseSocial, want: 200, cookie: "alpha",
		form: func(f *censusFixture) url.Values { return url.Values{"code": {f.beta.MeetCode}} }},
	"GET /user/meet-status/{id}":  {phase: PhaseSocial},
	"GET /user/match-stream/{id}": {phase: PhaseSocial},

	// Settings, theming and the personal recap.
	"GET /user/settings/{id}": {},
	"POST /user/settings/{id}": {want: 200,
		form: staticForm(url.Values{"email": {"census@example.com"}, "email_on": {"on"}})},
	"POST /user/theme/{id}":     {want: 200, form: staticForm(url.Values{"theme": {"synthwave"}})},
	"GET /user/recap-link/{id}": {},
	"GET /user/recap/{token}":   {},

	// Show & tell, from the audience side.
	"POST /user/showtell/signup/{id}": {phase: PhaseSocial, want: 200,
		form: staticForm(url.Values{"title": {"Census talk"}, "description": {"About testing"}})},
	"GET /user/showtell/{id}": {phase: PhaseSocial},
	"POST /user/showtell/question/{id}": {phase: PhaseShowTell, want: 200, stage: stagePresenterQuestion,
		form: staticForm(url.Values{"question": {"Why test?"}})},

	// Slide decks and the board, from the audience side.
	"GET /user/slides/{id}":        {},
	"GET /user/board/{id}":         {},
	"POST /user/board/{id}":        {want: 200, form: staticForm(url.Values{"text": {"census board post"}})},
	"POST /user/board/{id}/delete": {want: 200, form: idForm("post", func(f *censusFixture) int64 { return f.post }, nil)},

	// The personal pages' streams.
	"GET /user/pipeline-stream/{id}": {who: "iv"},
	"GET /user/wait-stream/{id}":     {},

	// Public: slide decks, the event recap and the join QR.
	"GET /slides/{id}":          {path: "/slides/{deck}"},
	"GET /slides/{id}/download": {path: "/slides/{deck}/download"},
	"GET /recap":                {phase: PhaseClosed},
	"GET /qr":                   {query: "data=/user&size=256"},

	// The big screen.
	"GET /bigscreen":            {},
	"GET /bigscreen/graph-data": {},
	"GET /bigscreen/stream":     {},

	// Debug data.
	"GET /data":                  {},
	"GET /data/participants":     {},
	"GET /data/participant/{id}": {},
	"DELETE /data/participant/{id}": {who: "g1", want: 200,
		raw: func(t *testing.T, f *censusFixture) *http.Request {
			req, err := http.NewRequest("DELETE", f.srv.URL+"/data/participant/g1", nil)
			if err != nil {
				t.Fatal(err)
			}
			return req
		}},
	"GET /data/activity": {},
	"GET /data/state":    {},
	"GET /data/meetings": {phase: PhaseSocial, stage: func(t *testing.T, f *censusFixture) {
		if _, _, err := f.deps.handler.meetings.Confirm(f.alpha, f.beta, OriginOther); err != nil {
			t.Fatal(err)
		}
	}},
	"GET /data/leaderboard":      {},
	"GET /data/channels":         {},
	"GET /data/missioncard/{id}": {phase: PhaseSocial, stage: stageMission},

	// The host console.
	"GET /admin":          {},
	"POST /admin/phase":   {want: 303, form: staticForm(url.Values{"to": {"social"}})},
	"POST /admin/reveal":  {want: 303, form: staticForm(url.Values{})},
	"POST /admin/reset":   {want: 303, form: staticForm(url.Values{})},
	"POST /admin/rematch": {want: 303, form: staticForm(url.Values{})},
	"POST /admin/showtell/stage": {phase: PhaseShowTell, want: 303, stage: stageQueuedSignup,
		form: idForm("id", func(f *censusFixture) int64 { return f.signup }, url.Values{"action": {"stage"}})},
	"POST /admin/showtell/move": {phase: PhaseShowTell, want: 303, stage: stageQueuedSignup,
		form: idForm("id", func(f *censusFixture) int64 { return f.signup }, url.Values{"dir": {"up"}})},
	"POST /admin/showtell/remove": {phase: PhaseShowTell, want: 303, stage: stageQueuedSignup,
		form: idForm("id", func(f *censusFixture) int64 { return f.signup }, nil)},
	"POST /admin/showtell/question": {phase: PhaseShowTell, want: 303, stage: stagePresenterQuestion,
		form: idForm("id", func(f *censusFixture) int64 { return f.question }, url.Values{"action": {"answered"}})},
	"GET /admin/showtell-queue":     {phase: PhaseShowTell, stage: stageQueuedSignup},
	"GET /admin/showtell-questions": {phase: PhaseShowTell, stage: stagePresenterQuestion},
	"GET /admin/showtell-stream": {phase: PhaseShowTell, stage: stageQueuedSignup, nudge: func(t *testing.T, f *censusFixture, s *liveStream) {
		// The queue stream has no connect-time sync: wait until it has
		// captured its opening signature, then change the queue.
		eventually(t, "the stream's first look", func() bool { return f.srv.streamChecks(s.path) >= 1 })
		if err := f.deps.showtell.Signup(reload(t, f.deps.db, "g2"), "Another talk", "Also about testing", ""); err != nil {
			t.Fatal(err)
		}
	}},
	"GET /admin/export": {query: "format=md"},
	"GET /admin/slides": {},
	"POST /admin/slides": {want: 303, raw: func(t *testing.T, f *censusFixture) *http.Request {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		_ = mw.WriteField("title", "Census deck")   // into a bytes.Buffer: cannot fail
		_ = mw.WriteField("speaker", "The Speaker") // into a bytes.Buffer: cannot fail
		part, err := mw.CreateFormFile("file", "deck.pdf")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(samplePDF) // into a bytes.Buffer: cannot fail
		_ = mw.Close()               // into a bytes.Buffer: cannot fail
		req, err := http.NewRequest("POST", f.srv.URL+"/admin/slides", &buf)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", mw.FormDataContentType())
		return req
	}},
	"POST /admin/slides/remove": {want: 303, form: idForm("id", func(f *censusFixture) int64 { return f.deck }, nil)},
	"POST /admin/board": {want: 303,
		form: idForm("id", func(f *censusFixture) int64 { return f.post }, url.Values{"action": {"approved"}})},
	"POST /admin/public-theme": {want: 303, form: staticForm(url.Values{"theme": {"synthwave"}})},
}

// ── driving helpers ────────────────────────────────────────────────────────

// path resolves the plan's request path: the pattern's path with {id}, {token}
// and {deck} filled from the fixture, or the plan's override.
func (f *censusFixture) path(rt route, p censusPlan) string {
	path := p.path
	if path == "" {
		_, path, _ = strings.Cut(rt.Pattern, " ")
		who := p.who
		if who == "" {
			who = "alpha"
		}
		path = strings.NewReplacer(
			"{id}", who,
			"{token}", f.token,
			"{deck}", strconv.FormatInt(f.deck, 10),
		).Replace(path)
	} else {
		path = strings.ReplaceAll(path, "{deck}", strconv.FormatInt(f.deck, 10))
	}
	if p.query != "" {
		path += "?" + p.query
	}
	return path
}

func (f *censusFixture) request(t *testing.T, rt route, p censusPlan) *http.Request {
	t.Helper()
	if p.raw != nil {
		return p.raw(t, f)
	}
	method, _, _ := strings.Cut(rt.Pattern, " ")
	var body io.Reader
	var form url.Values
	if p.form != nil {
		form = p.form(f)
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, f.srv.URL+f.path(rt, p), body)
	if err != nil {
		t.Fatal(err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if p.cookie != "" {
		req.AddCookie(&http.Cookie{Name: cookieName, Value: p.cookie}) //nolint:gosec // G124: an outgoing test request, no browser and no attributes to set
	}
	return req
}

// do runs one census plan's request; the body is closed through the
// t.Cleanup below.
//
//bodyclose:handled
func (f *censusFixture) do(t *testing.T, rt route, p censusPlan) *http.Response {
	t.Helper()
	client := f.srv.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(f.request(t, rt, p))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// ── the census ─────────────────────────────────────────────────────────────

func TestRouteCensus(t *testing.T) {
	for _, rt := range routes {
		t.Run(rt.Pattern, func(t *testing.T) {
			plan, ok := censusPlans[rt.Pattern]
			if !ok {
				t.Fatalf("no census plan for %s — every table row needs one", rt.Pattern)
			}
			f := newCensusFixture(t)
			if plan.phase != "" {
				if err := f.deps.events.Advance(plan.phase); err != nil {
					t.Fatal(err)
				}
			}
			if plan.stage != nil {
				plan.stage(t, f)
			}

			switch rt.Kind {
			case kindSSE:
				s := open(t, f.srv, f.path(rt, plan))
				if plan.nudge != nil {
					plan.nudge(t, f, s)
				}
				waitFor(t, s, rt.Marker)
				if ct := s.Header().Get("Content-Type"); ct != "text/event-stream" {
					t.Errorf("Content-Type: want text/event-stream, got %q", ct)
				}

			case kindRedirect:
				resp := f.do(t, rt, plan)
				if resp.StatusCode != http.StatusFound {
					t.Errorf("want 302, got %d", resp.StatusCode)
				}
				if loc := resp.Header.Get("Location"); loc != rt.Marker {
					t.Errorf("Location: want %q, got %q", rt.Marker, loc)
				}
				// The not-found guard: any other path under the pattern
				// stays a 404 instead of the redirect.
				guard := f.do(t, rt, censusPlan{path: "/not-a-route"})
				if guard.StatusCode != http.StatusNotFound {
					t.Errorf("guard: want 404 for an unmatched path, got %d", guard.StatusCode)
				}

			case kindAction:
				resp := f.do(t, rt, plan)
				if resp.StatusCode != plan.want {
					t.Errorf("want %d, got %d (Location %q)", plan.want, resp.StatusCode, resp.Header.Get("Location"))
					return
				}
				switch {
				case rt.Marker == "" || rt.Marker == "200":
					// Status-only action (DELETE): nothing rendered.
				case resp.StatusCode/100 == 3:
					if loc := resp.Header.Get("Location"); !strings.HasPrefix(loc, rt.Marker) {
						t.Errorf("Location: want prefix %q, got %q", rt.Marker, loc)
					}
				case rt.Marker == "HX-Redirect":
					if resp.Header.Get("HX-Redirect") == "" {
						t.Error("want the HX-Redirect header set")
					}
				default:
					if body := readBody(t, resp); !strings.Contains(body, rt.Marker) {
						t.Errorf("marker %q not in the response body", rt.Marker)
					}
				}

			case kindJSON:
				resp := f.do(t, rt, plan)
				if resp.StatusCode != 200 {
					t.Errorf("want 200, got %d", resp.StatusCode)
					return
				}
				if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
					t.Errorf("Content-Type: want application/json, got %q", ct)
				}
				assertJSONShape(t, rt, readBody(t, resp))

			case kindBinary, kindMarkdown:
				resp := f.do(t, rt, plan)
				if resp.StatusCode != 200 {
					t.Errorf("want 200, got %d", resp.StatusCode)
					return
				}
				if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, rt.Marker) {
					t.Errorf("Content-Type: want prefix %q, got %q", rt.Marker, ct)
				}
				if n := len(readBody(t, resp)); rt.Kind == kindBinary && n < 256 {
					t.Errorf("binary body: want at least 256 bytes, got %d", n)
				}

			default: // page, fragment
				resp := f.do(t, rt, plan)
				if resp.StatusCode != 200 {
					t.Errorf("want 200, got %d", resp.StatusCode)
					return
				}
				if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
					t.Errorf("Content-Type: want text/html, got %q", ct)
				}
				if body := readBody(t, resp); !strings.Contains(body, rt.Marker) {
					t.Errorf("marker %q not in the response body", rt.Marker)
				}
			}
		})
	}
}

// assertJSONShape checks the route's top-level JSON shape against its marker:
// "[]" wants a list; anything else wants an object carrying each named key.
func assertJSONShape(t *testing.T, rt route, body string) {
	t.Helper()
	var top any
	if err := json.Unmarshal([]byte(body), &top); err != nil {
		t.Fatalf("%s: body is not JSON: %v", rt.Pattern, err)
	}
	if rt.Marker == "[]" {
		if _, ok := top.([]any); !ok {
			t.Errorf("%s: want a JSON list, got %T", rt.Pattern, top)
		}
		return
	}
	obj, ok := top.(map[string]any)
	if !ok {
		t.Fatalf("%s: want a JSON object, got %T", rt.Pattern, top)
	}
	for _, key := range strings.Split(rt.Marker, ",") {
		if _, ok := obj[key]; !ok {
			t.Errorf("%s: want key %q among %v", rt.Pattern, key, keysOf(obj))
		}
	}
}

func keysOf(obj map[string]any) []string {
	out := make([]string, 0, len(obj))
	for k := range obj {
		out = append(out, k)
	}
	return out
}

// ── the maintenance rules ──────────────────────────────────────────────────

// TestRouteTable_EveryRowIsComplete is ADR-0022's maintenance rule: no route
// lands half-specified — every row carries a well-formed pattern, a known
// kind, a marker and a journey, and no pattern is registered twice.
func TestRouteTable_EveryRowIsComplete(t *testing.T) {
	kinds := map[routeKind]bool{
		kindPage: true, kindFragment: true, kindJSON: true, kindSSE: true,
		kindBinary: true, kindMarkdown: true, kindRedirect: true, kindAction: true,
	}
	seen := map[string]bool{}
	for _, rt := range routes {
		method, path, ok := strings.Cut(rt.Pattern, " ")
		if !ok || path == "" || !strings.HasPrefix(path, "/") {
			t.Errorf("%q: pattern must be 'METHOD /path'", rt.Pattern)
		}
		switch method {
		case "GET", "POST", "DELETE", "PUT", "PATCH":
		default:
			t.Errorf("%q: unknown method %q", rt.Pattern, method)
		}
		if rt.Handler == nil {
			t.Errorf("%s: no handler", rt.Pattern)
		}
		if !kinds[rt.Kind] {
			t.Errorf("%s: unknown kind %q", rt.Pattern, rt.Kind)
		}
		if rt.Marker == "" {
			t.Errorf("%s: no marker", rt.Pattern)
		}
		if !strings.HasPrefix(rt.Journey, "J") || len(rt.Journey) < 2 {
			t.Errorf("%s: no journey (want J1..J13), got %q", rt.Pattern, rt.Journey)
		} else if n, err := strconv.Atoi(strings.TrimPrefix(rt.Journey, "J")); err != nil || n < 1 || n > 13 {
			t.Errorf("%s: journey out of range (want J1..J13): %q", rt.Pattern, rt.Journey)
		}
		if seen[rt.Pattern] {
			t.Errorf("%s: registered twice in the table", rt.Pattern)
		}
		seen[rt.Pattern] = true
	}
}

// TestRouteTable_TheMuxRegistersExactlyTheTable is the structural half of the
// census: buildMux must register exactly the table's patterns — a route
// registered outside the table fails here.
func TestRouteTable_TheMuxRegistersExactlyTheTable(t *testing.T) {
	srv, _ := newTestServer(t, nil, nil)
	mux, ok := srv.h.(*http.ServeMux)
	if !ok {
		t.Fatalf("buildMux returned %T, not *http.ServeMux", srv.h)
	}
	registered := registeredPatterns(t, mux)
	want := map[string]bool{}
	for _, rt := range routes {
		want[rt.Pattern] = true
	}
	for _, pattern := range registered {
		if !want[pattern] {
			t.Errorf("registered outside the table: %s", pattern)
		}
	}
	got := map[string]bool{}
	for _, pattern := range registered {
		got[pattern] = true
	}
	for _, rt := range routes {
		if !got[rt.Pattern] {
			t.Errorf("in the table but never registered: %s", rt.Pattern)
		}
	}
	if len(registered) != len(routes) {
		t.Errorf("want %d registered routes, got %d", len(routes), len(registered))
	}
}

// registeredPatterns enumerates the patterns a ServeMux has registered.
// net/http has no public API for this as of Go 1.26, so this reads the
// ServeMux's internal routing tree and index (Go 1.26 layout); the Fatalf
// below fires loudly the day the internals move and this needs rewriting.
func registeredPatterns(t *testing.T, mux *http.ServeMux) []string {
	t.Helper()
	v := reflect.ValueOf(mux).Elem()
	seen := map[string]bool{}
	collect := func(p reflect.Value) {
		if !p.IsValid() || p.IsZero() {
			return
		}
		seen[p.Elem().FieldByName("str").String()] = true
	}

	index := v.FieldByName("index")
	if !index.IsValid() {
		t.Fatalf("net/http.ServeMux internals changed: no index — update registeredPatterns")
	}
	add := func(field string) {
		f := index.FieldByName(field)
		switch f.Kind() {
		case reflect.Map:
			for _, k := range f.MapKeys() {
				bucket := f.MapIndex(k)
				for i := 0; i < bucket.Len(); i++ {
					collect(bucket.Index(i))
				}
			}
		case reflect.Slice:
			for i := 0; i < f.Len(); i++ {
				collect(f.Index(i))
			}
		default:
			t.Fatalf("net/http.ServeMux internals changed: index field %q is %s — update registeredPatterns", field, f.Kind())
		}
	}
	add("segments")
	add("multis")

	// The routing tree carries the patterns the index buckets may not (the
	// wildcard children of paths like "GET /").
	node := v.FieldByName("tree")
	if !node.IsValid() {
		t.Fatalf("net/http.ServeMux internals changed: no tree — update registeredPatterns")
	}
	var walk func(reflect.Value)
	walk = func(n reflect.Value) {
		if n.Kind() == reflect.Pointer {
			n = n.Elem()
		}
		if !n.IsValid() || n.IsZero() {
			return
		}
		collect(n.FieldByName("pattern"))
		children := n.FieldByName("children")
		if children.IsValid() {
			for _, k := range children.FieldByName("m").MapKeys() {
				walk(children.FieldByName("m").MapIndex(k))
			}
			entries := children.FieldByName("s")
			for i := 0; i < entries.Len(); i++ {
				walk(entries.Index(i).FieldByName("value"))
			}
		}
		walk(n.FieldByName("multiChild"))
		walk(n.FieldByName("emptyChild"))
	}
	walk(node)

	out := make([]string, 0, len(seen))
	for pattern := range seen {
		out = append(out, pattern)
	}
	return out
}

// TestRouteTable_EveryRowHasACensusPlan guards the pairing of the table and
// the census: a route added to the table without a plan here cannot be
// existence-checked.
func TestRouteTable_EveryRowHasACensusPlan(t *testing.T) {
	for _, rt := range routes {
		if _, ok := censusPlans[rt.Pattern]; !ok {
			t.Errorf("%s: no census plan", rt.Pattern)
		}
	}
	if len(censusPlans) != len(routes) {
		t.Errorf("census plan for a route that is not in the table: %d plans, %d routes", len(censusPlans), len(routes))
	}
}
