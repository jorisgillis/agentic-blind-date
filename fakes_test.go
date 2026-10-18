package main

import (
	"context"
	"encoding/json"
	"fmt"
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

// timeoutGitHub is a GitHubAPI whose profile fetch fails with a
// deadline-exceeded error, like the production client when its bounded timeout
// fires (spec #46's fault, injected without touching production code).
type timeoutGitHub struct {
	fakeGitHub
}

func (*timeoutGitHub) FetchProfile(handle string) (*GitHubProfile, error) {
	return nil, fmt.Errorf("Get \"https://api.github.com/users/%s\": %w", handle, context.DeadlineExceeded)
}

// testSrv serves the mux in-process: no port is bound and nothing leaves the process.
type testSrv struct {
	URL string
	h   http.Handler

	mu     sync.Mutex
	checks map[string]int // how often each stream has looked at the current state
}

// streamChecks reports how many times the stream at path has looked at the current state.
func (s *testSrv) streamChecks(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.checks[path]
}

// OnStream records that the stream at path has looked at the current state,
// so tests can wait for a stream to catch up.
func (s *testSrv) OnStream(path string) {
	s.mu.Lock()
	s.checks[path]++
	s.mu.Unlock()
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
	events      *EventStore
	matcher     *Matcher
	missions    *Missions
	showtell    *ShowTell
	slides      *SlideDecks
	llm         *fakeLLM
	github      *fakeGitHub
	onboarding  *Onboarding
	matchmaking *Matchmaking
	handler     *Handler
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
	return newTestServerOnDB(t, newTestDB(t), llm, gh)
}

// newTestServerOnDB wires the test server on an existing database — the
// restart tests reopen the same file and serve it again.
func newTestServerOnDB(t *testing.T, db *DB, llm *fakeLLM, gh *fakeGitHub) (*testSrv, *testDeps) {
	t.Helper()
	if llm == nil {
		llm = newFakeLLM()
	}
	if gh == nil {
		gh = newFakeGitHub()
	}
	matcher := NewMatcher(db, gh, llm)
	missions := NewMissions(db, matcher, llm)
	showtell := NewShowTell(db, llm)
	slides := NewSlideDecks(db, t.TempDir())
	interview := NewInterview(db, llm)
	relations := NewRelationships(db)
	matchmaking := NewMatchmaking(db, matcher, relations, defaultMaxChain)
	onboarding := NewOnboarding(db, gh, interview, NewPersonas(llm), matchmaking, missions)
	srv := &testSrv{URL: "http://test", checks: map[string]int{}}
	h := NewHandler(db, onboarding, interview, matcher, relations, matchmaking, missions, showtell, slides, srv)
	srv.h = buildMux(h)
	return srv, &testDeps{db: db, events: NewEventStore(db), matcher: matcher, missions: missions, showtell: showtell, slides: slides, llm: llm, github: gh, onboarding: onboarding, matchmaking: matchmaking, handler: h}
}

// forceStep puts a Participant at any Pipeline Step, bypassing the guarded
// transitions (test setup only): the store's PipelineStep change only moves
// one step forward, so forcing an arbitrary step is not expressible through
// ParticipantStore.Change.
func forceStep(db *DB, id string, step Step) {
	db.db.Exec(`UPDATE participants SET pipeline_step = ? WHERE id = ?`, step, id)
	db.changed()
}

// forceQuestions sets a Participant's question set directly, bypassing the
// store (test setup only, like forceStep): no production code sets
// Questions on its own outside StartInterview's guarded, combined write.
func forceQuestions(db *DB, id string, questions []Question) {
	encoded, _ := json.Marshal(questions)
	db.db.Exec(`UPDATE participants SET questions = ? WHERE id = ?`, string(encoded), id)
	db.changed()
}

// forceInterview stages a Participant mid-Interview — a question set, the
// answers so far and the Pipeline Step, in one write — bypassing the store
// (test setup only, like forceStep and forceQuestions): no production code
// sets Questions outside StartInterview's guarded, combined write, and the
// store's PipelineStep change only moves one step forward, so a forced
// mid-Interview state is not expressible through ParticipantStore.Change.
func forceInterview(db *DB, id string, questions []Question, answers map[string]string, step Step) {
	questionsJSON, _ := json.Marshal(questions)
	answersJSON, _ := json.Marshal(answers)
	db.db.Exec(`UPDATE participants SET questions = ?, answers_json = ?, pipeline_step = ? WHERE id = ?`,
		string(questionsJSON), string(answersJSON), step, id)
}

// changeParticipant applies one Participant change through the Participant
// store, failing the test when it does not go through: the one wrapper the
// test-setup conveniences below share, now that DB's own per-column setters
// are gone (#53).
func changeParticipant(t *testing.T, db *DB, change ParticipantChange) {
	t.Helper()
	if err := NewParticipantStore(db).Change(change); err != nil {
		t.Fatal(err)
	}
}

// setProfile, setPersona and updateAnswers stage one Participant field each.
func setProfile(t *testing.T, db *DB, id string, profile *GitHubProfile) {
	t.Helper()
	changeParticipant(t, db, ParticipantChange{ID: id, Profile: profile})
}

func setPersona(t *testing.T, db *DB, id, name, tagline string) {
	t.Helper()
	changeParticipant(t, db, ParticipantChange{ID: id, Persona: &Persona{Name: name, Tagline: tagline}})
}

func updateAnswers(t *testing.T, db *DB, id string, answers map[string]string) {
	t.Helper()
	changeParticipant(t, db, ParticipantChange{ID: id, Answers: answers})
}

// withoutProfile corrupts a Participant's profile to JSON 'null', leaving
// them without one. Corruption setup: the store cannot express this — a nil
// Profile in a ParticipantChange means "leave the column alone", and a
// non-nil profile marshals to an object, never 'null'.
func withoutProfile(db *DB, id string) {
	db.db.Exec(`UPDATE participants SET profile_json = 'null' WHERE id = ?`, id)
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
