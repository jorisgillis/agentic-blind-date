package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeLLM is an in-memory LLM adapter. Replies are routed by a substring of the
// system prompt, so one fake can serve question, persona and match prompts.
type fakeLLM struct {
	mu      sync.Mutex
	routes  []llmRoute
	delay   time.Duration
	calls   []llmCall
	fallErr error
}

type llmRoute struct {
	systemContains string
	reply          func(user string) (string, error)
}

type llmCall struct {
	System string
	User   string
}

func newFakeLLM() *fakeLLM { return &fakeLLM{} }

// on registers a canned reply for prompts whose system message contains s.
func (f *fakeLLM) on(s, reply string) *fakeLLM {
	return f.onFunc(s, func(string) (string, error) { return reply, nil })
}

// onErr makes prompts whose system message contains s fail with err.
func (f *fakeLLM) onErr(s string, err error) *fakeLLM {
	return f.onFunc(s, func(string) (string, error) { return "", err })
}

func (f *fakeLLM) onFunc(s string, reply func(user string) (string, error)) *fakeLLM {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes = append(f.routes, llmRoute{s, reply})
	return f
}

func (f *fakeLLM) Chat(system, user string) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, llmCall{system, user})
	routes := append([]llmRoute(nil), f.routes...)
	delay := f.delay
	f.mu.Unlock()

	if delay > 0 {
		time.Sleep(delay)
	}
	for _, r := range routes {
		if strings.Contains(system, r.systemContains) {
			return r.reply(user)
		}
	}
	if f.fallErr != nil {
		return "", f.fallErr
	}
	return "", errUnscripted
}

// callsMatching counts calls whose system prompt contains s.
func (f *fakeLLM) callsMatching(s string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.Contains(c.System, s) {
			n++
		}
	}
	return n
}

// lastCallMatching returns the most recent call whose system prompt contains s.
func (f *fakeLLM) lastCallMatching(s string) (llmCall, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.calls) - 1; i >= 0; i-- {
		if strings.Contains(f.calls[i].System, s) {
			return f.calls[i], true
		}
	}
	return llmCall{}, false
}

type fakeError string

func (e fakeError) Error() string { return string(e) }

const errUnscripted = fakeError("fake LLM: no scripted reply")

// fakeGitHub is an in-memory GitHubAPI adapter.
type fakeGitHub struct {
	mu       sync.Mutex
	profiles map[string]*GitHubProfile
	follows  map[[2]string]bool
	lookups  []string
}

func newFakeGitHub() *fakeGitHub {
	return &fakeGitHub{profiles: map[string]*GitHubProfile{}, follows: map[[2]string]bool{}}
}

func (g *fakeGitHub) withProfile(p *GitHubProfile) *fakeGitHub {
	g.profiles[p.Login] = p
	return g
}

func (g *fakeGitHub) withFollow(follower, followee string) *fakeGitHub {
	g.follows[[2]string{follower, followee}] = true
	return g
}

func (g *fakeGitHub) FetchProfile(handle string) (*GitHubProfile, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	p, ok := g.profiles[handle]
	if !ok {
		return nil, fakeError("GitHub user not found: " + handle)
	}
	cp := *p
	return &cp, nil
}

func (g *fakeGitHub) CheckMutualFollow(a, b string) (bool, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.lookups = append(g.lookups, a+"→"+b)
	return g.follows[[2]string{a, b}], g.follows[[2]string{b, a}]
}

func (g *fakeGitHub) followLookups() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.lookups)
}

// testSrv serves the mux in-process: no port is bound and nothing leaves the process.
type testSrv struct {
	URL string
	h   http.Handler
}

func (s *testSrv) Client() *http.Client {
	return &http.Client{Transport: handlerTransport{s.h}}
}

type handlerTransport struct{ h http.Handler }

func (t handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body == nil {
		req.Body = http.NoBody
	}
	rec := httptest.NewRecorder()
	t.h.ServeHTTP(rec, req)
	return rec.Result(), nil
}

// testDeps bundles the fakes behind a test server so tests can script and inspect them.
type testDeps struct {
	db          *DB
	llm         *fakeLLM
	github      *fakeGitHub
	onboarding  *Onboarding
	matchmaking *Matchmaking
}

func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := NewDB(":memory:")
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	// Pin to one connection so all goroutines share the same in-memory database.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func newTestServer(t *testing.T, llm *fakeLLM, gh *fakeGitHub) (*testSrv, *testDeps) {
	t.Helper()
	if llm == nil {
		llm = newFakeLLM()
	}
	if gh == nil {
		gh = newFakeGitHub()
	}
	db := newTestDB(t)
	matcher := NewMatcher(db, gh, llm)
	interview := NewInterview(db, llm)
	relations := NewRelationships(db)
	matchmaking := NewMatchmaking(db, matcher, relations)
	onboarding := NewOnboarding(db, gh, interview, NewPersonas(llm), matchmaking)
	h := NewHandler(db, onboarding, interview, matcher, relations, matchmaking)
	return &testSrv{URL: "http://test", h: buildMux(h)}, &testDeps{db: db, llm: llm, github: gh, onboarding: onboarding, matchmaking: matchmaking}
}

// forceStep puts a Participant at any Pipeline Step, bypassing the guarded transitions (test setup only).
func forceStep(db *DB, id string, step Step) {
	db.db.Exec(`UPDATE participants SET pipeline_step = ? WHERE id = ?`, step, id)
	db.changed()
}

// eventually polls cond until it holds or the deadline passes.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", what)
}
