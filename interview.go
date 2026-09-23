package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Interview owns a Participant's progress through their interview questions:
// which question comes next, validating and recording answers, and noticing
// when the Interview is complete. Handlers only route and render.
type Interview struct {
	db *DB
}

// NewInterview creates the Interview module.
func NewInterview(db *DB) *Interview {
	return &Interview{db: db}
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
