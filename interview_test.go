package main

import (
	"errors"
	"strings"
	"testing"
)

var testQuestions = []Question{
	{ID: "q_tabs", Text: "Tabs or spaces?", Options: []string{"Tabs", "Spaces"}},
	{ID: "q_langs", Text: "Top languages?", Options: []string{"Go", "Rust", "Python", "Java"}, MaxSelections: 3},
	{ID: "q_bug", Text: "Weirdest bug?"},
}

func participantInInterview(t *testing.T, db *DB, questions []Question) *Participant {
	t.Helper()
	if err := db.CreateParticipant("p-1", "someone", "Someone"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetQuestions("p-1", questions); err != nil {
		t.Fatal(err)
	}
	db.UpdatePipelineStep("p-1", "interviewing")
	p, err := db.GetParticipant("p-1")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func reload(t *testing.T, db *DB, id string) *Participant {
	t.Helper()
	p, err := db.GetParticipant(id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestInterview_AnsweringEveryQuestionInOrderCompletesIt(t *testing.T) {
	db := newTestDB(t)
	iv := NewInterview(db, newFakeLLM())
	p := participantInInterview(t, db, testQuestions)

	answers := []string{"Tabs", `["Go","Rust"]`, "A race condition on Tuesdays"}
	for i, a := range answers {
		q := iv.Next(p)
		if q == nil {
			t.Fatalf("question %d: Next returned nil", i)
		}
		if q.Question.ID != testQuestions[i].ID || q.Index != i || q.Total != len(testQuestions) {
			t.Fatalf("question %d: got %s (%d/%d)", i, q.Question.ID, q.Index, q.Total)
		}
		done, err := iv.Submit(p, a)
		if err != nil {
			t.Fatalf("question %d: Submit: %v", i, err)
		}
		if want := i == len(answers)-1; done != want {
			t.Fatalf("question %d: done = %v, want %v", i, done, want)
		}
		p = reload(t, db, p.ID)
	}

	if q := iv.Next(p); q != nil {
		t.Errorf("Next after completion: want nil, got %s", q.Question.ID)
	}
	if got := p.Answers["q_bug"]; got != "A race condition on Tuesdays" {
		t.Errorf("recorded answer: got %q", got)
	}
	if _, err := iv.Submit(p, "extra"); !errors.Is(err, ErrInterviewOver) {
		t.Errorf("Submit after completion: want ErrInterviewOver, got %v", err)
	}
}

func TestInterview_RejectsMalformedMultiSelectAnswer(t *testing.T) {
	db := newTestDB(t)
	iv := NewInterview(db, newFakeLLM())
	p := participantInInterview(t, db, testQuestions)
	iv.Submit(p, "Tabs")
	p = reload(t, db, p.ID)

	_, err := iv.Submit(p, "Go, Rust")

	var invalid *InvalidAnswerError
	if !errors.As(err, &invalid) {
		t.Fatalf("want InvalidAnswerError, got %v", err)
	}
	if q := iv.Next(reload(t, db, p.ID)); q == nil || q.Question.ID != "q_langs" {
		t.Errorf("a rejected answer must not advance the Interview")
	}
}

func TestInterview_RejectsTooManySelections(t *testing.T) {
	db := newTestDB(t)
	iv := NewInterview(db, newFakeLLM())
	p := participantInInterview(t, db, testQuestions)
	iv.Submit(p, "Tabs")
	p = reload(t, db, p.ID)

	_, err := iv.Submit(p, `["Go","Rust","Python","Java"]`)

	var invalid *InvalidAnswerError
	if !errors.As(err, &invalid) || invalid.Error() != "Too many selections. Maximum 3 allowed." {
		t.Fatalf("want too-many-selections error, got %v", err)
	}
}

func startedQuestions(t *testing.T, llm *fakeLLM, githubUser bool) []Question {
	t.Helper()
	db := newTestDB(t)
	if err := db.CreateParticipant("p-1", "someone", "Someone"); err != nil {
		t.Fatal(err)
	}
	iv := NewInterview(db, llm)
	if err := iv.Start("p-1", &GitHubProfile{Login: "someone"}, githubUser); err != nil {
		t.Fatalf("Start: %v", err)
	}
	p := reload(t, db, "p-1")
	if p.PipelineStep != "interviewing" {
		t.Errorf("step after Start: want interviewing, got %s", p.PipelineStep)
	}
	return p.Questions
}

func questionIDs(qs []Question) []string {
	var ids []string
	for _, q := range qs {
		ids = append(ids, q.ID)
	}
	return ids
}

func TestInterviewStart_GitHubUserGetsFixedPlusCustomQuestions(t *testing.T) {
	llm := newFakeLLM().on("interviewer", `{"questions": ["Why Go?", "Favourite repo?", "Tabs?"]}`)

	qs := startedQuestions(t, llm, true)

	want := []string{"fixed_0", "fixed_1", "fixed_2", "fixed_3", "fixed_4", "fixed_5", "custom_0", "custom_1", "custom_2"}
	if got := questionIDs(qs); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("question IDs:\n got %v\nwant %v", got, want)
	}
	if qs[6].Text != "Why Go?" {
		t.Errorf("first Custom Question text: got %q", qs[6].Text)
	}
}

func TestInterviewStart_GitHubUserFallsBackToExtraQuestionsWhenTheLLMFails(t *testing.T) {
	llm := newFakeLLM().onErr("interviewer", fakeError("mistral HTTP 401"))

	qs := startedQuestions(t, llm, true)

	want := []string{"fixed_0", "fixed_1", "fixed_2", "fixed_3", "fixed_4", "fixed_5", "extra_0", "extra_1", "extra_2", "extra_3", "extra_4"}
	if got := questionIDs(qs); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("question IDs:\n got %v\nwant %v", got, want)
	}
	if qs[6].MaxSelections != ExtraQuestions[0].MaxSelections || len(qs[6].Options) == 0 {
		t.Errorf("fallback Extra Questions must keep their options and selection limits")
	}
}

func TestInterviewStart_NonGitHubUserGetsExtraQuestionsAndFixedWithoutGoToLanguage(t *testing.T) {
	llm := newFakeLLM()

	qs := startedQuestions(t, llm, false)

	want := []string{"extra_0", "extra_1", "extra_2", "extra_3", "extra_4", "fixed_0", "fixed_2", "fixed_3", "fixed_4", "fixed_5"}
	if got := questionIDs(qs); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("question IDs:\n got %v\nwant %v", got, want)
	}
	if n := llm.callsMatching("interviewer"); n != 0 {
		t.Errorf("Non-GitHub Users need no Custom Questions, but the LLM was asked %d times", n)
	}
}
