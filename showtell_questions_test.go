package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// This file covers ticket #121's audience questions: the showtell_questions
// store (submission bound to the on-stage sign-up, the host's asked → answered
// / dismissed transitions), the submission route, the host console's question
// queue, the Big Screen's current question, the push streams, the recap's show
// & tell items, and the cleanup (reset, participant delete).

// askQuestion submits the Participant's question through the store.
func askQuestion(t *testing.T, deps *testDeps, askerID, text string) {
	t.Helper()
	if err := deps.showtell.Ask(reload(t, deps.db, askerID), text); err != nil {
		t.Fatal(err)
	}
}

// questionID returns the id of the Participant's nth question (0-based), in
// submission order.
func questionID(t *testing.T, deps *testDeps, participantID string, n int) int64 {
	t.Helper()
	questions, err := deps.showtell.QuestionsForParticipant(participantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(questions) <= n {
		t.Fatalf("%s has %d questions, want at least %d", participantID, len(questions), n+1)
	}
	return questions[n].ID
}

// stagePresenter signs the Participant up for show & tell and puts them on
// stage.
func stagePresenter(t *testing.T, deps *testDeps, participantID string) *ShowTellSignup {
	t.Helper()
	signUp(t, deps, participantID, "Turing Tumble")
	return stageSignup(t, deps, participantID)
}

// lastPayload parses the last data line of an SSE stream body.
func lastPayload(t *testing.T, body string) map[string]any {
	t.Helper()
	lines := strings.Split(body, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if payload, ok := strings.CutPrefix(lines[i], "data: "); ok {
			var data map[string]any
			if err := json.Unmarshal([]byte(payload), &data); err != nil {
				t.Fatalf("the last payload is not JSON: %v", err)
			}
			return data
		}
	}
	t.Fatal("no data line in the stream")
	return nil
}

// TestShowTell_AskIsValidatedAndPhaseGated covers the question's guards: a
// question needs 1–300 characters, and asking is live during show & tell
// only, while someone is on stage.
func TestShowTell_AskIsValidatedAndPhaseGated(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	p := reload(t, deps.db, "b")

	if err := deps.showtell.Ask(p, ""); !errors.Is(err, ErrInvalidQuestion) {
		t.Errorf("empty question: want ErrInvalidQuestion, got %v", err)
	}
	if err := deps.showtell.Ask(p, "   "); !errors.Is(err, ErrInvalidQuestion) {
		t.Errorf("blank question: want ErrInvalidQuestion, got %v", err)
	}
	if err := deps.showtell.Ask(p, strings.Repeat("x", maxQuestionLength+1)); !errors.Is(err, ErrInvalidQuestion) {
		t.Errorf("an over-long question: want ErrInvalidQuestion, got %v", err)
	}
	if err := deps.showtell.Ask(p, "Why?"); !errors.Is(err, ErrShowTellClosed) {
		t.Errorf("during onboarding: want ErrShowTellClosed, got %v", err)
	}
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	if err := deps.showtell.Ask(p, "Why?"); !errors.Is(err, ErrShowTellClosed) {
		t.Errorf("during the social: want ErrShowTellClosed, got %v", err)
	}
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	if err := deps.showtell.Ask(p, "Why?"); !errors.Is(err, ErrNoPresenter) {
		t.Errorf("with an empty stage: want ErrNoPresenter, got %v", err)
	}
	if err := deps.events.Advance(PhaseClosed); err != nil {
		t.Fatal(err)
	}
	if err := deps.showtell.Ask(p, "Why?"); !errors.Is(err, ErrShowTellClosed) {
		t.Errorf("once closed: want ErrShowTellClosed, got %v", err)
	}
}

// TestShowTell_AskBindsToTheOnStageSignup covers the question's binding: it
// binds to the sign-up currently on stage, renders through the asker's
// persona, and follows the stage.
func TestShowTell_AskBindsToTheOnStageSignup(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	seed(t, deps.db, "c", "The Snake", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	presenter := stagePresenter(t, deps, "a")

	askQuestion(t, deps, "b", "Why tabs?")
	askQuestion(t, deps, "c", strings.Repeat("x", maxQuestionLength)) // the boundary is allowed

	questions, err := deps.showtell.Questions()
	if err != nil {
		t.Fatal(err)
	}
	if len(questions) != 2 {
		t.Fatalf("questions: want 2, got %d", len(questions))
	}
	for i, want := range []struct{ asker, text string }{
		{"b", "Why tabs?"},
		{"c", strings.Repeat("x", maxQuestionLength)},
	} {
		q := questions[i]
		if q.ParticipantID != want.asker || q.Text != want.text || q.SignupID != presenter.ID || q.Status != QuestionQueued {
			t.Errorf("questions[%d]: got %+v, want %s's question bound to sign-up %d", i, q, want.asker, presenter.ID)
		}
	}
	// The asker's Persona is joined — questions render through personas only.
	if questions[0].PersonaName != "The Crab" || questions[0].PersonaSymbol == "" || questions[0].PersonaColor == "" {
		t.Errorf("questions[0] should carry the asker's Persona, got %+v", questions[0])
	}
	// The question is announced on the ticker, through the persona.
	msgs, _ := deps.db.GetRecentActivity(10)
	if !strings.Contains(strings.Join(msgs, "\n"), `The Crab asks: "Why tabs?"`) {
		t.Errorf("the question should be announced, got %v", msgs)
	}

	// The binding follows the stage: once the talk is done, new questions
	// bind to the next presenter.
	if _, err := deps.showtell.Done(presenter.ID); err != nil {
		t.Fatal(err)
	}
	if err := deps.showtell.Ask(reload(t, deps.db, "b"), "Why?"); !errors.Is(err, ErrNoPresenter) {
		t.Errorf("with an empty stage: want ErrNoPresenter, got %v", err)
	}
	next := stagePresenter(t, deps, "b")
	askQuestion(t, deps, "c", "Second talk, who dis?")
	if questions, err = deps.showtell.Questions(); err != nil {
		t.Fatal(err)
	}
	if len(questions) != 3 || questions[2].SignupID != next.ID {
		t.Errorf("the new question should bind to the new presenter, got %+v", questions)
	}
}

// TestShowTell_AskReportsAFault covers the question submission's failures: a
// broken insert and a broken stage read.
func TestShowTell_AskReportsAFault(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	stagePresenter(t, deps, "a")

	breakOnFault(t, deps.db, "showtell_question_insert")
	if err := deps.showtell.Ask(reload(t, deps.db, "b"), "Why?"); err == nil {
		t.Error("a broken insert: want the failure reported")
	}
	breakOnFault(t, deps.db, "showtell_read")
	if err := deps.showtell.Ask(reload(t, deps.db, "b"), "Why?"); err == nil {
		t.Error("a broken stage read: want the failure reported")
	}
}

// TestShowTell_QuestionReads covers the question store's reads: the queue in
// submission order, by id, the current question, and the Participant's own.
func TestShowTell_QuestionReads(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	seed(t, deps.db, "c", "The Snake", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}

	if questions, err := deps.showtell.Questions(); err != nil || len(questions) != 0 {
		t.Errorf("an empty queue: want no questions, got %v, %v", questions, err)
	}
	if q, err := deps.showtell.QuestionByID(4242); err != nil || q != nil {
		t.Errorf("unknown id: want nil, nil — got %v, %v", q, err)
	}
	if q, err := deps.showtell.CurrentQuestion(); err != nil || q != nil {
		t.Errorf("nothing asked: want nil, nil — got %v, %v", q, err)
	}
	if questions, err := deps.showtell.QuestionsForParticipant("b"); err != nil || len(questions) != 0 {
		t.Errorf("no questions yet: want none, got %v, %v", questions, err)
	}

	stagePresenter(t, deps, "a")
	askQuestion(t, deps, "b", "First")
	askQuestion(t, deps, "c", "Second")
	askQuestion(t, deps, "b", "Third")

	questions, err := deps.showtell.Questions()
	if err != nil {
		t.Fatal(err)
	}
	if len(questions) != 3 {
		t.Fatalf("questions: want 3, got %d", len(questions))
	}
	for i, want := range []string{"First", "Second", "Third"} {
		if questions[i].Text != want {
			t.Errorf("questions[%d]: want %q, got %q", i, want, questions[i].Text)
		}
	}
	if q, err := deps.showtell.QuestionByID(questions[1].ID); err != nil || q == nil || q.Text != "Second" {
		t.Errorf("QuestionByID: got %v, %v", q, err)
	}
	mine, err := deps.showtell.QuestionsForParticipant("b")
	if err != nil || len(mine) != 2 {
		t.Errorf("b's questions: want 2, got %v, %v", mine, err)
	} else if mine[0].Text != "First" || mine[1].Text != "Third" {
		t.Errorf("b's questions in submission order: got %v", mine)
	}
	if _, err := deps.showtell.TransitionQuestion(mine[0].ID, QuestionAsked); err != nil {
		t.Fatal(err)
	}
	current, err := deps.showtell.CurrentQuestion()
	if err != nil || current == nil || current.Text != "First" {
		t.Errorf("CurrentQuestion: got %v, %v", current, err)
	}
}

// TestShowTell_QuestionReadsReportAFault covers the question reads' failures.
func TestShowTell_QuestionReadsReportAFault(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	stagePresenter(t, deps, "a")
	askQuestion(t, deps, "b", "Why?")
	// CurrentQuestion only scans a row while a question is being asked.
	if _, err := deps.showtell.TransitionQuestion(1, QuestionAsked); err != nil {
		t.Fatal(err)
	}

	breakOnFault(t, deps.db, "showtell_question_read")
	if _, err := deps.showtell.Questions(); err == nil {
		t.Error("Questions: want the read failure reported")
	}
	if _, err := deps.showtell.QuestionByID(1); err == nil {
		t.Error("QuestionByID: want the read failure reported")
	}
	if _, err := deps.showtell.CurrentQuestion(); err == nil {
		t.Error("CurrentQuestion: want the read failure reported")
	}
	if _, err := deps.showtell.QuestionsForParticipant("b"); err == nil {
		t.Error("QuestionsForParticipant: want the read failure reported")
	}
	breakDB(deps.db)
	if _, err := deps.showtell.Questions(); err == nil {
		t.Error("Questions on a broken database: want the query failure reported")
	}
}

// TestShowTell_TransitionQuestionMovesTheQuestionThroughTheQueue covers the
// host's question-queue transitions: queued → asked (one current question on
// the Big Screen at a time) → answered, or dismissed from queued or asked;
// answered and dismissed are final.
func TestShowTell_TransitionQuestionMovesTheQuestionThroughTheQueue(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	seed(t, deps.db, "c", "The Snake", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	stagePresenter(t, deps, "a")
	askQuestion(t, deps, "b", "First")
	askQuestion(t, deps, "c", "Second")
	first := questionID(t, deps, "b", 0)
	second := questionID(t, deps, "c", 0)

	if _, err := deps.showtell.TransitionQuestion(4242, QuestionAsked); !errors.Is(err, ErrQuestionNotFound) {
		t.Errorf("unknown id: want ErrQuestionNotFound, got %v", err)
	}
	if _, err := deps.showtell.TransitionQuestion(first, QuestionAnswered); !errors.Is(err, ErrInvalidQuestionTransition) {
		t.Errorf("answering a queued question: want ErrInvalidQuestionTransition, got %v", err)
	}

	asked, err := deps.showtell.TransitionQuestion(first, QuestionAsked)
	if err != nil {
		t.Fatal(err)
	}
	if asked.Status != QuestionAsked {
		t.Errorf("the returned question should carry the new status, got %+v", asked)
	}
	if current, _ := deps.showtell.CurrentQuestion(); current == nil || current.ID != first {
		t.Errorf("the current question should be the asked one, got %+v", current)
	}
	msgs, _ := deps.db.GetRecentActivity(10)
	if !strings.Contains(strings.Join(msgs, "\n"), "On the big screen") {
		t.Errorf("asking should be announced, got %v", msgs)
	}

	// One current question at a time.
	if _, err := deps.showtell.TransitionQuestion(second, QuestionAsked); !errors.Is(err, ErrAlreadyAsked) {
		t.Errorf("asking while a question is on the big screen: want ErrAlreadyAsked, got %v", err)
	}
	if _, err := deps.showtell.TransitionQuestion(first, QuestionAsked); !errors.Is(err, ErrAlreadyAsked) {
		t.Errorf("asking the asked question again: want ErrAlreadyAsked, got %v", err)
	}

	answered, err := deps.showtell.TransitionQuestion(first, QuestionAnswered)
	if err != nil {
		t.Fatal(err)
	}
	if answered.Status != QuestionAnswered {
		t.Errorf("status: want answered, got %s", answered.Status)
	}
	if current, _ := deps.showtell.CurrentQuestion(); current != nil {
		t.Errorf("the big screen should be clear, got %+v", current)
	}
	// Answered is final.
	if _, err := deps.showtell.TransitionQuestion(first, QuestionDismissed); !errors.Is(err, ErrInvalidQuestionTransition) {
		t.Errorf("dismissing an answered question: want ErrInvalidQuestionTransition, got %v", err)
	}
	if _, err := deps.showtell.TransitionQuestion(first, QuestionAsked); !errors.Is(err, ErrInvalidQuestionTransition) {
		t.Errorf("re-asking an answered question: want ErrInvalidQuestionTransition, got %v", err)
	}

	// Dismissed, from queued and from asked; dismissed is final.
	if _, err := deps.showtell.TransitionQuestion(second, QuestionDismissed); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.showtell.TransitionQuestion(second, QuestionAsked); !errors.Is(err, ErrInvalidQuestionTransition) {
		t.Errorf("asking a dismissed question: want ErrInvalidQuestionTransition, got %v", err)
	}
	askQuestion(t, deps, "b", "Third")
	third := questionID(t, deps, "b", 1)
	if _, err := deps.showtell.TransitionQuestion(third, QuestionAsked); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.showtell.TransitionQuestion(third, QuestionDismissed); err != nil {
		t.Fatal(err)
	}
	if q, _ := deps.showtell.QuestionByID(third); q == nil || q.Status != QuestionDismissed {
		t.Errorf("the question should be dismissed, got %+v", q)
	}
	msgs, _ = deps.db.GetRecentActivity(10)
	for _, want := range []string{"Answered:", "Dismissed:"} {
		if !strings.Contains(strings.Join(msgs, "\n"), want) {
			t.Errorf("the transition should be announced (%q), got %v", want, msgs)
		}
	}

	// The queue is a show & tell gain: once closed, no transitions.
	if err := deps.events.Advance(PhaseClosed); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.showtell.TransitionQuestion(first, QuestionAnswered); !errors.Is(err, ErrShowTellClosed) {
		t.Errorf("once closed: want ErrShowTellClosed, got %v", err)
	}
}

// TestShowTell_TransitionQuestionReportsAFault covers the transition's
// failures: a broken write and a broken read.
func TestShowTell_TransitionQuestionReportsAFault(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	stagePresenter(t, deps, "a")
	askQuestion(t, deps, "b", "Why?")
	askQuestion(t, deps, "b", "Why not?")
	asked := questionID(t, deps, "b", 0)
	queued := questionID(t, deps, "b", 1)
	// One question on the Big Screen, so the current-question read scans a
	// row when the read is broken.
	if _, err := deps.showtell.TransitionQuestion(asked, QuestionAsked); err != nil {
		t.Fatal(err)
	}

	breakOnFault(t, deps.db, "showtell_question_update")
	if _, err := deps.showtell.TransitionQuestion(asked, QuestionAnswered); err == nil {
		t.Error("a broken write: want the failure reported")
	}
	breakOnFault(t, deps.db, "showtell_question_read")
	if _, err := deps.showtell.TransitionQuestion(queued, QuestionAsked); err == nil {
		t.Error("a broken current-question read: want the failure reported")
	}
	if _, err := deps.showtell.TransitionQuestion(queued, QuestionAnswered); err == nil {
		t.Error("a broken question read: want the failure reported")
	}
}

// TestShowTell_RemoveTakesTheQuestionsAboutTheTalk covers the cleanup: taking
// a sign-up out of the queue takes the questions asked about it — and only
// those.
func TestShowTell_RemoveTakesTheQuestionsAboutTheTalk(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	seed(t, deps.db, "c", "The Snake", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	first := stagePresenter(t, deps, "a")
	askQuestion(t, deps, "b", "About the first talk")
	if _, err := deps.showtell.Done(first.ID); err != nil {
		t.Fatal(err)
	}
	second := stagePresenter(t, deps, "b")
	askQuestion(t, deps, "c", "About the second talk")

	if err := deps.showtell.Remove(first.ID); err != nil {
		t.Fatal(err)
	}
	questions, err := deps.showtell.Questions()
	if err != nil {
		t.Fatal(err)
	}
	if len(questions) != 1 || questions[0].Text != "About the second talk" || questions[0].SignupID != second.ID {
		t.Errorf("only the removed talk's questions go with it, got %+v", questions)
	}

	breakOnFault(t, deps.db, "showtell_question_delete")
	if err := deps.showtell.Remove(second.ID); err == nil {
		t.Error("want the questions' delete failure reported")
	}
	if signup, _ := deps.showtell.ByID(second.ID); signup == nil {
		t.Error("the transaction rolled back: the sign-up stays")
	}
}

// TestShowTell_RemoveParticipantTakesTheirQuestions covers the cleanup when a
// Participant leaves: their questions as an asker, and the questions their
// talk drew, go with them.
func TestShowTell_RemoveParticipantTakesTheirQuestions(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	seed(t, deps.db, "c", "The Snake", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	stagePresenter(t, deps, "a")
	askQuestion(t, deps, "b", "b's question")
	askQuestion(t, deps, "c", "c's question")

	// The asker leaves: their questions go.
	if err := deps.showtell.RemoveParticipant("b"); err != nil {
		t.Fatal(err)
	}
	questions, err := deps.showtell.Questions()
	if err != nil {
		t.Fatal(err)
	}
	if len(questions) != 1 || questions[0].ParticipantID != "c" {
		t.Errorf("the asker's questions should be gone, got %+v", questions)
	}

	// The presenter leaves: their talk and the questions it drew go.
	if err := deps.showtell.RemoveParticipant("a"); err != nil {
		t.Fatal(err)
	}
	if questions, _ := deps.showtell.Questions(); len(questions) != 0 {
		t.Errorf("the talk's questions should be gone, got %+v", questions)
	}
	if signup, _ := deps.showtell.ForParticipant("a"); signup != nil {
		t.Errorf("the sign-up should be gone, got %+v", signup)
	}

	stagePresenter(t, deps, "c")
	askQuestion(t, deps, "b", "Another question")
	breakOnFault(t, deps.db, "showtell_question_delete")
	if err := deps.showtell.RemoveParticipant("c"); err == nil {
		t.Error("want the questions' delete failure reported")
	}
}

// TestShowTell_SignatureWatchesTheQuestionsAndTheStage covers what the
// personal page's stream watches beyond the sign-up: the Participant's
// questions and who is on stage, because the question form depends on the
// presenter.
func TestShowTell_SignatureWatchesTheQuestionsAndTheStage(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}

	if got := deps.showtell.Signature("b"); got != "" {
		t.Errorf("nothing yet: want an empty signature, got %q", got)
	}
	stagePresenter(t, deps, "a")
	if got := deps.showtell.Signature("b"); !strings.Contains(got, "stage:") {
		t.Errorf("the signature should name the presenter on stage, got %q", got)
	}
	askQuestion(t, deps, "b", "Why?")
	if got := deps.showtell.Signature("b"); !strings.Contains(got, "queued") {
		t.Errorf("the signature should track the question's status, got %q", got)
	}
	if _, err := deps.showtell.TransitionQuestion(questionID(t, deps, "b", 0), QuestionAsked); err != nil {
		t.Fatal(err)
	}
	if got := deps.showtell.Signature("b"); !strings.Contains(got, "asked") {
		t.Errorf("the signature should track the question being asked, got %q", got)
	}
	// Another participant's question does not move b's signature.
	before := deps.showtell.Signature("b")
	askQuestion(t, deps, "a", "The presenter asks too")
	if got := deps.showtell.Signature("b"); got != before {
		t.Errorf("another participant's question should not move b's signature, got %q", got)
	}
}

// TestShowTellQuestion_Route covers the audience-question submission route:
// validation, phase gating, the empty stage, and the fragment it answers with.
func TestShowTellQuestion_Route(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")

	ask := func(form url.Values) *http.Response {
		return post(t, srv, "/user/showtell/question/b", form)
	}
	if resp := post(t, srv, "/user/showtell/question/nobody", url.Values{"question": {"Why?"}}); resp.StatusCode != 404 {
		t.Errorf("unknown participant: want 404, got %d", resp.StatusCode)
	}
	if resp := ask(url.Values{"question": {"Why?"}}); resp.StatusCode != 400 {
		t.Errorf("during onboarding: want 400, got %d", resp.StatusCode)
	}
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	if resp := ask(url.Values{"question": {"Why?"}}); resp.StatusCode != 400 {
		t.Errorf("during the social: want 400, got %d", resp.StatusCode)
	}
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	if resp := ask(url.Values{"question": {"Why?"}}); resp.StatusCode != 400 {
		t.Errorf("with an empty stage: want 400, got %d", resp.StatusCode)
	}
	stagePresenter(t, deps, "a")
	if resp := ask(url.Values{"question": {"  "}}); resp.StatusCode != 400 {
		t.Errorf("no question: want 400, got %d", resp.StatusCode)
	}
	if resp := ask(url.Values{"question": {strings.Repeat("x", maxQuestionLength+1)}}); resp.StatusCode != 400 {
		t.Errorf("an over-long question: want 400, got %d", resp.StatusCode)
	}

	body := readBody(t, ask(url.Values{"question": {"Why tabs?"}}))
	for _, want := range []string{"Why tabs?", "Queued — the host picks it up", `hx-post="/user/showtell/question/b"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the question fragment should contain %q, got:\n%s", want, body)
		}
	}
	questions, err := deps.showtell.QuestionsForParticipant("b")
	if err != nil || len(questions) != 1 || questions[0].Text != "Why tabs?" {
		t.Errorf("the question should be stored, got %v, %v", questions, err)
	}

	breakOnFault(t, deps.db, "showtell_question_insert")
	if resp := ask(url.Values{"question": {"Why?"}}); resp.StatusCode != 500 {
		t.Errorf("a write failure: want 500, got %d", resp.StatusCode)
	}
}

// TestShowTellQuestionAction_Route covers the host's question-queue route:
// the three actions and every rejection.
func TestShowTellQuestionAction_Route(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	stagePresenter(t, deps, "a")
	askQuestion(t, deps, "b", "Why tabs?")
	id := questionID(t, deps, "b", 0)

	action := func(form url.Values) *http.Response {
		return post(t, srv, "/admin/showtell/question", form)
	}
	if resp := action(url.Values{"id": {"not-a-number"}, "action": {"asked"}}); resp.StatusCode != 400 {
		t.Errorf("a malformed id: want 400, got %d", resp.StatusCode)
	}
	if resp := action(url.Values{"id": {idValue(id)}, "action": {"explode"}}); resp.StatusCode != 400 {
		t.Errorf("an unknown action: want 400, got %d", resp.StatusCode)
	}
	if resp := action(url.Values{"id": {"4242"}, "action": {"asked"}}); resp.StatusCode != 404 {
		t.Errorf("an unknown question: want 404, got %d", resp.StatusCode)
	}
	if resp := action(url.Values{"id": {idValue(id)}, "action": {"answered"}}); resp.StatusCode != 400 {
		t.Errorf("answering a queued question: want 400, got %d", resp.StatusCode)
	}
	if resp := action(url.Values{"id": {idValue(id)}, "action": {"asked"}}); resp.StatusCode != 303 {
		t.Errorf("asking: want a redirect, got %d", resp.StatusCode)
	}
	if q, _ := deps.showtell.QuestionByID(id); q == nil || q.Status != QuestionAsked {
		t.Errorf("the question should be asked, got %+v", q)
	}
	if resp := action(url.Values{"id": {idValue(id)}, "action": {"asked"}}); resp.StatusCode != 400 {
		t.Errorf("asking while a question is on the big screen: want 400, got %d", resp.StatusCode)
	}
	if resp := action(url.Values{"id": {idValue(id)}, "action": {"answered"}}); resp.StatusCode != 303 {
		t.Errorf("answering: want a redirect, got %d", resp.StatusCode)
	}
	if resp := action(url.Values{"id": {idValue(id)}, "action": {"dismissed"}}); resp.StatusCode != 400 {
		t.Errorf("dismissing an answered question: want 400, got %d", resp.StatusCode)
	}

	askQuestion(t, deps, "b", "One more")
	second := questionID(t, deps, "b", 1)
	if resp := action(url.Values{"id": {idValue(second)}, "action": {"dismissed"}}); resp.StatusCode != 303 {
		t.Errorf("dismissing: want a redirect, got %d", resp.StatusCode)
	}
	if resp := action(url.Values{"id": {idValue(second)}, "action": {"asked"}}); resp.StatusCode != 400 {
		t.Errorf("asking a dismissed question: want 400, got %d", resp.StatusCode)
	}

	askQuestion(t, deps, "b", "A third")
	third := questionID(t, deps, "b", 2)
	breakOnFault(t, deps.db, "showtell_question_update")
	if resp := action(url.Values{"id": {idValue(third)}, "action": {"asked"}}); resp.StatusCode != 500 {
		t.Errorf("a write failure: want 500, got %d", resp.StatusCode)
	}

	if err := deps.events.Advance(PhaseClosed); err != nil {
		t.Fatal(err)
	}
	if resp := action(url.Values{"id": {idValue(third)}, "action": {"asked"}}); resp.StatusCode != 400 {
		t.Errorf("once closed: want 400, got %d", resp.StatusCode)
	}
}

// TestShowTellQuestions_Fragment covers the host console's question queue.
func TestShowTellQuestions_Fragment(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	stagePresenter(t, deps, "a")

	if body := readBody(t, get(t, srv, "/admin/showtell-questions")); !strings.Contains(body, "No questions yet") {
		t.Errorf("an empty queue should say so, got:\n%s", body)
	}

	askQuestion(t, deps, "b", "Why tabs?")
	askQuestion(t, deps, "b", "And why not?")
	body := readBody(t, get(t, srv, "/admin/showtell-questions"))
	for _, want := range []string{"Why tabs?", "The Crab asks", "queued", `value="asked"`, `value="dismissed"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the question queue should show %q, got:\n%s", want, body)
		}
	}
	if strings.Contains(body, "@b") {
		t.Errorf("questions render with the asker's persona only, got:\n%s", body)
	}

	// Asked: on the big screen, with answered and dismiss.
	if _, err := deps.showtell.TransitionQuestion(questionID(t, deps, "b", 0), QuestionAsked); err != nil {
		t.Fatal(err)
	}
	body = readBody(t, get(t, srv, "/admin/showtell-questions"))
	for _, want := range []string{"on the big screen", `value="answered"`} {
		if !strings.Contains(body, want) {
			t.Errorf("an asked question should show %q, got:\n%s", want, body)
		}
	}
	if got := strings.Count(body, `value="asked"`); got != 1 {
		t.Errorf("only the queued question should offer asking, got %d, got:\n%s", got, body)
	}

	// Answered and dismissed are final: no actions left.
	if _, err := deps.showtell.TransitionQuestion(questionID(t, deps, "b", 0), QuestionAnswered); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.showtell.TransitionQuestion(questionID(t, deps, "b", 1), QuestionDismissed); err != nil {
		t.Fatal(err)
	}
	body = readBody(t, get(t, srv, "/admin/showtell-questions"))
	for _, want := range []string{"answered", "dismissed"} {
		if !strings.Contains(body, want) {
			t.Errorf("a final question should show %q, got:\n%s", want, body)
		}
	}
	if strings.Contains(body, `action="/admin/showtell/question"`) {
		t.Errorf("final questions offer no actions, got:\n%s", body)
	}

	// A read failure still renders the section.
	breakOnFault(t, deps.db, "showtell_question_read")
	if resp := get(t, srv, "/admin/showtell-questions"); resp.StatusCode != 200 {
		t.Errorf("a read failure still renders the queue, got %d", resp.StatusCode)
	}
}

// TestAdmin_ShowsTheAudienceQuestions covers the host console's question
// section: live during show & tell, gone before it.
func TestAdmin_ShowsTheAudienceQuestions(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")

	if body := readBody(t, get(t, srv, "/admin")); strings.Contains(body, `id="showtell-questions"`) {
		t.Errorf("during onboarding the host console should not show the question queue")
	}
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	if body := readBody(t, get(t, srv, "/admin")); strings.Contains(body, `id="showtell-questions"`) {
		t.Errorf("during the social the host console should not show the question queue")
	}

	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	stagePresenter(t, deps, "a")
	askQuestion(t, deps, "b", "Why tabs?")
	body := readBody(t, get(t, srv, "/admin"))
	for _, want := range []string{`id="showtell-questions"`, "Audience questions", "Why tabs?", "The Crab asks", "/admin/showtell-stream"} {
		if !strings.Contains(body, want) {
			t.Errorf("the host console should show %q, got:\n%s", want, body)
		}
	}
}

// TestShowTellQuestionsSignature covers what the queue stream watches for the
// question queue.
func TestShowTellQuestionsSignature(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}

	if got := deps.handler.showtellQuestionsSignature(); got != "" {
		t.Errorf("an empty queue: want an empty signature, got %q", got)
	}
	stagePresenter(t, deps, "a")
	askQuestion(t, deps, "b", "Why?")
	if got := deps.handler.showtellQuestionsSignature(); !strings.Contains(got, "queued") {
		t.Errorf("a question should change the signature, got %q", got)
	}
	breakOnFault(t, deps.db, "showtell_question_read")
	if got := deps.handler.showtellQuestionsSignature(); got != "" {
		t.Errorf("a read failure: want an empty signature, got %q", got)
	}
}

// TestShowTellStream_PushesTheQuestionQueue covers the host console's stream
// for the question queue: a refresh when a question changes, silence
// otherwise.
func TestShowTellStream_PushesTheQuestionQueue(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	stagePresenter(t, deps, "a")

	s := open(t, srv, "/admin/showtell-stream")
	sentNothingNew(t, s, 1, "", "an unchanged question queue")

	askQuestion(t, deps, "b", "Why?")
	waitFor(t, s, "event: refresh")

	before := s.String()
	deps.db.LogActivity("someone else did something")
	sentNothingNew(t, s, 3, before, "a change that does not touch the queues")
}

// TestPersonalPages_AskFormIsDisabledUntilSomeoneIsOnStage covers the
// question form on the personal page: live during show & tell, bound to the
// presenter on stage, its button disabled while the stage is empty.
func TestPersonalPages_AskFormIsDisabledUntilSomeoneIsOnStage(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")

	// Before show & tell there is no question form.
	if body := readBody(t, get(t, srv, "/user/wait/b")); strings.Contains(body, "/user/showtell/question/b") {
		t.Errorf("during onboarding the wait page should not show the question form")
	}
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	if body := readBody(t, get(t, srv, "/user/wait/b")); strings.Contains(body, "/user/showtell/question/b") {
		t.Errorf("during the social the wait page should not show the question form")
	}

	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	body := readBody(t, get(t, srv, "/user/wait/b"))
	for _, want := range []string{`hx-post="/user/showtell/question/b"`, "The stage is empty"} {
		if !strings.Contains(body, want) {
			t.Errorf("with an empty stage the wait page should show %q, got:\n%s", want, body)
		}
	}
	if !strings.Contains(body, "disabled\n      >\n        Ask the presenter") {
		t.Errorf("with an empty stage the submit button should be disabled, got:\n%s", body)
	}

	// Once someone is on stage the form binds to them.
	stagePresenter(t, deps, "a")
	body = readBody(t, get(t, srv, "/user/wait/b"))
	for _, want := range []string{"The Gopher", "is on stage — “Turing Tumble”"} {
		if !strings.Contains(body, want) {
			t.Errorf("with a presenter on stage the wait page should show %q, got:\n%s", want, body)
		}
	}
	if strings.Contains(body, "disabled\n      >\n        Ask the presenter") {
		t.Errorf("with a presenter on stage the submit button should be enabled, got:\n%s", body)
	}

	// The asker's own questions list with their status.
	askQuestion(t, deps, "b", "Why tabs?")
	if body := readBody(t, get(t, srv, "/user/wait/b")); !strings.Contains(body, "“Why tabs?” — ⏳ queued") {
		t.Errorf("the asker should see their question queued, got:\n%s", body)
	}

	// The Match page carries the same form (once paired, the wait page
	// redirects there).
	pair(t, deps.db, "a", "b")
	if body := readBody(t, get(t, srv, "/user/match/b")); !strings.Contains(body, "/user/showtell/question/b") {
		t.Errorf("during show & tell the Match page should show the question form")
	}
}

// TestShowTellStatus_FragmentTracksTheQuestions covers the personal page's
// section listing the Participant's questions with their status.
func TestShowTellStatus_FragmentTracksTheQuestions(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	stagePresenter(t, deps, "a")

	// Without questions there is no list.
	if body := readBody(t, get(t, srv, "/user/showtell/b")); strings.Contains(body, "“Why tabs?”") {
		t.Errorf("no questions yet: no list, got:\n%s", body)
	}
	askQuestion(t, deps, "b", "Why tabs?")
	if body := readBody(t, get(t, srv, "/user/showtell/b")); !strings.Contains(body, "“Why tabs?” — ⏳ queued") {
		t.Errorf("a queued question should show its status, got:\n%s", body)
	}
	if _, err := deps.showtell.TransitionQuestion(questionID(t, deps, "b", 0), QuestionAsked); err != nil {
		t.Fatal(err)
	}
	if body := readBody(t, get(t, srv, "/user/showtell/b")); !strings.Contains(body, "being asked on the big screen") {
		t.Errorf("an asked question should say so, got:\n%s", body)
	}
	if _, err := deps.showtell.TransitionQuestion(questionID(t, deps, "b", 0), QuestionAnswered); err != nil {
		t.Fatal(err)
	}
	if body := readBody(t, get(t, srv, "/user/showtell/b")); !strings.Contains(body, "✅ answered") {
		t.Errorf("an answered question should say so, got:\n%s", body)
	}

	// A read failure still renders the section.
	breakOnFault(t, deps.db, "showtell_question_read")
	if resp := get(t, srv, "/user/showtell/b"); resp.StatusCode != 200 {
		t.Errorf("a read failure still renders the section, got %d", resp.StatusCode)
	}
}

// TestShowTellStatus_ThePresenterReadFails covers the personal page's section
// against a broken on-stage read: the form falls back to an empty stage.
func TestShowTellStatus_ThePresenterReadFails(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	stagePresenter(t, deps, "a")

	breakOnFault(t, deps.db, "showtell_read")
	resp := get(t, srv, "/user/showtell/b")
	if resp.StatusCode != 200 {
		t.Fatalf("a broken presenter read still renders the section, got %d", resp.StatusCode)
	}
	if body := readBody(t, resp); !strings.Contains(body, "The stage is empty") {
		t.Errorf("without the presenter the form should show an empty stage, got:\n%s", body)
	}
}

// TestWaitStream_RefreshesTheQuestionFormWhenTheStageChanges covers the
// personal page's stream: the question form depends on who is on stage, so
// staging a presenter refreshes every page — and so does the Participant's
// own question changing status.
func TestWaitStream_RefreshesTheQuestionFormWhenTheStageChanges(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}

	s := open(t, srv, "/user/wait-stream/b")
	sentNothingNew(t, s, 1, "", "nothing changed")

	stagePresenter(t, deps, "a")
	waitFor(t, s, "event: refresh")

	before := s.String()
	askQuestion(t, deps, "b", "Why?")
	eventually(t, "the question to refresh the section", func() bool {
		return len(s.String()) > len(before)
	})
}

// TestScreenStream_PushesTheCurrentQuestion covers the Big Screen's stream:
// the current question arrives by push when the host asks it, and leaves on
// the next push when it is answered.
func TestScreenStream_PushesTheCurrentQuestion(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	stagePresenter(t, deps, "a")

	s := open(t, srv, "/bigscreen/stream")
	waitFor(t, s, "The Gopher")

	askQuestion(t, deps, "b", "Why tabs?")
	if _, err := deps.showtell.TransitionQuestion(questionID(t, deps, "b", 0), QuestionAsked); err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, `"text":"Why tabs?"`)
	if strings.Contains(s.String(), "@b") {
		t.Errorf("the Big Screen must not carry the asker's handle")
	}

	before := len(s.String())
	if _, err := deps.showtell.TransitionQuestion(questionID(t, deps, "b", 0), QuestionAnswered); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the answer to push a new payload", func() bool {
		return len(s.String()) > before
	})
	presenter, ok := lastPayload(t, s.String())["presenter"].(map[string]any)
	if !ok {
		t.Fatalf("the presenter should still be on stage, got %v", lastPayload(t, s.String()))
	}
	if _, ok := presenter["question"]; ok {
		t.Errorf("the answered question should leave the screen, got %v", presenter["question"])
	}
}

// TestPresenterPayload_CarriesTheCurrentQuestionWithTheAskersPersona covers
// the Big Screen's current question: the asker's persona and the text ride
// with the presenter card — personas only, never identities — and leave when
// the question is answered.
func TestPresenterPayload_CarriesTheCurrentQuestionWithTheAskersPersona(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	stagePresenter(t, deps, "a")

	// No question asked: no current question.
	if body := readBody(t, get(t, srv, "/bigscreen/graph-data")); strings.Contains(body, `"question":`) {
		t.Errorf("without an asked question there is no current question, got:\n%s", body)
	}

	askQuestion(t, deps, "b", "Why tabs?")
	if _, err := deps.showtell.TransitionQuestion(questionID(t, deps, "b", 0), QuestionAsked); err != nil {
		t.Fatal(err)
	}
	var graph map[string]any
	if err := json.Unmarshal([]byte(readBody(t, get(t, srv, "/bigscreen/graph-data"))), &graph); err != nil {
		t.Fatal(err)
	}
	presenter, ok := graph["presenter"].(map[string]any)
	if !ok {
		t.Fatalf("presenter mode: the payload should carry the presenter, got %v", graph)
	}
	question, ok := presenter["question"].(map[string]any)
	if !ok {
		t.Fatalf("the current question should ride with the presenter card, got %v", presenter)
	}
	if question["text"] != "Why tabs?" || question["persona_name"] != "The Crab" {
		t.Errorf("the current question should carry the text and the asker's persona, got %v", question)
	}
	if question["persona_symbol"] == "" || question["persona_color"] == "" {
		t.Errorf("the current question should carry the asker's persona symbol and colour, got %v", question)
	}
	if len(question) != 4 {
		t.Errorf("the current question carries the asker's persona only — never an identity, got %v", question)
	}
	if body := readBody(t, get(t, srv, "/bigscreen/graph-data")); strings.Contains(body, "@b") {
		t.Errorf("the Big Screen must not carry the asker's handle, got:\n%s", body)
	}

	// Answered: the current question leaves.
	if _, err := deps.showtell.TransitionQuestion(questionID(t, deps, "b", 0), QuestionAnswered); err != nil {
		t.Fatal(err)
	}
	if body := readBody(t, get(t, srv, "/bigscreen/graph-data")); strings.Contains(body, `"question":`) {
		t.Errorf("an answered question leaves the screen, got:\n%s", body)
	}

	// A broken question read drops the current question, not the presenter.
	askQuestion(t, deps, "b", "One more")
	if _, err := deps.showtell.TransitionQuestion(questionID(t, deps, "b", 1), QuestionAsked); err != nil {
		t.Fatal(err)
	}
	breakOnFault(t, deps.db, "showtell_question_read")
	body := readBody(t, get(t, srv, "/bigscreen/graph-data"))
	if !strings.Contains(body, `"presenter"`) {
		t.Errorf("the presenter should still be there, got:\n%s", body)
	}
	if strings.Contains(body, `"question":`) {
		t.Errorf("a broken question read drops the current question, got:\n%s", body)
	}
}

// TestQuestionFlow_SubmitQueueScreenAnswered is the acceptance flow end to
// end, through the routes: a question flows submit → host console → big
// screen → answered within one push cycle.
func TestQuestionFlow_SubmitQueueScreenAnswered(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")

	// The social: sign-up opens.
	post(t, srv, "/admin/phase", url.Values{"to": {"social"}})
	post(t, srv, "/user/showtell/signup/a", url.Values{"title": {"Turing Tumble"}})

	// Show & tell: the presenter takes the stage.
	post(t, srv, "/admin/phase", url.Values{"to": {"showtell"}})
	post(t, srv, "/admin/showtell/stage", url.Values{"id": {idValue(signupID(t, deps, "a"))}, "action": {"stage"}})

	// Submit: the question is queued, bound to the on-stage sign-up.
	if resp := post(t, srv, "/user/showtell/question/b", url.Values{"question": {"Why tabs?"}}); resp.StatusCode != 200 {
		t.Fatalf("submit: want 200, got %d", resp.StatusCode)
	}
	questions, err := deps.showtell.Questions()
	if err != nil || len(questions) != 1 {
		t.Fatalf("the question should be queued, got %v, %v", questions, err)
	}
	if questions[0].SignupID != signupID(t, deps, "a") {
		t.Errorf("the question should bind to the on-stage sign-up, got %+v", questions[0])
	}

	// The host console sees it, through the asker's persona.
	body := readBody(t, get(t, srv, "/admin/showtell-questions"))
	for _, want := range []string{"Why tabs?", "The Crab asks"} {
		if !strings.Contains(body, want) {
			t.Errorf("the host console should show %q, got:\n%s", want, body)
		}
	}
	if strings.Contains(body, "@b") {
		t.Errorf("the host console must not carry the asker's handle, got:\n%s", body)
	}

	// The host asks it: the big screen shows the current question.
	post(t, srv, "/admin/showtell/question", url.Values{"id": {idValue(questions[0].ID)}, "action": {"asked"}})
	var graph map[string]any
	if err := json.Unmarshal([]byte(readBody(t, get(t, srv, "/bigscreen/graph-data"))), &graph); err != nil {
		t.Fatal(err)
	}
	presenter, ok := graph["presenter"].(map[string]any)
	if !ok || presenter["question"] == nil {
		t.Fatalf("the big screen should show the current question, got %v", graph["presenter"])
	}

	// The host marks it answered: the screen takes it down.
	post(t, srv, "/admin/showtell/question", url.Values{"id": {idValue(questions[0].ID)}, "action": {"answered"}})
	if err := json.Unmarshal([]byte(readBody(t, get(t, srv, "/bigscreen/graph-data"))), &graph); err != nil {
		t.Fatal(err)
	}
	if presenter, ok := graph["presenter"].(map[string]any); !ok || presenter["question"] != nil {
		t.Errorf("the answered question should leave the screen, got %v", graph["presenter"])
	}
}

// TestRecapPage_ShowsTheShowTellItems covers the recap's show & tell items:
// the Participant's sign-up and their audience questions, with how each ended
// up — personas only.
func TestRecapPage_ShowsTheShowTellItems(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	seed(t, deps.db, "c", "The Snake", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	presenter := stagePresenter(t, deps, "a")
	askQuestion(t, deps, "b", "Why tabs?")
	if _, err := deps.showtell.TransitionQuestion(questionID(t, deps, "b", 0), QuestionAsked); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.showtell.TransitionQuestion(questionID(t, deps, "b", 0), QuestionAnswered); err != nil {
		t.Fatal(err)
	}
	askQuestion(t, deps, "b", "One more")
	if _, err := deps.showtell.TransitionQuestion(questionID(t, deps, "b", 1), QuestionDismissed); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.showtell.Done(presenter.ID); err != nil {
		t.Fatal(err)
	}
	if err := deps.events.Advance(PhaseClosed); err != nil {
		t.Fatal(err)
	}

	// The asker's recap: their questions with their final statuses.
	body := readBody(t, get(t, srv, "/user/recap/"+recapToken(t, srv, "b")))
	for _, want := range []string{"Your show &amp; tell", "“Why tabs?” — answered", "“One more” — dismissed"} {
		if !strings.Contains(body, want) {
			t.Errorf("the recap should show %q, got:\n%s", want, body)
		}
	}
	if strings.Contains(body, "@b") || strings.Contains(body, "@a") {
		t.Errorf("the recap's show & tell items are personas only, got:\n%s", body)
	}

	// The presenter's recap: their sign-up, presented.
	body = readBody(t, get(t, srv, "/user/recap/"+recapToken(t, srv, "a")))
	for _, want := range []string{"“Turing Tumble”", "✅ presented"} {
		if !strings.Contains(body, want) {
			t.Errorf("the recap should show %q, got:\n%s", want, body)
		}
	}

	// A Participant without show & tell items has no section.
	if body := readBody(t, get(t, srv, "/user/recap/"+recapToken(t, srv, "c"))); strings.Contains(body, "Your show &amp; tell") {
		t.Errorf("without show & tell items there is no section, got:\n%s", body)
	}
}

// TestRecapData_TheShowTellReadsFail covers the recap against show & tell
// reads that fail: the page renders without the items.
func TestRecapData_TheShowTellReadsFail(t *testing.T) {
	t.Run("the sign-up cannot be read", func(t *testing.T) {
		srv, deps := newTestServer(t, nil, nil)
		seed(t, deps.db, "a", "The Gopher", "ready")
		if err := deps.events.Advance(PhaseShowTell); err != nil {
			t.Fatal(err)
		}
		signUp(t, deps, "a", "Turing Tumble")
		token, err := NewRecaps(deps.db).TokenFor("a")
		if err != nil {
			t.Fatal(err)
		}
		breakOnFault(t, deps.db, "showtell_read")
		if body := readBody(t, get(t, srv, "/user/recap/"+token)); strings.Contains(body, "Your show &amp; tell") {
			t.Errorf("without the sign-up read there is no section, got:\n%s", body)
		}
	})
	t.Run("the questions cannot be read", func(t *testing.T) {
		srv, deps := newTestServer(t, nil, nil)
		seed(t, deps.db, "a", "The Gopher", "ready")
		seed(t, deps.db, "b", "The Crab", "ready")
		if err := deps.events.Advance(PhaseShowTell); err != nil {
			t.Fatal(err)
		}
		stagePresenter(t, deps, "a")
		askQuestion(t, deps, "b", "Why tabs?")
		token, err := NewRecaps(deps.db).TokenFor("b")
		if err != nil {
			t.Fatal(err)
		}
		breakOnFault(t, deps.db, "showtell_question_read")
		if body := readBody(t, get(t, srv, "/user/recap/"+token)); strings.Contains(body, "Why tabs?") {
			t.Errorf("without the questions read there are no items, got:\n%s", body)
		}
	})
}

// TestReset_ClearsTheShowTellQuestions covers the reset: the questions go
// with everything else, in the same transaction.
func TestReset_ClearsTheShowTellQuestions(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	stagePresenter(t, deps, "a")
	askQuestion(t, deps, "b", "Why tabs?")

	if resp := post(t, srv, "/admin/reset", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("reset: want 303, got %d", resp.StatusCode)
	}
	if questions, _ := deps.showtell.Questions(); len(questions) != 0 {
		t.Errorf("the questions should be cleared, got %v", questions)
	}
}

// TestReset_ReportsAShowTellQuestionsDeleteFailure covers the reset's question
// statement: one transaction, so a failure leaves everything as it was.
func TestReset_ReportsAShowTellQuestionsDeleteFailure(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	stagePresenter(t, deps, "a")
	askQuestion(t, deps, "b", "Why tabs?")
	breakOnFault(t, deps.db, "reset_showtell_questions")

	if err := deps.events.Reset(); err == nil {
		t.Fatal("want the failure reported")
	}
	if questions, _ := deps.showtell.Questions(); len(questions) != 1 {
		t.Errorf("the questions should be untouched, got %v", questions)
	}
	if deps.events.State() != PhaseShowTell {
		t.Errorf("the phase should be untouched, got %s", deps.events.State())
	}
}

// TestDeleteParticipant_RemovesTheirShowTellQuestions covers the cleanup
// through the debug delete: the Participant's questions go with them — both
// the ones they asked and the ones their talk drew.
func TestDeleteParticipant_RemovesTheirShowTellQuestions(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	seed(t, deps.db, "c", "The Snake", "ready")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	stagePresenter(t, deps, "a")
	askQuestion(t, deps, "b", "b's question")
	askQuestion(t, deps, "c", "c's question")

	// The asker leaves: their question goes.
	if resp := del(t, srv, "/data/participant/b"); resp.StatusCode != 200 {
		t.Fatalf("delete: want 200, got %d", resp.StatusCode)
	}
	questions, err := deps.showtell.Questions()
	if err != nil || len(questions) != 1 || questions[0].ParticipantID != "c" {
		t.Errorf("the asker's question should be gone, got %v, %v", questions, err)
	}

	// The presenter leaves: the questions their talk drew go too.
	if resp := del(t, srv, "/data/participant/a"); resp.StatusCode != 200 {
		t.Fatalf("delete: want 200, got %d", resp.StatusCode)
	}
	if questions, _ := deps.showtell.Questions(); len(questions) != 0 {
		t.Errorf("the talk's questions should be gone, got %v", questions)
	}
}
