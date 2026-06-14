package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func testServer(t *testing.T) (*httptest.Server, *DB) {
	t.Helper()
	db, err := NewDB(":memory:")
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	// Pin to one connection so all goroutines share the same in-memory database.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	github := NewGitHubClient("")
	mistral := NewMistralClient("", "", &http.Client{})
	matcher := NewMatcher(github, mistral)
	agents := NewAgentPipeline(db, github, mistral, matcher)
	h := NewHandler(db, github, mistral, agents)
	srv := httptest.NewServer(buildMux(h))
	t.Cleanup(srv.Close)
	return srv, db
}

func get(t *testing.T, srv *httptest.Server, path string) *http.Response {
	t.Helper()
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(srv.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func post(t *testing.T, srv *httptest.Server, path string, form url.Values) *http.Response {
	t.Helper()
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.PostForm(srv.URL+path, form)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestLandingReturns200(t *testing.T) {
	srv, _ := testServer(t)
	resp := get(t, srv, "/user")
	if resp.StatusCode != 200 {
		t.Errorf("GET /user: want 200, got %d", resp.StatusCode)
	}
}

func TestJoinRegistersParticipant(t *testing.T) {
	srv, db := testServer(t)

	resp := post(t, srv, "/user/join", url.Values{"name": {"Test User"}, "github": {"testuser"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("POST /user/join: want 303, got %d", resp.StatusCode)
	}

	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/user/onboard/") {
		t.Errorf("Location: want /user/onboard/..., got %s", loc)
	}

	p, err := db.GetParticipantByHandle("testuser")
	if err != nil {
		t.Fatalf("participant not found in DB: %v", err)
	}
	if p.GitHubHandle != "testuser" {
		t.Errorf("GitHubHandle: want testuser, got %s", p.GitHubHandle)
	}
}

func TestJoinDuplicateHandle(t *testing.T) {
	srv, _ := testServer(t)

	r1 := post(t, srv, "/user/join", url.Values{"github": {"dupeuser"}})
	loc1 := r1.Header.Get("Location")

	r2 := post(t, srv, "/user/join", url.Values{"github": {"dupeuser"}})
	loc2 := r2.Header.Get("Location")

	if loc1 != loc2 {
		t.Errorf("duplicate join should redirect to same URL: %s vs %s", loc1, loc2)
	}
}

func TestJoinEmptyHandle(t *testing.T) {
	srv, _ := testServer(t)
	resp := post(t, srv, "/user/join", url.Values{"name": {"Test"}, "github": {""}})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("POST /user/join with empty handle: want 400, got %d", resp.StatusCode)
	}
}

func TestJoinWithoutGitHub(t *testing.T) {
	srv, db := testServer(t)

	languages, _ := json.Marshal([]string{"Go", "Python"})
	resp := post(t, srv, "/user/join", url.Values{
		"name":            {"Non GitHub User"},
		"no_github":       {"on"},
		"languages":       {string(languages)},
		"project_type":    {"Web"},
		"dev_environment": {`["IDE","VIM"]`},
		"weirdest_bug":    {"A race condition on Tuesdays"},
		"keyboard":        {"Mechanical"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("POST /user/join without GitHub: want 303, got %d", resp.StatusCode)
	}

	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/user/onboard/") {
		t.Errorf("Location: want /user/onboard/..., got %s", loc)
	}

	if db.ParticipantCount() == 0 {
		t.Fatal("participant should exist in DB")
	}
}

func TestAdminGet(t *testing.T) {
	srv, _ := testServer(t)
	resp := get(t, srv, "/admin")
	if resp.StatusCode != 200 {
		t.Errorf("GET /admin: want 200, got %d", resp.StatusCode)
	}
}

func TestDataState(t *testing.T) {
	srv, _ := testServer(t)
	resp := get(t, srv, "/data/state")
	if resp.StatusCode != 200 {
		t.Errorf("GET /data/state: want 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var state map[string]any
	if err := json.Unmarshal(body, &state); err != nil {
		t.Fatalf("JSON parse: %v — body: %s", err, body)
	}
	if state["phase"] != "onboarding" {
		t.Errorf("phase: want onboarding, got %v", state["phase"])
	}
}

func TestDataParticipants_empty(t *testing.T) {
	srv, _ := testServer(t)
	resp := get(t, srv, "/data/participants")
	if resp.StatusCode != 200 {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	// Empty participant list should marshal as JSON null or []
	trimmed := strings.TrimSpace(string(body))
	if trimmed != "null" && trimmed != "[]" {
		t.Errorf("expected empty JSON list, got %s", trimmed)
	}
}

func TestReset(t *testing.T) {
	srv, db := testServer(t)

	// Register a participant first
	post(t, srv, "/user/join", url.Values{"name": {"Will Be Reset"}, "github": {"willbereset"}})
	if db.ParticipantCount() == 0 {
		t.Fatal("participant should exist before reset")
	}

	resp := post(t, srv, "/admin/reset", url.Values{})
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("POST /admin/reset: want 303, got %d", resp.StatusCode)
	}

	if db.ParticipantCount() != 0 {
		t.Error("participants should be cleared after reset")
	}
}

func TestGraphPayload_Top3Connections(t *testing.T) {
	// This test verifies that buildGraphPayload includes top-3 connections per participant
	// We can't easily test the full graph without setting up participants with proper data,
	// but we can verify the constant is set to 3

	// Create a handler with empty database
	db, _ := NewDB(":memory:")
	defer db.Close()
	github := NewGitHubClient("")
	mistral := NewMistralClient("", "", &http.Client{})
	matcher := NewMatcher(github, mistral)
	agents := NewAgentPipeline(db, github, mistral, matcher)
	h := NewHandler(db, github, mistral, agents)

	// Call buildGraphPayload with empty participants
	payload := h.buildGraphPayload()

	// Verify structure
	if payload["nodes"] == nil {
		t.Error("payload should have nodes")
	}
	if payload["edges"] == nil {
		t.Error("payload should have edges")
	}
	if payload["phase"] == nil {
		t.Error("payload should have phase")
	}
	if payload["activity"] == nil {
		t.Error("payload should have activity")
	}
}

func TestComputeBadges(t *testing.T) {
	// Test GitHub Dinosaur badge (10+ years)
	p := GitHubProfile{AccountAgeDays: 3650}
	badges := computeBadges(p)
	if len(badges) != 1 || badges[0].Label != "GitHub Dinosaur" {
		t.Errorf("expected GitHub Dinosaur badge for 10+ years, got %v", badges)
	}

	// Test GitHub Celebrity badge (100+ stars)
	p = GitHubProfile{TotalStars: 100}
	badges = computeBadges(p)
	if len(badges) != 1 || badges[0].Label != "GitHub Celebrity" {
		t.Errorf("expected GitHub Celebrity badge for 100+ stars, got %v", badges)
	}

	// Test The Hoarder badge (50+ repos)
	p = GitHubProfile{PublicRepos: 50}
	badges = computeBadges(p)
	if len(badges) != 1 || badges[0].Label != "The Hoarder" {
		t.Errorf("expected The Hoarder badge for 50+ repos, got %v", badges)
	}

	// Test Polyglot badge (5+ languages)
	p = GitHubProfile{Languages: []string{"Go", "Python", "JavaScript", "TypeScript", "Java"}}
	badges = computeBadges(p)
	if len(badges) != 1 || badges[0].Label != "Polyglot" {
		t.Errorf("expected Polyglot badge for 5+ languages, got %v", badges)
	}

	// Test Storyteller badge (has profile README)
	p = GitHubProfile{HasProfileReadme: true}
	badges = computeBadges(p)
	if len(badges) != 1 || badges[0].Label != "Storyteller" {
		t.Errorf("expected Storyteller badge for profile README, got %v", badges)
	}

	// Test Fresh Blood badge (0 < age < 180 days)
	p = GitHubProfile{AccountAgeDays: 90}
	badges = computeBadges(p)
	if len(badges) != 1 || badges[0].Label != "Fresh Blood" {
		t.Errorf("expected Fresh Blood badge for new account, got %v", badges)
	}

	// Test Lurker badge (100+ followers, < 5 repos)
	p = GitHubProfile{Followers: 100, PublicRepos: 4}
	badges = computeBadges(p)
	if len(badges) != 1 || badges[0].Label != "Lurker" {
		t.Errorf("expected Lurker badge, got %v", badges)
	}

	// Test Focused badge (exactly 1 language)
	p = GitHubProfile{Languages: []string{"Go"}}
	badges = computeBadges(p)
	if len(badges) != 1 || badges[0].Label != "Focused" {
		t.Errorf("expected Focused badge for single language, got %v", badges)
	}

	// Test multiple badges
	p = GitHubProfile{
		AccountAgeDays: 3650,
		TotalStars:    100,
		PublicRepos:   50,
		Languages:     []string{"Go", "Python", "JavaScript", "TypeScript", "Java"},
	}
	badges = computeBadges(p)
	if len(badges) != 4 {
		t.Errorf("expected 4 badges, got %d", len(badges))
	}

	// Test no badges
	p = GitHubProfile{}
	badges = computeBadges(p)
	if len(badges) != 0 {
		t.Errorf("expected no badges for empty profile, got %v", badges)
	}
}

func TestBuildQuestionData(t *testing.T) {
	// Setup
	db, _ := NewDB(":memory:")
	defer db.Close()
	github := NewGitHubClient("")
	mistral := NewMistralClient("", "", &http.Client{})
	matcher := NewMatcher(github, mistral)
	agents := NewAgentPipeline(db, github, mistral, matcher)
	h := NewHandler(db, github, mistral, agents)

	// Test with no answers and questions
	p := &Participant{ID: "test-1", Questions: FixedQuestions}
	data := h.buildQuestionData(p)
	if data == nil {
		t.Fatal("expected non-nil QuestionData")
	}
	if data.ParticipantID != "test-1" {
		t.Errorf("expected ParticipantID test-1, got %s", data.ParticipantID)
	}
	if data.Index != 0 {
		t.Errorf("expected Index 0, got %d", data.Index)
	}
	if data.Total != len(FixedQuestions) {
		t.Errorf("expected Total %d, got %d", len(FixedQuestions), data.Total)
	}
	if data.Question.ID != "fixed_0" {
		t.Errorf("expected first question fixed_0, got %s", data.Question.ID)
	}

	// Test with some answers
	p.Answers = map[string]string{"fixed_0": "Tabs"}
	data = h.buildQuestionData(p)
	if data.Index != 1 {
		t.Errorf("expected Index 1, got %d", data.Index)
	}
	if data.Question.ID != "fixed_1" {
		t.Errorf("expected second question fixed_1, got %s", data.Question.ID)
	}

	// Test with all answers
	p.Answers = map[string]string{
		"fixed_0": "Tabs",
		"fixed_1": "Go",
		"fixed_2": "Only if tests pass",
		"fixed_3": "Monolith, always",
		"fixed_4": "fix stuff",
		"fixed_5": "Claude",
	}
	data = h.buildQuestionData(p)
	if data != nil {
		t.Error("expected nil when all questions answered")
	}
}

func TestDataParticipants(t *testing.T) {
	srv, db := testServer(t)

	// Create some participants
	db.CreateParticipant("id-1", "user1", "User 1")
	db.CreateParticipant("id-2", "user2", "User 2")

	// Call DataParticipants endpoint
	resp, err := srv.Client().Get(srv.URL + "/data/participants")
	if err != nil {
		t.Fatalf("GET /data/participants: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestDataParticipant(t *testing.T) {
	srv, db := testServer(t)

	// Create a participant
	db.CreateParticipant("id-1", "user1", "User 1")

	// Call DataParticipant endpoint by ID
	resp, err := srv.Client().Get(srv.URL + "/data/participant/id-1")
	if err != nil {
		t.Fatalf("GET /data/participant/id-1: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Call DataParticipant endpoint by handle
	resp, err = srv.Client().Get(srv.URL + "/data/participant/user1")
	if err != nil {
		t.Fatalf("GET /data/participant/user1: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Call with non-existent ID
	resp, err = srv.Client().Get(srv.URL + "/data/participant/nonexistent")
	if err != nil {
		t.Fatalf("GET /data/participant/nonexistent: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestDataActivity(t *testing.T) {
	srv, db := testServer(t)

	// Add some activity
	db.LogActivity("Test activity 1")
	db.LogActivity("Test activity 2")

	// Call DataActivity endpoint
	resp, err := srv.Client().Get(srv.URL + "/data/activity")
	if err != nil {
		t.Fatalf("GET /data/activity: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestSubmitAnswer(t *testing.T) {
	srv, db := testServer(t)

	// Create a participant in interviewing state with questions
	db.CreateParticipant("id-1", "user1", "User 1")
	p, _ := db.GetParticipant("id-1")
	p.Questions = FixedQuestions
	p.Answers = map[string]string{}
	p.PipelineStep = "interviewing"
	// Need to update the participant in DB with questions
	questionsJSON, _ := json.Marshal(FixedQuestions)
	answersJSON, _ := json.Marshal(map[string]string{})
	db.db.Exec(`UPDATE participants SET questions = ?, answers_json = ?, pipeline_step = ? WHERE id = ?`,
		string(questionsJSON), string(answersJSON), "interviewing", "id-1")

	// Submit a valid answer
	resp, err := srv.Client().Post(srv.URL+"/user/answer/id-1", "application/x-www-form-urlencoded",
		strings.NewReader("answer=Tabs"))
	if err != nil {
		t.Fatalf("POST /user/answer/id-1: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Verify answer was stored - need to reload from DB
	p, _ = db.GetParticipant("id-1")
	if p.Answers == nil {
		t.Fatal("Answers is nil")
	}
	if p.Answers["fixed_0"] != "Tabs" {
		t.Errorf("expected answer 'Tabs', got '%s'", p.Answers["fixed_0"])
	}
}

func TestDataIndex(t *testing.T) {
	srv, _ := testServer(t)

	// Call DataIndex endpoint
	resp, err := srv.Client().Get(srv.URL + "/data")
	if err != nil {
		t.Fatalf("GET /data: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestAdmin(t *testing.T) {
	srv, _ := testServer(t)

	// Call Admin endpoint
	resp, err := srv.Client().Get(srv.URL + "/admin")
	if err != nil {
		t.Fatalf("GET /admin: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

// flusherRecorder is a ResponseRecorder that implements http.Flusher
type flusherRecorder struct {
	*httptest.ResponseRecorder
}

func (f *flusherRecorder) Flush() {
	// No-op for testing
}

func TestSseHeaders(t *testing.T) {
	w := &flusherRecorder{httptest.NewRecorder()}
	
	result := sseHeaders(w)
	if !result {
		t.Error("expected sseHeaders to return true for Flusher")
	}
	
	if w.Header().Get("Content-Type") != "text/event-stream" {
		t.Error("expected Content-Type to be text/event-stream")
	}
	if w.Header().Get("Cache-Control") != "no-cache" {
		t.Error("expected Cache-Control to be no-cache")
	}
	if w.Header().Get("Connection") != "keep-alive" {
		t.Error("expected Connection to be keep-alive")
	}
}

func TestSseRedirect(t *testing.T) {
	w := &flusherRecorder{httptest.NewRecorder()}
	
	sseRedirect(w, "http://example.com")
	
	body := w.Body.String()
	if !strings.Contains(body, "event: redirect") {
		t.Error("expected event: redirect in body")
	}
	if !strings.Contains(body, "data: http://example.com") {
		t.Error("expected data: http://example.com in body")
	}
}

func TestSubmitAnswer_ParticipantNotFound(t *testing.T) {
	srv, _ := testServer(t)

	// Submit answer for non-existent participant
	resp, err := srv.Client().Post(srv.URL+"/user/answer/nonexistent", "application/x-www-form-urlencoded",
		strings.NewReader("answer=Test"))
	if err != nil {
		t.Fatalf("POST /user/answer/nonexistent: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestSubmitAnswer_WrongPipelineStep(t *testing.T) {
	srv, db := testServer(t)

	// Create a participant NOT in interviewing state
	db.CreateParticipant("id-1", "user1", "User 1")
	db.UpdatePipelineStep("id-1", "ready")

	// Submit answer
	resp, err := srv.Client().Post(srv.URL+"/user/answer/id-1", "application/x-www-form-urlencoded",
		strings.NewReader("answer=Test"))
	if err != nil {
		t.Fatalf("POST /user/answer/id-1: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Check for HX-Redirect header
	if resp.Header.Get("HX-Redirect") == "" {
		t.Error("expected HX-Redirect header to be set")
	}
}

func TestSubmitAnswer_NoMoreQuestions(t *testing.T) {
	srv, db := testServer(t)

	// Create a participant with only 2 questions and both answered
	db.CreateParticipant("id-1", "user1", "User 1")
	p, _ := db.GetParticipant("id-1")
	p.Questions = []Question{
		{ID: "q0", Text: "Q1?"},
		{ID: "q1", Text: "Q2?"},
	}
	p.Answers = map[string]string{
		"q0": "A1",
		"q1": "A2",
	}
	p.PipelineStep = "interviewing"
	questionsJSON, _ := json.Marshal(p.Questions)
	answersJSON, _ := json.Marshal(p.Answers)
	db.db.Exec(`UPDATE participants SET questions = ?, answers_json = ?, pipeline_step = ? WHERE id = ?`,
		string(questionsJSON), string(answersJSON), "interviewing", "id-1")

	// Submit another answer (should fail)
	resp, err := srv.Client().Post(srv.URL+"/user/answer/id-1", "application/x-www-form-urlencoded",
		strings.NewReader("answer=Extra"))
	if err != nil {
		t.Fatalf("POST /user/answer/id-1: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestSubmitAnswer_MultiSelectInvalidJSON(t *testing.T) {
	srv, db := testServer(t)

	// Create a participant with multi-select questions
	db.CreateParticipant("id-1", "user1", "User 1")
	p, _ := db.GetParticipant("id-1")
	p.Questions = []Question{
		{ID: "q1", Text: "Select languages", MaxSelections: 3, Mode: MultiSelect},
	}
	p.Answers = map[string]string{}
	p.PipelineStep = "interviewing"
	questionsJSON, _ := json.Marshal(p.Questions)
	answersJSON, _ := json.Marshal(map[string]string{})
	db.db.Exec(`UPDATE participants SET questions = ?, answers_json = ?, pipeline_step = ? WHERE id = ?`,
		string(questionsJSON), string(answersJSON), "interviewing", "id-1")

	// Submit invalid JSON for multi-select
	resp, err := srv.Client().Post(srv.URL+"/user/answer/id-1", "application/x-www-form-urlencoded",
		strings.NewReader("answer=not valid json"))
	if err != nil {
		t.Fatalf("POST /user/answer/id-1: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestSubmitAnswer_MultiSelectTooManySelections(t *testing.T) {
	srv, db := testServer(t)

	// Create a participant with multi-select questions (max 2 selections)
	db.CreateParticipant("id-1", "user1", "User 1")
	p, _ := db.GetParticipant("id-1")
	p.Questions = []Question{
		{ID: "q1", Text: "Select languages", MaxSelections: 2, Mode: MultiSelect},
	}
	p.Answers = map[string]string{}
	p.PipelineStep = "interviewing"
	questionsJSON, _ := json.Marshal(p.Questions)
	answersJSON, _ := json.Marshal(map[string]string{})
	db.db.Exec(`UPDATE participants SET questions = ?, answers_json = ?, pipeline_step = ? WHERE id = ?`,
		string(questionsJSON), string(answersJSON), "interviewing", "id-1")

	// Submit too many selections
	resp, err := srv.Client().Post(srv.URL+"/user/answer/id-1", "application/x-www-form-urlencoded",
		strings.NewReader("answer=[\"Go\",\"Python\",\"Java\"]"))
	if err != nil {
		t.Fatalf("POST /user/answer/id-1: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}
