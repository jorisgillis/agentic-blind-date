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
	db, err := initDB(":memory:")
	if err != nil {
		t.Fatalf("initDB: %v", err)
	}
	// Pin to one connection so all goroutines share the same in-memory database.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	h := newHandler(db, &GitHubClient{}, &MistralClient{})
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

	resp := post(t, srv, "/user/join", url.Values{"github": {"testuser"}})
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
	resp := post(t, srv, "/user/join", url.Values{"github": {""}})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("empty handle: want 400, got %d", resp.StatusCode)
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
	post(t, srv, "/user/join", url.Values{"github": {"willbereset"}})
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
