package main

import (
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestDB_EventFormatDefaultsToClassic(t *testing.T) {
	db := newTestDB(t)
	if got := db.EventFormat(); got != ClassicFlow {
		t.Errorf("want ClassicFlow, got %q", got)
	}
}

func TestDB_EventFormatFallsBackToClassicForAnUnknownStoredValue(t *testing.T) {
	db := newTestDB(t)
	db.db.Exec(`INSERT OR REPLACE INTO event_state (key, value) VALUES (?, ?)`, eventFormatKey, "not-a-real-format")

	if got := db.EventFormat(); got != ClassicFlow {
		t.Errorf("want ClassicFlow for an unrecognised stored value, got %q", got)
	}
}

func TestDB_SetEventFormatIsRefusedWithParticipantsPresent(t *testing.T) {
	db := newTestDB(t)
	db.CreateParticipant("p", "p", "P", true)

	err := db.SetEventFormat(GameShow)

	if !errors.Is(err, ErrEventFormatLocked) {
		t.Fatalf("want ErrEventFormatLocked, got %v", err)
	}
	if got := db.EventFormat(); got != ClassicFlow {
		t.Errorf("the Event Format must not have changed, got %q", got)
	}
}

func TestDB_SetEventFormatSurvivesReopeningTheDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := NewDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	db.Close()

	reopened, err := NewDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got := reopened.EventFormat(); got != GameShow {
		t.Errorf("want the choice to survive reopening, got %q", got)
	}
}

// TestDB_ResetAllowsChangingTheEventFormatAgain covers #77: the format locks
// once a Participant registers, but a Reset (which clears every Participant)
// unlocks it again.
func TestDB_ResetAllowsChangingTheEventFormatAgain(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	db.CreateParticipant("p", "p", "P", true)
	if err := db.SetEventFormat(ClassicFlow); !errors.Is(err, ErrEventFormatLocked) {
		t.Fatalf("want it locked with a Participant present, got %v", err)
	}

	if err := db.Reset(); err != nil {
		t.Fatal(err)
	}

	if err := db.SetEventFormat(ClassicFlow); err != nil {
		t.Errorf("want the format changeable again after Reset, got %v", err)
	}
}

func TestHandler_AdminShowsAndChangesTheEventFormat(t *testing.T) {
	srv, _ := newTestServer(t, nil, nil)

	body := readBody(t, get(t, srv, "/admin"))
	if !strings.Contains(body, `value="classic" selected`) {
		t.Errorf("admin page should show Classic flow selected by default:\n%s", body)
	}

	if loc := post(t, srv, "/admin/event-format", url.Values{"format": {"game_show"}}).Header.Get("Location"); loc != "/admin" {
		t.Errorf("want redirect to admin, got %q", loc)
	}

	body = readBody(t, get(t, srv, "/admin"))
	if !strings.Contains(body, `value="game_show" selected`) {
		t.Errorf("admin page should now show Game Show selected:\n%s", body)
	}
}

func TestHandler_ChangingTheEventFormatWithParticipantsIsRefused(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	deps.db.CreateParticipant("p", "p", "P", true)

	resp := post(t, srv, "/admin/event-format", url.Values{"format": {"game_show"}})

	if resp.StatusCode != 400 {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
	if got := deps.db.EventFormat(); got != ClassicFlow {
		t.Errorf("the Event Format must not have changed, got %q", got)
	}
	if body := readBody(t, get(t, srv, "/admin")); !strings.Contains(body, "disabled") {
		t.Error("admin page should show the selector disabled while locked")
	}
}

func TestHandler_SetEventFormatReportsAGenuineFailure(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	breakDB(deps.db)

	resp := post(t, srv, "/admin/event-format", url.Values{"format": {"game_show"}})

	if resp.StatusCode != 500 {
		t.Errorf("want 500, got %d", resp.StatusCode)
	}
}

// TestOnboarding_GameShowLobbyAsksOnlyThePersonalizedQuestions covers #77:
// in a Game Show, a new Participant answers only their 5 personalised
// questions, then gets their Persona and becomes ready, same as the Classic
// flow but without the 6 Fixed Questions, which the Rounds ask later.
func TestOnboarding_GameShowLobbyAsksOnlyThePersonalizedQuestions(t *testing.T) {
	llm := newFakeLLM().
		on("interviewer", `{"questions": ["Why?", "How?", "When?", "Where?", "Who?"]}`).
		on("personality generator", `{"name": "The Gopher", "tagline": "Ships"}`)
	gh := newFakeGitHub().withProfile(&GitHubProfile{Login: "octo", Languages: []string{"Go"}})
	o, db := onboardingFor(t, llm, gh)
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}

	id, err := o.Register("Octo", "octo", true)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "interview started", func() bool { return reload(t, db, id).PipelineStep == StepInterviewing })

	p := reload(t, db, id)
	if len(p.Questions) != PersonalizedQuestions {
		t.Fatalf("Lobby question set: want %d personalised questions, got %d (%v)", PersonalizedQuestions, len(p.Questions), p.Questions)
	}
	for _, q := range p.Questions {
		if strings.HasPrefix(q.ID, "fixed_") {
			t.Errorf("the Lobby must not ask a Fixed Question, got %q", q.ID)
		}
	}

	answerEverything(t, o, db, id)
	eventually(t, "ready", func() bool { return reload(t, db, id).PipelineStep == StepReady })
	if got := reload(t, db, id).PersonaName; got != "The Gopher" {
		t.Errorf("persona: got %q", got)
	}
}
