package main

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandler_SetBigScreenThemeReportsAGenuineFailure(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	breakDB(deps.db)

	resp := post(t, srv, "/admin/bigscreen-theme", url.Values{"theme": {"neon"}})

	if resp.StatusCode != 500 {
		t.Errorf("want 500, got %d", resp.StatusCode)
	}
}

func TestDB_BigScreenThemeDefaultsToClassic(t *testing.T) {
	db := newTestDB(t)
	if got := db.BigScreenTheme().Key; got != DefaultThemeKey {
		t.Errorf("want the default Theme, got %q", got)
	}
}

func TestDB_BigScreenThemeFallsBackToDefaultForAnUnknownKey(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetBigScreenTheme("not-a-real-theme"); err != nil {
		t.Fatal(err)
	}
	if got := db.BigScreenTheme().Key; got != DefaultThemeKey {
		t.Errorf("want the default Theme for an unknown key, got %q", got)
	}
}

func TestDB_BigScreenThemeSurvivesReopeningTheDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := NewDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetBigScreenTheme("neon"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	reopened, err := NewDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got := reopened.BigScreenTheme().Key; got != "neon" {
		t.Errorf("want the choice to survive reopening, got %q", got)
	}
}

func TestDB_BigScreenThemeIsUntouchedByResetAndRematch(t *testing.T) {
	llm := newFakeLLM().on("matchmaker", `{"score": 70, "reason": "fine"}`)
	db := newTestDB(t)
	matcher := NewMatcher(db, newFakeGitHub(), llm)
	mm := NewMatchmaking(db, matcher, NewRelationships(db))
	admin := NewEventAdmin(db, matcher, mm)
	if err := admin.SetBigScreenTheme("paper"); err != nil {
		t.Fatal(err)
	}

	if err := admin.Reset(); err != nil {
		t.Fatal(err)
	}
	if got := db.BigScreenTheme().Key; got != "paper" {
		t.Errorf("Reset should not touch the Big Screen Theme, got %q", got)
	}

	seed(t, db, "a", "A", "ready")
	seed(t, db, "b", "B", "ready")
	if err := admin.Rematch().Wait(); err != nil {
		t.Fatal(err)
	}
	if got := db.BigScreenTheme().Key; got != "paper" {
		t.Errorf("Rematch should not touch the Big Screen Theme, got %q", got)
	}
}

// TestBigScreenTheme_ChosenByTheAdminAndPushedLiveToTheStream covers #42
// end to end: the admin's choice renders on /bigscreen and reaches an
// already-open stream without a reload.
func TestBigScreenTheme_ChosenByTheAdminAndPushedLiveToTheStream(t *testing.T) {
	srv, _ := newTestServer(t, nil, nil)

	if body := readBody(t, get(t, srv, "/bigscreen")); !strings.Contains(body, `data-theme="classic"`) {
		t.Errorf("want the default Theme before any choice, got:\n%s", body)
	}

	s := open(t, srv, "/bigscreen/stream")
	waitFor(t, s, `"theme_key":"classic"`)

	if loc := post(t, srv, "/admin/bigscreen-theme", url.Values{"theme": {"neon"}}).Header.Get("Location"); loc != "/admin" {
		t.Errorf("want redirect to admin, got %q", loc)
	}

	waitFor(t, s, `"theme_key":"neon"`)

	if body := readBody(t, get(t, srv, "/bigscreen")); !strings.Contains(body, `data-theme="neon"`) {
		t.Errorf("a fresh load should render the chosen Theme, got:\n%s", body)
	}
}
