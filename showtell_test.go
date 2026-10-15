package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// signUp signs a Participant up for show & tell through the store.
func signUp(t *testing.T, deps *testDeps, id, title string) {
	t.Helper()
	if err := deps.showtell.Signup(reload(t, deps.db, id), title, "One line", "https://example.com/repo"); err != nil {
		t.Fatal(err)
	}
}

// signupID returns the id of the Participant's sign-up.
func signupID(t *testing.T, deps *testDeps, participantID string) int64 {
	t.Helper()
	signup, err := deps.showtell.ForParticipant(participantID)
	if err != nil {
		t.Fatal(err)
	}
	if signup == nil {
		t.Fatalf("%s has no sign-up", participantID)
	}
	return signup.ID
}

// idValue renders a sign-up id as the form value the routes parse.
func idValue(id int64) string {
	return strconv.FormatInt(id, 10)
}

// stageSignup puts the Participant's sign-up on stage through the store.
func stageSignup(t *testing.T, deps *testDeps, participantID string) *ShowTellSignup {
	t.Helper()
	signup, err := deps.showtell.Stage(signupID(t, deps, participantID))
	if err != nil {
		t.Fatal(err)
	}
	return signup
}

func TestPhase_AllowsShowTell(t *testing.T) {
	for phase, want := range map[Phase]bool{
		PhaseOnboarding: false,
		PhaseSocial:     true,
		PhaseShowTell:   true,
		PhaseClosed:     false,
	} {
		if got := phase.AllowsShowTell(); got != want {
			t.Errorf("%s.AllowsShowTell: want %v, got %v", phase, want, got)
		}
	}
}

// TestShowTell_SignupIsValidatedAndPhaseGated covers the sign-up's guards: a
// title is required, the repo link must be an http(s) URL, and sign-up is
// open during the social and show & tell only.
func TestShowTell_SignupIsValidatedAndPhaseGated(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	p := reload(t, deps.db, "a")

	if err := deps.showtell.Signup(p, "", "", ""); !errors.Is(err, ErrInvalidSignup) {
		t.Errorf("empty title: want ErrInvalidSignup, got %v", err)
	}
	if err := deps.showtell.Signup(p, "  ", "", ""); !errors.Is(err, ErrInvalidSignup) {
		t.Errorf("blank title: want ErrInvalidSignup, got %v", err)
	}
	if err := deps.showtell.Signup(p, "Talk", "", "example.com/repo"); !errors.Is(err, ErrInvalidSignup) {
		t.Errorf("repo without a scheme: want ErrInvalidSignup, got %v", err)
	}
	if err := deps.showtell.Signup(p, "Talk", "", ""); !errors.Is(err, ErrShowTellClosed) {
		t.Errorf("during onboarding: want ErrShowTellClosed, got %v", err)
	}
	if err := deps.events.Advance(PhaseClosed); err != nil {
		t.Fatal(err)
	}
	if err := deps.showtell.Signup(p, "Talk", "", ""); !errors.Is(err, ErrShowTellClosed) {
		t.Errorf("once closed: want ErrShowTellClosed, got %v", err)
	}
}

// TestShowTell_SignupJoinsTheTailAndUpdatesInPlace covers the queue's order:
// late sign-ups join the tail, and a Participant who signs up again keeps
// their place and status while the talk itself is updated.
func TestShowTell_SignupJoinsTheTailAndUpdatesInPlace(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	seed(t, deps.db, "c", "The Snake", "ready")
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}

	signUp(t, deps, "a", "First talk")
	signUp(t, deps, "b", "Second talk")
	signUp(t, deps, "c", "Third talk")

	queue, err := deps.showtell.Queue()
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 3 {
		t.Fatalf("queue: want 3 sign-ups, got %d", len(queue))
	}
	for i, want := range []string{"First talk", "Second talk", "Third talk"} {
		if queue[i].Title != want || queue[i].Position != i || queue[i].Status != SignupQueued {
			t.Errorf("queue[%d]: got %+v, want %q at position %d queued", i, queue[i], want, i)
		}
	}
	// The presenter's Persona is joined for the host queue and the Big Screen.
	if queue[0].PersonaName != "The Gopher" || queue[0].PersonaSymbol == "" || queue[0].PersonaColor == "" {
		t.Errorf("queue[0] should carry the presenter's Persona, got %+v", queue[0])
	}

	// Signing up again updates the talk and keeps the place.
	first := queue[0]
	if err := deps.showtell.Signup(reload(t, deps.db, "a"), "Renamed talk", "New line", ""); err != nil {
		t.Fatal(err)
	}
	again, err := deps.showtell.ForParticipant("a")
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != first.ID || again.Position != first.Position || again.Title != "Renamed talk" ||
		again.Description != "New line" || again.RepoURL != "" {
		t.Errorf("re-sign-up should update in place, got %+v", again)
	}
	// A re-sign-up must not add a place to the queue.
	signUp(t, deps, "b", "Second talk")
	if queue, err = deps.showtell.Queue(); err != nil {
		t.Fatal(err)
	}
	if len(queue) != 3 {
		t.Errorf("a re-sign-up must not add a place, got %d sign-ups", len(queue))
	}

	// The sign-up is announced on the ticker.
	msgs, _ := deps.db.GetRecentActivity(10)
	if !strings.Contains(strings.Join(msgs, "\n"), "signed up for show & tell") {
		t.Errorf("the sign-up should be announced, got %v", msgs)
	}
}

// TestShowTell_QueueOnStageByIDAndForParticipant covers the store's reads.
func TestShowTell_QueueOnStageByIDAndForParticipant(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}

	if signup, err := deps.showtell.OnStage(); err != nil || signup != nil {
		t.Errorf("empty stage: want nil, nil — got %v, %v", signup, err)
	}
	if signup, err := deps.showtell.ByID(4242); err != nil || signup != nil {
		t.Errorf("unknown id: want nil, nil — got %v, %v", signup, err)
	}
	if signup, err := deps.showtell.ForParticipant("a"); err != nil || signup != nil {
		t.Errorf("no sign-up: want nil, nil — got %v, %v", signup, err)
	}

	signUp(t, deps, "a", "Talk")
	onStage := stageSignup(t, deps, "a")
	if got, _ := deps.showtell.OnStage(); got == nil || got.ID != onStage.ID {
		t.Errorf("OnStage: want sign-up %d, got %+v", onStage.ID, got)
	}
	if got, _ := deps.showtell.ByID(onStage.ID); got == nil || got.Title != "Talk" {
		t.Errorf("ByID: got %+v", got)
	}
	if got, _ := deps.showtell.ForParticipant("a"); got == nil || got.ID != onStage.ID {
		t.Errorf("ForParticipant: got %+v", got)
	}
}

// TestShowTell_QueuePosition covers the derived place in the queue.
func TestShowTell_QueuePosition(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")
	seed(t, deps.db, "b", "B", "ready")
	seed(t, deps.db, "c", "C", "ready")
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}

	if got := deps.showtell.QueuePosition("a"); got != 0 {
		t.Errorf("not signed up: want 0, got %d", got)
	}
	signUp(t, deps, "a", "A")
	signUp(t, deps, "b", "B")
	signUp(t, deps, "c", "C")
	for id, want := range map[string]int{"a": 1, "b": 2, "c": 3} {
		if got := deps.showtell.QueuePosition(id); got != want {
			t.Errorf("%s: want #%d, got %d", id, want, got)
		}
	}
	// Once on stage the Participant is no longer queued.
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	stageSignup(t, deps, "a")
	if got := deps.showtell.QueuePosition("a"); got != 0 {
		t.Errorf("on stage: want 0, got %d", got)
	}
}

// TestShowTell_Signature covers what the personal page's stream watches.
func TestShowTell_Signature(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}

	if got := deps.showtell.Signature("a"); got != "" {
		t.Errorf("no sign-up: want an empty signature, got %q", got)
	}
	signUp(t, deps, "a", "A")
	stageSignup(t, deps, "a")
	if got := deps.showtell.Signature("a"); got == "" || !strings.Contains(got, "on_stage") {
		t.Errorf("on stage: want a signature naming the status, got %q", got)
	}
	if _, err := deps.showtell.RevealIdentity(signupID(t, deps, "a")); err != nil {
		t.Fatal(err)
	}
	if got := deps.showtell.Signature("a"); !strings.Contains(got, "true") {
		t.Errorf("revealed: the signature should track the reveal, got %q", got)
	}

	breakOnFault(t, deps.db, "showtell_read")
	if got := deps.showtell.Signature("a"); got != "" {
		t.Errorf("a read failure: want an empty signature, got %q", got)
	}
}

// TestShowTell_StagePutsTheSignupOnStageWithTheHypeIntro covers presenter
// mode: the sign-up goes on stage with the Hype Agent's intro line, and the
// hype prompt carries the Persona and the talk — never the handle.
func TestShowTell_StagePutsTheSignupOnStageWithTheHypeIntro(t *testing.T) {
	llm := newFakeLLM().on("show & tell", `{"hype": "Please welcome… The Gopher 🦊 — ships on Fridays"}`)
	_, deps := newTestServer(t, llm, nil)
	seed(t, deps.db, "octocat", "The Gopher", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	signUp(t, deps, "octocat", "Turing Tumble")

	signup := stageSignup(t, deps, "octocat")

	if signup.Status != SignupOnStage {
		t.Errorf("status: want on_stage, got %s", signup.Status)
	}
	if signup.Hype != "Please welcome… The Gopher 🦊 — ships on Fridays" {
		t.Errorf("hype: got %q", signup.Hype)
	}
	stored, err := deps.showtell.ForParticipant("octocat")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Hype != signup.Hype || stored.Status != SignupOnStage {
		t.Errorf("the hype intro is stored on the sign-up, got %+v", stored)
	}
	msgs, _ := deps.db.GetRecentActivity(10)
	if !strings.Contains(strings.Join(msgs, "\n"), "is on stage") {
		t.Errorf("staging should be announced, got %v", msgs)
	}

	call, ok := llm.lastCallMatching("show & tell")
	if !ok {
		t.Fatal("the Hype Agent was not called")
	}
	if !strings.Contains(call.User, "The Gopher") || !strings.Contains(call.User, "Turing Tumble") {
		t.Errorf("the hype prompt should carry the Persona and the talk, got:\n%s", call.User)
	}
	if strings.Contains(call.User, "octocat") {
		t.Errorf("the hype prompt must not carry the handle:\n%s", call.User)
	}
}

// TestShowTell_StageFallsBackToTheGenericHypeLine covers the Hype Agent's
// fallback: on any LLM failure the generic line stands, so staging never
// waits on the LLM.
func TestShowTell_StageFallsBackToTheGenericHypeLine(t *testing.T) {
	for name, llm := range map[string]*fakeLLM{
		"the LLM fails":         newFakeLLM().onErr("show & tell", errUnscripted),
		"the reply is not JSON": newFakeLLM().on("show & tell", "Welcome everybody!"),
		"the reply is empty":    newFakeLLM().on("show & tell", `{"hype": "  "}`),
	} {
		t.Run(name, func(t *testing.T) {
			_, deps := newTestServer(t, llm, nil)
			seed(t, deps.db, "a", "The Gopher", "ready")
			if err := deps.events.Advance(PhaseShowTell); err != nil {
				t.Fatal(err)
			}
			signUp(t, deps, "a", "Talk")

			signup := stageSignup(t, deps, "a")

			if signup.Status != SignupOnStage {
				t.Errorf("staging should succeed on LLM failure, got %s", signup.Status)
			}
			if signup.Hype != genericHype {
				t.Errorf("hype: want the generic line %q, got %q", genericHype, signup.Hype)
			}
		})
	}
}

// TestShowTell_StageGuards covers the staging guards: presenter mode is a
// show & tell gain, the stage holds one presenter, and only a queued sign-up
// goes on stage.
func TestShowTell_StageGuards(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")
	seed(t, deps.db, "b", "B", "ready")

	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	signUp(t, deps, "a", "A")
	signUp(t, deps, "b", "B")
	if _, err := deps.showtell.Stage(signupID(t, deps, "a")); !errors.Is(err, ErrShowTellClosed) {
		t.Errorf("during the social: want ErrShowTellClosed, got %v", err)
	}

	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.showtell.Stage(4242); !errors.Is(err, ErrSignupNotFound) {
		t.Errorf("unknown id: want ErrSignupNotFound, got %v", err)
	}
	stageSignup(t, deps, "a")
	if _, err := deps.showtell.Stage(signupID(t, deps, "b")); !errors.Is(err, ErrAlreadyOnStage) {
		t.Errorf("while someone is on stage: want ErrAlreadyOnStage, got %v", err)
	}
	if _, err := deps.showtell.Stage(signupID(t, deps, "a")); !errors.Is(err, ErrAlreadyOnStage) {
		t.Errorf("staging the presenter again: want ErrAlreadyOnStage, got %v", err)
	}
	// A presented talk cannot go on stage again.
	if _, err := deps.showtell.Done(signupID(t, deps, "a")); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.showtell.Stage(signupID(t, deps, "a")); !errors.Is(err, ErrSignupNotQueued) {
		t.Errorf("a presented talk: want ErrSignupNotQueued, got %v", err)
	}
}

// TestShowTell_StageReportsAWriteFailure covers the branch that handles a
// write which passed every guard but failed in the database itself.
func TestShowTell_StageReportsAWriteFailure(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	signUp(t, deps, "a", "A")
	breakOnFault(t, deps.db, "showtell_update")

	if _, err := deps.showtell.Stage(signupID(t, deps, "a")); err == nil {
		t.Error("want the write failure reported")
	}
}

// TestShowTell_RevealIdentity covers the identity reveal: it is the host's
// explicit action, only ever available for the on-stage presenter, and
// idempotent.
func TestShowTell_RevealIdentity(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")
	seed(t, deps.db, "b", "B", "ready")
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	signUp(t, deps, "a", "A")
	signUp(t, deps, "b", "B")

	if _, err := deps.showtell.RevealIdentity(signupID(t, deps, "a")); !errors.Is(err, ErrShowTellClosed) {
		t.Errorf("during the social: want ErrShowTellClosed, got %v", err)
	}
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.showtell.RevealIdentity(4242); !errors.Is(err, ErrSignupNotFound) {
		t.Errorf("unknown id: want ErrSignupNotFound, got %v", err)
	}
	if _, err := deps.showtell.RevealIdentity(signupID(t, deps, "a")); !errors.Is(err, ErrNotOnStage) {
		t.Errorf("queued: want ErrNotOnStage, got %v", err)
	}

	signup := stageSignup(t, deps, "a")
	if signup.IdentityRevealed {
		t.Error("a freshly staged presenter is not revealed")
	}
	revealed, err := deps.showtell.RevealIdentity(signup.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !revealed.IdentityRevealed {
		t.Error("the reveal should set the flag")
	}
	// A second reveal is a friendly no-op.
	again, err := deps.showtell.RevealIdentity(signup.ID)
	if err != nil || !again.IdentityRevealed {
		t.Errorf("a second reveal is a no-op, got %v, %v", again, err)
	}
	msgs, _ := deps.db.GetRecentActivity(10)
	if !strings.Contains(strings.Join(msgs, "\n"), "revealed") {
		t.Errorf("the reveal should be announced, got %v", msgs)
	}

	// Once done, the reveal is gone with the stage.
	if _, err := deps.showtell.Done(signup.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.showtell.RevealIdentity(signup.ID); !errors.Is(err, ErrNotOnStage) {
		t.Errorf("presented: want ErrNotOnStage, got %v", err)
	}
}

// TestShowTell_RevealIdentityReportsAWriteFailure covers the reveal's write
// failure.
func TestShowTell_RevealIdentityReportsAWriteFailure(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	signUp(t, deps, "a", "A")
	stageSignup(t, deps, "a")
	breakOnFault(t, deps.db, "showtell_update")

	if _, err := deps.showtell.RevealIdentity(signupID(t, deps, "a")); err == nil {
		t.Error("want the write failure reported")
	}
}

// TestShowTell_DoneMarksTheTalkPresented covers the last status transition:
// the stage is taken for the next presenter.
func TestShowTell_DoneMarksTheTalkPresented(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")
	seed(t, deps.db, "b", "B", "ready")
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	signUp(t, deps, "a", "A")
	signUp(t, deps, "b", "B")

	if _, err := deps.showtell.Done(signupID(t, deps, "a")); !errors.Is(err, ErrShowTellClosed) {
		t.Errorf("during the social: want ErrShowTellClosed, got %v", err)
	}
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.showtell.Done(4242); !errors.Is(err, ErrSignupNotFound) {
		t.Errorf("unknown id: want ErrSignupNotFound, got %v", err)
	}
	if _, err := deps.showtell.Done(signupID(t, deps, "a")); !errors.Is(err, ErrNotOnStage) {
		t.Errorf("queued: want ErrNotOnStage, got %v", err)
	}

	stageSignup(t, deps, "a")
	done, err := deps.showtell.Done(signupID(t, deps, "a"))
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != SignupPresented {
		t.Errorf("status: want presented, got %s", done.Status)
	}
	if onStage, _ := deps.showtell.OnStage(); onStage != nil {
		t.Errorf("the stage should be empty, got %+v", onStage)
	}
	// The next presenter can take the stage.
	if _, err := deps.showtell.Stage(signupID(t, deps, "b")); err != nil {
		t.Errorf("the next presenter should take the empty stage, got %v", err)
	}

	breakOnFault(t, deps.db, "showtell_update")
	if _, err := deps.showtell.Done(signupID(t, deps, "b")); err == nil {
		t.Error("want the write failure reported")
	}
}

// TestShowTell_MoveReordersTheQueue covers the host's reorder: up and down
// swap places with the neighbour, and the ends, unknown sign-ups and
// non-queued sign-ups are no-ops.
func TestShowTell_MoveReordersTheQueue(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")
	seed(t, deps.db, "b", "B", "ready")
	seed(t, deps.db, "c", "C", "ready")
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	signUp(t, deps, "a", "A")
	signUp(t, deps, "b", "B")
	signUp(t, deps, "c", "C")

	queueTitles := func() []string {
		queue, err := deps.showtell.Queue()
		if err != nil {
			t.Fatal(err)
		}
		var titles []string
		for _, s := range queue {
			titles = append(titles, s.Title)
		}
		return titles
	}

	if err := deps.showtell.Move(signupID(t, deps, "b"), "sideways"); !errors.Is(err, ErrInvalidDirection) {
		t.Errorf("an unknown direction: want ErrInvalidDirection, got %v", err)
	}
	if err := deps.showtell.Move(4242, "up"); err != nil {
		t.Errorf("an unknown sign-up: a no-op, got %v", err)
	}
	if err := deps.showtell.Move(signupID(t, deps, "a"), "up"); err != nil {
		t.Errorf("the head moving up: a no-op, got %v", err)
	}
	if err := deps.showtell.Move(signupID(t, deps, "c"), "down"); err != nil {
		t.Errorf("the tail moving down: a no-op, got %v", err)
	}
	if got := queueTitles(); strings.Join(got, ",") != "A,B,C" {
		t.Fatalf("the no-ops should not move anything, got %v", got)
	}

	if err := deps.showtell.Move(signupID(t, deps, "b"), "up"); err != nil {
		t.Fatal(err)
	}
	if got := queueTitles(); strings.Join(got, ",") != "B,A,C" {
		t.Errorf("b up: want B,A,C, got %v", got)
	}
	if err := deps.showtell.Move(signupID(t, deps, "b"), "down"); err != nil {
		t.Fatal(err)
	}
	if got := queueTitles(); strings.Join(got, ",") != "A,B,C" {
		t.Errorf("b down again: want A,B,C, got %v", got)
	}
	if err := deps.showtell.Move(signupID(t, deps, "a"), "down"); err != nil {
		t.Fatal(err)
	}
	if got := queueTitles(); strings.Join(got, ",") != "B,A,C" {
		t.Errorf("a down: want B,A,C, got %v", got)
	}

	// A sign-up that is not queued does not move.
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	stageSignup(t, deps, "a")
	if err := deps.showtell.Move(signupID(t, deps, "a"), "up"); err != nil {
		t.Errorf("an on-stage sign-up: a no-op, got %v", err)
	}
	if got := queueTitles(); strings.Join(got, ",") != "B,A,C" {
		t.Errorf("the on-stage sign-up should not move, got %v", got)
	}
}

// TestShowTell_MoveReportsAFault covers the reorder's failures: a read
// failure and a write failure.
func TestShowTell_MoveReportsAFault(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")
	seed(t, deps.db, "b", "B", "ready")
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	signUp(t, deps, "a", "A")
	signUp(t, deps, "b", "B")
	aID := signupID(t, deps, "a")

	breakOnFault(t, deps.db, "showtell_update")
	if err := deps.showtell.Move(aID, "down"); err == nil {
		t.Error("want the write failure reported")
	}
	breakOnFault(t, deps.db, "showtell_read")
	if err := deps.showtell.Move(aID, "down"); err == nil {
		t.Error("want the read failure reported")
	}
}

// TestShowTell_RemoveTakesTheSignupOut covers the host's remove, whatever the
// sign-up's status.
func TestShowTell_RemoveTakesTheSignupOut(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")
	seed(t, deps.db, "b", "B", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	signUp(t, deps, "a", "A")
	signUp(t, deps, "b", "B")

	if err := deps.showtell.Remove(4242); err != nil {
		t.Errorf("removing an unknown sign-up: a no-op, got %v", err)
	}
	if err := deps.showtell.Remove(signupID(t, deps, "a")); err != nil {
		t.Fatal(err)
	}
	if signup, _ := deps.showtell.ForParticipant("a"); signup != nil {
		t.Errorf("the sign-up should be gone, got %+v", signup)
	}
	// An on-stage sign-up can be removed too.
	stageSignup(t, deps, "b")
	if err := deps.showtell.Remove(signupID(t, deps, "b")); err != nil {
		t.Fatal(err)
	}
	if onStage, _ := deps.showtell.OnStage(); onStage != nil {
		t.Errorf("the stage should be empty, got %+v", onStage)
	}

	signUp(t, deps, "b", "B again")
	breakOnFault(t, deps.db, "showtell_delete")
	if err := deps.showtell.Remove(signupID(t, deps, "b")); err == nil {
		t.Error("want the write failure reported")
	}
}

// TestShowTell_RemoveParticipant covers the cleanup when a Participant leaves
// the event.
func TestShowTell_RemoveParticipant(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	signUp(t, deps, "a", "A")

	if err := deps.showtell.RemoveParticipant("a"); err != nil {
		t.Fatal(err)
	}
	if signup, _ := deps.showtell.ForParticipant("a"); signup != nil {
		t.Errorf("the sign-up should be gone, got %+v", signup)
	}

	signUp(t, deps, "a", "A again")
	breakOnFault(t, deps.db, "showtell_delete")
	if err := deps.showtell.RemoveParticipant("a"); err == nil {
		t.Error("want the write failure reported")
	}
}

// TestShowTell_ReadsReportAFault covers the store's read failures.
func TestShowTell_ReadsReportAFault(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	signUp(t, deps, "a", "A")
	stageSignup(t, deps, "a")

	breakOnFault(t, deps.db, "showtell_read")
	if _, err := deps.showtell.Queue(); err == nil {
		t.Error("Queue: want the read failure reported")
	}
	if _, err := deps.showtell.OnStage(); err == nil {
		t.Error("OnStage: want the read failure reported")
	}
	if _, err := deps.showtell.ByID(1); err == nil {
		t.Error("ByID: want the read failure reported")
	}
	if _, err := deps.showtell.ForParticipant("a"); err == nil {
		t.Error("ForParticipant: want the read failure reported")
	}
	if _, err := deps.showtell.Stage(1); err == nil {
		t.Error("Stage: want the read failure reported")
	}
	if _, err := deps.showtell.RevealIdentity(1); err == nil {
		t.Error("RevealIdentity: want the read failure reported")
	}
	if _, err := deps.showtell.Done(1); err == nil {
		t.Error("Done: want the read failure reported")
	}
}

// TestShowTell_QueueReportsABrokenDatabase covers the queue's query failure.
func TestShowTell_QueueReportsABrokenDatabase(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	breakDB(deps.db)

	if _, err := deps.showtell.Queue(); err == nil {
		t.Error("want the query failure reported")
	}
}

// TestShowTellSignup_Route covers the sign-up route: validation, phase
// gating and the fragment it answers with.
func TestShowTellSignup_Route(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")

	if resp := post(t, srv, "/user/showtell/signup/nobody", url.Values{"title": {"Talk"}}); resp.StatusCode != 404 {
		t.Errorf("unknown participant: want 404, got %d", resp.StatusCode)
	}
	if resp := post(t, srv, "/user/showtell/signup/a", url.Values{"title": {"Talk"}}); resp.StatusCode != 400 {
		t.Errorf("during onboarding: want 400, got %d", resp.StatusCode)
	}
	if resp := post(t, srv, "/user/showtell/signup/a", url.Values{"title": {"  "}}); resp.StatusCode != 400 {
		t.Errorf("no title: want 400, got %d", resp.StatusCode)
	}
	if resp := post(t, srv, "/user/showtell/signup/a", url.Values{"title": {"Talk"}, "repo": {"example.com"}}); resp.StatusCode != 400 {
		t.Errorf("a repo link without a scheme: want 400, got %d", resp.StatusCode)
	}
	if err := deps.events.Advance(PhaseClosed); err != nil {
		t.Fatal(err)
	}
	if resp := post(t, srv, "/user/showtell/signup/a", url.Values{"title": {"Talk"}}); resp.StatusCode != 400 {
		t.Errorf("once closed: want 400, got %d", resp.StatusCode)
	}

	if err := deps.events.Reset(); err != nil {
		t.Fatal(err)
	}
	seed(t, deps.db, "a", "The Gopher", "ready")
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	body := readBody(t, post(t, srv, "/user/showtell/signup/a", url.Values{
		"title":       {"My talk"},
		"description": {"About things"},
		"repo":        {"https://example.com/talk"},
	}))
	for _, want := range []string{"My talk", "You're in the queue", `hx-post="/user/showtell/signup/a"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the sign-up fragment should contain %q", want)
		}
	}
	signup, err := deps.showtell.ForParticipant("a")
	if err != nil {
		t.Fatal(err)
	}
	if signup == nil || signup.Title != "My talk" || signup.RepoURL != "https://example.com/talk" {
		t.Errorf("the sign-up should be stored, got %+v", signup)
	}

	breakOnFault(t, deps.db, "showtell_insert")
	if resp := post(t, srv, "/user/showtell/signup/a", url.Values{"title": {"Talk"}}); resp.StatusCode != 500 {
		t.Errorf("a write failure: want 500, got %d", resp.StatusCode)
	}
}

// TestShowTellStatus_Fragment covers the personal page's show & tell section.
func TestShowTellStatus_Fragment(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")

	if resp := get(t, srv, "/user/showtell/nobody"); resp.StatusCode != 404 {
		t.Errorf("unknown participant: want 404, got %d", resp.StatusCode)
	}
	// During onboarding the section is empty.
	if body := readBody(t, get(t, srv, "/user/showtell/a")); strings.Contains(body, "Sign me up") {
		t.Errorf("during onboarding the sign-up form should be hidden")
	}

	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	if body := readBody(t, get(t, srv, "/user/showtell/a")); !strings.Contains(body, "Sign me up") {
		t.Errorf("during show & tell the sign-up form should show")
	}

	signUp(t, deps, "a", "My talk")
	body := readBody(t, get(t, srv, "/user/showtell/a"))
	for _, want := range []string{"My talk", "In the queue — #1", "Update my sign-up"} {
		if !strings.Contains(body, want) {
			t.Errorf("a queued sign-up should show %q, got:\n%s", want, body)
		}
	}

	stageSignup(t, deps, "a")
	if body := readBody(t, get(t, srv, "/user/showtell/a")); !strings.Contains(body, "You're on stage!") {
		t.Errorf("an on-stage sign-up should say so, got:\n%s", body)
	}
	if _, err := deps.showtell.RevealIdentity(signupID(t, deps, "a")); err != nil {
		t.Fatal(err)
	}
	if body := readBody(t, get(t, srv, "/user/showtell/a")); !strings.Contains(body, "revealed your identity") {
		t.Errorf("a revealed sign-up should say so, got:\n%s", body)
	}
	if _, err := deps.showtell.Done(signupID(t, deps, "a")); err != nil {
		t.Fatal(err)
	}
	if body := readBody(t, get(t, srv, "/user/showtell/a")); !strings.Contains(body, "You presented") {
		t.Errorf("a presented sign-up should say so, got:\n%s", body)
	}

	breakOnFault(t, deps.db, "showtell_read")
	if resp := get(t, srv, "/user/showtell/a"); resp.StatusCode != 200 {
		t.Errorf("a read failure still renders the section, got %d", resp.StatusCode)
	}
}

// TestPersonalPages_ShowTheSignUpFormDuringSocialAndShowTell covers the
// personal pages carrying the sign-up form while it is open: the wait page
// and the Match page's phase-content region from the social on — sign-up is
// open during the social and the show & tell, and the matched Participants
// who live on the Match page during the social must reach it there too (the
// first live event's "the register show & tell was gone").
func TestPersonalPages_ShowTheSignUpFormDuringSocialAndShowTell(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	seed(t, deps.db, "c", "The Snake", "ready")
	pair(t, deps.db, "a", "b")

	if body := readBody(t, get(t, srv, "/user/wait/c")); strings.Contains(body, "/user/showtell/signup/c") {
		t.Errorf("during onboarding the wait page should not show the sign-up form")
	}

	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	if body := readBody(t, get(t, srv, "/user/wait/c")); !strings.Contains(body, "/user/showtell/signup/c") {
		t.Errorf("during the social the wait page should show the sign-up form")
	}
	if body := readBody(t, get(t, srv, "/user/match/a")); !strings.Contains(body, `hx-post="/user/showtell/signup/a"`) {
		t.Errorf("during the social the Match page should show the sign-up form")
	}

	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	if body := readBody(t, get(t, srv, "/user/wait/c")); !strings.Contains(body, `hx-post="/user/showtell/signup/c"`) {
		t.Errorf("during show & tell the wait page should show the sign-up form")
	}
	if body := readBody(t, get(t, srv, "/user/match/a")); !strings.Contains(body, `hx-post="/user/showtell/signup/a"`) {
		t.Errorf("during show & tell the Match page should show the sign-up form")
	}
}

// TestShowTellStage_Route covers the host's presenter-mode route: the three
// actions and every rejection.
func TestShowTellStage_Route(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")
	seed(t, deps.db, "b", "B", "ready")

	stage := func(form url.Values) *http.Response {
		return post(t, srv, "/admin/showtell/stage", form)
	}

	if resp := stage(url.Values{"id": {"not-a-number"}, "action": {"stage"}}); resp.StatusCode != 400 {
		t.Errorf("a malformed id: want 400, got %d", resp.StatusCode)
	}
	if resp := stage(url.Values{"id": {"1"}, "action": {"explode"}}); resp.StatusCode != 400 {
		t.Errorf("an unknown action: want 400, got %d", resp.StatusCode)
	}
	if resp := stage(url.Values{"id": {"4242"}, "action": {"stage"}}); resp.StatusCode != 400 {
		t.Errorf("during onboarding: want 400, got %d", resp.StatusCode)
	}

	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	signUp(t, deps, "a", "A")
	signUp(t, deps, "b", "B")
	if resp := stage(url.Values{"id": {"4242"}, "action": {"stage"}}); resp.StatusCode != 400 {
		t.Errorf("an unknown sign-up during the social: want 400 (presenter mode is closed), got %d", resp.StatusCode)
	}
	if resp := stage(url.Values{"id": {idValue(signupID(t, deps, "a"))}, "action": {"reveal"}}); resp.StatusCode != 400 {
		t.Errorf("revealing a queued sign-up: want 400, got %d", resp.StatusCode)
	}
	if resp := stage(url.Values{"id": {idValue(signupID(t, deps, "a"))}, "action": {"stage"}}); resp.StatusCode != 400 {
		t.Errorf("staging during the social: want 400, got %d", resp.StatusCode)
	}

	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	if resp := stage(url.Values{"id": {"4242"}, "action": {"stage"}}); resp.StatusCode != 404 {
		t.Errorf("an unknown sign-up: want 404, got %d", resp.StatusCode)
	}
	// An error response must not fall through to the redirect — a second
	// WriteHeader is superfluous (#128). Observed on a recorder, where the
	// redirect's Location header would leak into the header map.
	rec := httptest.NewRecorder()
	form := strings.NewReader("id=4242&action=stage")
	req := httptest.NewRequest("POST", "/admin/showtell/stage", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.h.ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Errorf("an unknown sign-up: want 404, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("an error response must not carry a redirect, got Location %q", loc)
	}
	id := signupID(t, deps, "a")
	if resp := stage(url.Values{"id": {idValue(id)}, "action": {"stage"}}); resp.Header.Get("Location") != "/admin" {
		t.Errorf("staging: want a redirect to /admin, got %q", resp.Header.Get("Location"))
	}
	if onStage, _ := deps.showtell.OnStage(); onStage == nil || onStage.ID != id {
		t.Fatalf("the sign-up should be on stage, got %+v", onStage)
	}
	if resp := stage(url.Values{"id": {idValue(signupID(t, deps, "b"))}, "action": {"stage"}}); resp.StatusCode != 400 {
		t.Errorf("staging while someone is on stage: want 400, got %d", resp.StatusCode)
	}
	if resp := stage(url.Values{"id": {idValue(id)}, "action": {"reveal"}}); resp.StatusCode != 303 {
		t.Errorf("the reveal: want a redirect, got %d", resp.StatusCode)
	}
	if signup, _ := deps.showtell.ByID(id); signup == nil || !signup.IdentityRevealed {
		t.Errorf("the reveal should be stored, got %+v", signup)
	}
	if resp := stage(url.Values{"id": {idValue(id)}, "action": {"done"}}); resp.StatusCode != 303 {
		t.Errorf("done: want a redirect, got %d", resp.StatusCode)
	}
	if signup, _ := deps.showtell.ByID(id); signup == nil || signup.Status != SignupPresented {
		t.Errorf("the talk should be presented, got %+v", signup)
	}
	if resp := stage(url.Values{"id": {idValue(id)}, "action": {"done"}}); resp.StatusCode != 400 {
		t.Errorf("done on a presented talk: want 400, got %d", resp.StatusCode)
	}
	if resp := stage(url.Values{"id": {idValue(id)}, "action": {"stage"}}); resp.StatusCode != 400 {
		t.Errorf("staging a presented talk: want 400, got %d", resp.StatusCode)
	}

	breakOnFault(t, deps.db, "showtell_update")
	if resp := stage(url.Values{"id": {idValue(signupID(t, deps, "b"))}, "action": {"stage"}}); resp.StatusCode != 500 {
		t.Errorf("a write failure: want 500, got %d", resp.StatusCode)
	}
}

// TestShowTellMoveAndRemove_Routes covers the host queue's reorder and
// remove routes.
func TestShowTellMoveAndRemove_Routes(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")
	seed(t, deps.db, "b", "B", "ready")
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	signUp(t, deps, "a", "A")
	signUp(t, deps, "b", "B")

	move := func(form url.Values) *http.Response {
		return post(t, srv, "/admin/showtell/move", form)
	}
	if resp := move(url.Values{"id": {"nope"}, "dir": {"up"}}); resp.StatusCode != 400 {
		t.Errorf("a malformed id: want 400, got %d", resp.StatusCode)
	}
	if resp := move(url.Values{"id": {idValue(signupID(t, deps, "a"))}, "dir": {"sideways"}}); resp.StatusCode != 400 {
		t.Errorf("an unknown direction: want 400, got %d", resp.StatusCode)
	}
	if resp := move(url.Values{"id": {idValue(signupID(t, deps, "b"))}, "dir": {"up"}}); resp.StatusCode != 303 {
		t.Errorf("a reorder: want a redirect, got %d", resp.StatusCode)
	}
	queue, err := deps.showtell.Queue()
	if err != nil {
		t.Fatal(err)
	}
	if queue[0].Title != "B" || queue[1].Title != "A" {
		t.Errorf("b should have moved up, got %v", queue)
	}

	remove := func(form url.Values) *http.Response {
		return post(t, srv, "/admin/showtell/remove", form)
	}
	if resp := remove(url.Values{"id": {"nope"}}); resp.StatusCode != 400 {
		t.Errorf("a malformed id: want 400, got %d", resp.StatusCode)
	}
	if resp := remove(url.Values{"id": {idValue(signupID(t, deps, "a"))}}); resp.StatusCode != 303 {
		t.Errorf("a remove: want a redirect, got %d", resp.StatusCode)
	}
	if signup, _ := deps.showtell.ForParticipant("a"); signup != nil {
		t.Errorf("the sign-up should be gone, got %+v", signup)
	}

	signUp(t, deps, "a", "A again")
	breakOnFault(t, deps.db, "showtell_update")
	if resp := move(url.Values{"id": {idValue(signupID(t, deps, "b"))}, "dir": {"down"}}); resp.StatusCode != 500 {
		t.Errorf("a write failure on move: want 500, got %d", resp.StatusCode)
	}
	breakOnFault(t, deps.db, "showtell_delete")
	if resp := remove(url.Values{"id": {idValue(signupID(t, deps, "b"))}}); resp.StatusCode != 500 {
		t.Errorf("a write failure on remove: want 500, got %d", resp.StatusCode)
	}
}

// TestAdmin_ShowsTheShowTellQueue covers the host console's queue section.
func TestAdmin_ShowsTheShowTellQueue(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")

	if body := readBody(t, get(t, srv, "/admin")); strings.Contains(body, "showtell-queue") {
		t.Errorf("during onboarding the host console should not show the queue")
	}

	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	signUp(t, deps, "a", "Turing Tumble")
	body := readBody(t, get(t, srv, "/admin"))
	for _, want := range []string{"showtell-queue", "Turing Tumble", "The Gopher", "On stage", "/admin/showtell/move", "/admin/showtell/remove"} {
		if !strings.Contains(body, want) {
			t.Errorf("the host console should show %q", want)
		}
	}

	// The fragment endpoint serves the same section.
	body = readBody(t, get(t, srv, "/admin/showtell-queue"))
	if !strings.Contains(body, "Turing Tumble") {
		t.Errorf("the queue fragment should list the sign-up, got:\n%s", body)
	}

	// The stage actions appear once the sign-up is on stage.
	stageSignup(t, deps, "a")
	body = readBody(t, get(t, srv, "/admin/showtell-queue"))
	for _, want := range []string{"Reveal identity", "Done", "on stage"} {
		if !strings.Contains(body, want) {
			t.Errorf("the on-stage sign-up should show %q, got:\n%s", want, body)
		}
	}
	if strings.Contains(body, `value="stage"`) {
		t.Errorf("an on-stage sign-up should not offer staging again")
	}

	// A presented talk keeps its place in the history.
	if _, err := deps.showtell.Done(signupID(t, deps, "a")); err != nil {
		t.Fatal(err)
	}
	if body := readBody(t, get(t, srv, "/admin/showtell-queue")); !strings.Contains(body, "presented") {
		t.Errorf("a presented talk should be listed, got:\n%s", body)
	}

	// A read failure still renders the section.
	breakOnFault(t, deps.db, "showtell_read")
	if resp := get(t, srv, "/admin/showtell-queue"); resp.StatusCode != 200 {
		t.Errorf("a read failure still renders the queue, got %d", resp.StatusCode)
	}
	if body := readBody(t, get(t, srv, "/admin")); !strings.Contains(body, "No sign-ups yet") {
		t.Errorf("a read failure shows the empty queue, got:\n%s", body)
	}
}

// TestShowTellQueueSignature covers what the queue stream watches.
func TestShowTellQueueSignature(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}

	if got := deps.handler.showtellQueueSignature(); got != "" {
		t.Errorf("an empty queue: want an empty signature, got %q", got)
	}
	signUp(t, deps, "a", "A")
	if got := deps.handler.showtellQueueSignature(); !strings.Contains(got, "queued") {
		t.Errorf("a sign-up should change the signature, got %q", got)
	}
	breakOnFault(t, deps.db, "showtell_read")
	if got := deps.handler.showtellQueueSignature(); got != "" {
		t.Errorf("a read failure: want an empty signature, got %q", got)
	}
}

// TestShowTellFragments_ExplainTheirActions covers #131: the host console's
// queue and question fragments say what their buttons do — the host should
// never have to guess what "Reveal identity" or "Ask" does.
func TestShowTellFragments_ExplainTheirActions(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}

	queue := readBody(t, get(t, srv, "/admin/showtell-queue"))
	for _, want := range []string{"On stage puts the talk up", "Reveal identity flips the presenter's card", "Done ends the talk"} {
		if !strings.Contains(queue, want) {
			t.Errorf("the queue fragment should explain %q, got: %s", want, queue)
		}
	}
	questions := readBody(t, get(t, srv, "/admin/showtell-questions"))
	for _, want := range []string{"Ask puts the question on the big screen", "Answered marks it done", "Dismiss drops it"} {
		if !strings.Contains(questions, want) {
			t.Errorf("the questions fragment should explain %q, got: %s", want, questions)
		}
	}
}

// TestShowTellFlow_SignUpQueueOnStageIdentityReveal is the acceptance flow
// end to end, through the routes: sign-up → host queue (reorder/remove) → on
// stage → identity reveal.
func TestShowTellFlow_SignUpQueueOnStageIdentityReveal(t *testing.T) {
	llm := newFakeLLM().on("show & tell", `{"hype": "Please welcome… The Gopher 🦊 — ships on Fridays"}`)
	srv, deps := newTestServer(t, llm, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")

	// The social: sign-up opens.
	post(t, srv, "/admin/phase", url.Values{"to": {"social"}})
	post(t, srv, "/user/showtell/signup/a", url.Values{"title": {"Turing Tumble"}, "description": {"A talk"}, "repo": {"https://example.com/t"}})
	post(t, srv, "/user/showtell/signup/b", url.Values{"title": {"Crab Rave"}})

	// The host queue: reorder and remove.
	queue, err := deps.showtell.Queue()
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 2 {
		t.Fatalf("queue: want 2 sign-ups, got %d", len(queue))
	}
	post(t, srv, "/admin/showtell/move", url.Values{"id": {idValue(queue[1].ID)}, "dir": {"up"}})
	queue, _ = deps.showtell.Queue()
	if queue[0].Title != "Crab Rave" {
		t.Fatalf("the reorder should swap the queue, got %v", queue)
	}
	post(t, srv, "/admin/showtell/remove", url.Values{"id": {idValue(queue[0].ID)}})
	if queue, _ = deps.showtell.Queue(); len(queue) != 1 || queue[0].Title != "Turing Tumble" {
		t.Fatalf("the remove should shrink the queue to a's talk, got %v", queue)
	}

	// Show & tell: presenter mode.
	post(t, srv, "/admin/phase", url.Values{"to": {"showtell"}})
	id := signupID(t, deps, "a")
	post(t, srv, "/admin/showtell/stage", url.Values{"id": {idValue(id)}, "action": {"stage"}})

	var graph map[string]any
	if err := json.Unmarshal([]byte(readBody(t, get(t, srv, "/bigscreen/graph-data"))), &graph); err != nil {
		t.Fatal(err)
	}
	presenter, ok := graph["presenter"].(map[string]any)
	if !ok {
		t.Fatalf("presenter mode: the payload should carry the presenter, got %v", graph)
	}
	for key, want := range map[string]string{
		"persona_name": "The Gopher",
		"title":        "Turing Tumble",
		"hype":         "Please welcome… The Gopher 🦊 — ships on Fridays",
	} {
		if presenter[key] != want {
			t.Errorf("presenter.%s: want %q, got %v", key, want, presenter[key])
		}
	}
	if presenter["identity_revealed"] != false {
		t.Errorf("the identity starts hidden, got %v", presenter["identity_revealed"])
	}

	// The identity reveal: only via the host's explicit action.
	post(t, srv, "/admin/showtell/stage", url.Values{"id": {idValue(id)}, "action": {"reveal"}})
	if err := json.Unmarshal([]byte(readBody(t, get(t, srv, "/bigscreen/graph-data"))), &graph); err != nil {
		t.Fatal(err)
	}
	presenter = graph["presenter"].(map[string]any)
	if presenter["handle"] != "@a" || presenter["name"] != "a" {
		t.Errorf("the reveal flips the card to the handle/name, got %v", presenter)
	}

	// Done: the stage is taken.
	post(t, srv, "/admin/showtell/stage", url.Values{"id": {idValue(id)}, "action": {"done"}})
	var afterDone map[string]any
	if err := json.Unmarshal([]byte(readBody(t, get(t, srv, "/bigscreen/graph-data"))), &afterDone); err != nil {
		t.Fatal(err)
	}
	if _, ok := afterDone["presenter"]; ok {
		t.Errorf("after done there is no presenter, got %v", afterDone["presenter"])
	}
}

// TestPresenterMode_TheBigScreenShowsPersonasOnlyUntilTheHostReveals covers
// the privacy baseline: without the host's explicit reveal, no handle or name
// reaches the Big Screen — personas only, in every phase.
func TestPresenterMode_TheBigScreenShowsPersonasOnlyUntilTheHostReveals(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")

	// During the social there is no presenter mode at all.
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	signUp(t, deps, "a", "Turing Tumble")
	if body := readBody(t, get(t, srv, "/bigscreen/graph-data")); strings.Contains(body, `"presenter"`) {
		t.Errorf("during the social the Big Screen has no presenter mode")
	}

	// On stage: the persona card and the hype intro, never the identity.
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	stageSignup(t, deps, "a")
	body := readBody(t, get(t, srv, "/bigscreen/graph-data"))
	for _, want := range []string{"The Gopher", "Turing Tumble", genericHype} {
		if !strings.Contains(body, want) {
			t.Errorf("the presenter card should show %q", want)
		}
	}
	for _, absent := range []string{"@a", `"handle"`, `"name": "a"`} {
		if strings.Contains(body, absent) {
			t.Errorf("before the reveal the Big Screen must not carry %q", absent)
		}
	}

	// The other participants' pages show no identity either.
	seed(t, deps.db, "b", "The Crab", "ready")
	if body := readBody(t, get(t, srv, "/user/wait/b")); strings.Contains(body, "@a") {
		t.Errorf("the personal page must not carry the presenter's handle")
	}

	// The reveal is the only flip.
	if _, err := deps.showtell.RevealIdentity(signupID(t, deps, "a")); err != nil {
		t.Fatal(err)
	}
	if body := readBody(t, get(t, srv, "/bigscreen/graph-data")); !strings.Contains(body, "@a") {
		t.Errorf("after the host's reveal the card shows the handle")
	}
}

// TestPresenterPayload_ReadFailures covers presenter mode's read failures:
// a broken queue read drops the presenter, and a broken identity lookup
// keeps the identity off the payload.
func TestPresenterPayload_ReadFailures(t *testing.T) {
	setup := func(t *testing.T) (*testSrv, *testDeps) {
		srv, deps := newTestServer(t, nil, nil)
		seed(t, deps.db, "a", "The Gopher", "ready")
		if err := deps.events.Advance(PhaseShowTell); err != nil {
			t.Fatal(err)
		}
		signUp(t, deps, "a", "Turing Tumble")
		stageSignup(t, deps, "a")
		if _, err := deps.showtell.RevealIdentity(signupID(t, deps, "a")); err != nil {
			t.Fatal(err)
		}
		return srv, deps
	}

	t.Run("a broken queue read drops the presenter", func(t *testing.T) {
		srv, deps := setup(t)
		breakOnFault(t, deps.db, "showtell_read")
		if resp := get(t, srv, "/bigscreen/graph-data"); resp.StatusCode != 200 {
			t.Errorf("a queue read failure still serves the graph, got %d", resp.StatusCode)
		}
		if body := readBody(t, get(t, srv, "/bigscreen/graph-data")); strings.Contains(body, `"presenter"`) {
			t.Errorf("a queue read failure drops the presenter")
		}
	})

	t.Run("a broken identity lookup keeps the identity off the payload", func(t *testing.T) {
		srv, deps := setup(t)
		breakOnFault(t, deps.db, "presenter_identity_read")
		body := readBody(t, get(t, srv, "/bigscreen/graph-data"))
		if !strings.Contains(body, `"presenter"`) {
			t.Fatalf("the presenter should still be there, got:\n%s", body)
		}
		if strings.Contains(body, "@a") {
			t.Errorf("a broken identity lookup must not leak the identity")
		}
	})
}

// TestShowTellStream_PushesTheQueueByPush covers the host console's queue
// stream: a refresh when the queue changes, silence otherwise.
func TestShowTellStream_PushesTheQueueByPush(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}

	s := open(t, srv, "/admin/showtell-stream")
	sentNothingNew(t, s, 1, "", "an unchanged queue")

	signUp(t, deps, "a", "A")
	waitFor(t, s, "event: refresh")

	before := s.String()
	deps.db.LogActivity("someone else did something")
	sentNothingNew(t, s, 3, before, "a change that does not touch the queue")
}

// TestWaitStream_RefreshesTheShowTellSection covers the personal page's
// stream: the sign-up state is part of what it watches.
func TestWaitStream_RefreshesTheShowTellSection(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}

	s := open(t, srv, "/user/wait-stream/a")
	waitFor(t, s, "event: refresh") // the sync on connect
	before := s.String()
	sentNothingNew(t, s, 1, before, "nothing changed")

	signUp(t, deps, "a", "A")
	waitForCount(t, s, "event: refresh", strings.Count(before, "event: refresh")+1)

	before = s.String()
	stageSignup(t, deps, "a")
	eventually(t, "staging to refresh the section", func() bool {
		return len(s.String()) > len(before)
	})
}

// TestScreenStream_PushesThePresenterAndTheReveal covers the Big Screen's
// stream: the presenter card arrives by push, and so does the identity reveal.
func TestScreenStream_PushesThePresenterAndTheReveal(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	signUp(t, deps, "a", "Turing Tumble")

	s := open(t, srv, "/bigscreen/stream")
	waitFor(t, s, "The Gopher")

	stageSignup(t, deps, "a")
	waitFor(t, s, "Turing Tumble")
	if strings.Contains(s.String(), "@a") {
		t.Errorf("before the reveal the stream must not carry the handle")
	}

	if _, err := deps.showtell.RevealIdentity(signupID(t, deps, "a")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, "@a")
}

// TestReset_ClearsTheShowTellSignups covers the reset: the sign-ups go with
// everything else, in the same transaction.
func TestReset_ClearsTheShowTellSignups(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	signUp(t, deps, "a", "A")

	if resp := post(t, srv, "/admin/reset", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("reset: want 303, got %d", resp.StatusCode)
	}
	if queue, _ := deps.showtell.Queue(); len(queue) != 0 {
		t.Errorf("the sign-ups should be cleared, got %v", queue)
	}
}

// TestReset_ReportsAShowTellSignupsDeleteFailure covers the reset's show &
// tell statement: one transaction, so a failure leaves everything as it was.
func TestReset_ReportsAShowTellSignupsDeleteFailure(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	signUp(t, deps, "a", "A")
	breakOnFault(t, deps.db, "reset_showtell_signups")

	if err := deps.events.Reset(); err == nil {
		t.Fatal("want the failure reported")
	}
	if queue, _ := deps.showtell.Queue(); len(queue) != 1 {
		t.Errorf("the sign-ups should be untouched, got %v", queue)
	}
	if deps.events.State() != PhaseShowTell {
		t.Errorf("the phase should be untouched, got %s", deps.events.State())
	}
}

// TestDeleteParticipant_RemovesTheShowTellSignup covers the cleanup through
// the debug delete: the sign-up goes with the Participant.
func TestDeleteParticipant_RemovesTheShowTellSignup(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	signUp(t, deps, "a", "A")

	if resp := del(t, srv, "/data/participant/a"); resp.StatusCode != 200 {
		t.Fatalf("delete: want 200, got %d", resp.StatusCode)
	}
	if queue, _ := deps.showtell.Queue(); len(queue) != 0 {
		t.Errorf("the sign-up should be gone, got %v", queue)
	}

	seed(t, deps.db, "b", "B", "ready")
	signUp(t, deps, "b", "B")
	breakOnFault(t, deps.db, "showtell_delete")
	if resp := del(t, srv, "/data/participant/b"); resp.StatusCode != 500 {
		t.Errorf("a sign-up delete failure: want 500, got %d", resp.StatusCode)
	}
}
