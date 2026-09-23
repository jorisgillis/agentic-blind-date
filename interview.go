package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
)

// Interview owns a Participant's progress through their interview questions:
// which question comes next, validating and recording answers, and noticing
// when the Interview is complete. Handlers only route and render.
type Interview struct {
	db  *DB
	llm LLM
}

// NewInterview creates the Interview module. The LLM generates Custom Questions.
func NewInterview(db *DB, llm LLM) *Interview {
	return &Interview{db: db, llm: llm}
}

// QuestionData contains the data needed to render a single interview question.
type QuestionData struct {
	ParticipantID string
	Index         int
	Total         int
	Question      Question
}

// ErrInterviewOver is returned when an answer is submitted after the last question.
var ErrInterviewOver = errors.New("No more questions to answer")

// InvalidAnswerError reports an answer that does not fit the current question.
// Its message is safe to show to the Participant.
type InvalidAnswerError struct{ msg string }

func (e *InvalidAnswerError) Error() string { return e.msg }

// Start picks the Participant's question set, stores it with their profile, and
// only then opens the Interview. Until Start returns, the Participant is still
// being prepared. The question sets are:
//   - GitHub user: Fixed Questions + Custom Questions generated from their profile,
//     or + Extra Questions when generation fails (ADR-0003, ADR-0006)
//   - Non-GitHub User: Extra Questions + Fixed Questions without "go-to language",
//     which the Extra Questions already cover
func (iv *Interview) Start(participantID string, profile *GitHubProfile, githubUser bool) error {
	questions := iv.questionSet(profile, githubUser)
	if err := iv.db.UpdateProfile(participantID, profile, "", "", questions); err != nil {
		return err
	}
	return iv.db.UpdatePipelineStep(participantID, "interviewing")
}

func (iv *Interview) questionSet(profile *GitHubProfile, githubUser bool) []Question {
	var qs []Question
	if !githubUser {
		qs = append(qs, ExtraQuestions...)
		for _, q := range FixedQuestions {
			if q.ID != "fixed_1" {
				qs = append(qs, q)
			}
		}
		return qs
	}

	qs = append(qs, FixedQuestions...)
	custom, err := iv.customQuestions(profile)
	if err != nil {
		log.Printf("Custom questions error: %v", err)
		return append(qs, ExtraQuestions...)
	}
	return append(qs, custom...)
}

func (iv *Interview) customQuestions(profile *GitHubProfile) ([]Question, error) {
	system := `You are an interviewer at a tech meetup blind date event.
Generate 3 fun, opinionated questions tailored to this developer's GitHub profile.
Questions should be conversational and tech-related.
Respond with ONLY valid JSON — no markdown:
{"questions": ["...", "...", "..."]}`

	response, err := iv.llm.Chat(system, "Generate 3 personalized questions for:\n\n"+profile.Summary())
	if err != nil {
		return nil, err
	}

	var result struct {
		Questions []string `json:"questions"`
	}
	if err := json.Unmarshal([]byte(extractJSON(response)), &result); err != nil {
		return nil, fmt.Errorf("questions parse error: %v", err)
	}
	if len(result.Questions) == 0 {
		return nil, errors.New("empty questions")
	}
	var qs []Question
	for i, text := range result.Questions {
		qs = append(qs, Question{ID: "custom_" + strconv.Itoa(i), Text: text})
	}
	return qs, nil
}

// Next returns the first unanswered question, or nil when none remain.
func (iv *Interview) Next(p *Participant) *QuestionData {
	for i, q := range p.Questions {
		if _, answered := p.Answers[q.ID]; !answered {
			return &QuestionData{ParticipantID: p.ID, Index: i, Total: len(p.Questions), Question: q}
		}
	}
	return nil
}

// Submit validates raw as the answer to the next question and records it.
// It reports whether that answer completed the Interview.
func (iv *Interview) Submit(p *Participant, raw string) (done bool, err error) {
	next := iv.Next(p)
	if next == nil {
		return false, ErrInterviewOver
	}
	answer := strings.TrimSpace(raw)
	if err := validateAnswer(next.Question, answer); err != nil {
		return false, err
	}

	answers := make(map[string]string, len(p.Answers)+1)
	for k, v := range p.Answers {
		answers[k] = v
	}
	answers[next.Question.ID] = answer
	if err := iv.db.UpdateAnswers(p.ID, answers); err != nil {
		return false, err
	}
	p.Answers = answers
	return iv.Next(p) == nil, nil
}

func validateAnswer(q Question, answer string) error {
	if q.MaxSelections <= 1 {
		return nil
	}
	var selected []string
	if err := json.Unmarshal([]byte(answer), &selected); err != nil {
		return &InvalidAnswerError{"Invalid answer format for multi-select question"}
	}
	if len(selected) > q.MaxSelections {
		return &InvalidAnswerError{fmt.Sprintf("Too many selections. Maximum %d allowed.", q.MaxSelections)}
	}
	return nil
}
