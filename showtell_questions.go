package main

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// QuestionStatus is where an audience question stands: queued for the host,
// asked (the current question on the Big Screen), answered, or dismissed.
type QuestionStatus string

// The question statuses.
const (
	// QuestionQueued waits in the host's question queue.
	QuestionQueued QuestionStatus = "queued"
	// QuestionAsked is the current question on the Big Screen.
	QuestionAsked QuestionStatus = "asked"
	// QuestionAnswered is a question the presenter answered.
	QuestionAnswered QuestionStatus = "answered"
	// QuestionDismissed is a question the host dropped.
	QuestionDismissed QuestionStatus = "dismissed"
)

// ShowTellQuestion is one audience question: the text, the Sign-up it is bound
// to (the presenter on stage when it was asked), its status and the asker's
// Persona, joined live — questions render through the asker's persona only.
type ShowTellQuestion struct {
	ID            int64          `json:"id"`
	ParticipantID string         `json:"participant_id"`
	SignupID      int64          `json:"signup_id"`
	Text          string         `json:"text"`
	Status        QuestionStatus `json:"status"`
	CreatedAt     time.Time      `json:"created_at"`
	// The asker's Persona, joined live — personas are safe to show anywhere.
	PersonaName   string `json:"persona_name"`
	PersonaSymbol string `json:"persona_symbol"`
	PersonaColor  string `json:"persona_color"`
}

// The question guards.
var (
	// ErrInvalidQuestion is returned for a question that fails validation: no
	// text, or more than maxQuestionLength characters.
	ErrInvalidQuestion = errors.New("invalid show & tell question")
	// ErrNoPresenter is returned when a question is submitted while nobody is
	// on stage: questions bind to the on-stage sign-up.
	ErrNoPresenter = errors.New("nobody is on stage")
	// ErrQuestionNotFound is returned for an unknown question id.
	ErrQuestionNotFound = errors.New("show & tell question not found")
	// ErrAlreadyAsked is returned when marking a question asked while another
	// question is on the Big Screen: one current question at a time.
	ErrAlreadyAsked = errors.New("a question is already being asked")
	// ErrInvalidQuestionTransition is returned when moving a question to a
	// status it cannot reach: asked → answered, or dismissed from queued or
	// asked; answered and dismissed are final.
	ErrInvalidQuestionTransition = errors.New("the question cannot move to that status")
)

// maxQuestionLength caps a question's text, so one question cannot flood the
// Big Screen.
const maxQuestionLength = 300

// questionTransitions lists the statuses a question may move to from each
// status: queued → asked (on the Big Screen) or dismissed, asked → answered or
// dismissed, and answered and dismissed are final.
var questionTransitions = map[QuestionStatus][]QuestionStatus{
	QuestionQueued:    {QuestionAsked, QuestionDismissed},
	QuestionAsked:     {QuestionAnswered, QuestionDismissed},
	QuestionAnswered:  nil,
	QuestionDismissed: nil,
}

// canTransition reports whether a question may move from one status to the
// other.
func canTransition(from, to QuestionStatus) bool {
	for _, allowed := range questionTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// selectQuestion reads one question joined with its asker's Persona. The
// persona columns are COALESCEd: a question whose asker left the event still
// lists (debug-only), with an empty persona.
const selectQuestion = `
	SELECT q.id, q.participant_id, q.signup_id, q.text, q.status, q.created_at,
	       COALESCE(p.persona_name, ''), COALESCE(p.persona_symbol, ''), COALESCE(p.persona_color, '')
	FROM showtell_questions q
	LEFT JOIN participants p ON p.id = q.participant_id`

// scanQuestion scans one question row.
func scanQuestion(row interface{ Scan(...any) error }) (*ShowTellQuestion, error) {
	q := &ShowTellQuestion{}
	err := row.Scan(
		&q.ID, &q.ParticipantID, &q.SignupID, &q.Text, &q.Status, &q.CreatedAt,
		&q.PersonaName, &q.PersonaSymbol, &q.PersonaColor,
	)
	if err != nil {
		return nil, err
	}
	return q, nil
}

// questionRow reads one question from row, mapping "no row" to nil.
func questionRow(row *sql.Row) (*ShowTellQuestion, error) {
	question, err := scanQuestion(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return question, nil
}

// Ask records the Participant's question for the presenter currently on
// stage — rendered through the asker's persona, "{symbol} asks: …". The
// question binds to the on-stage sign-up. Questioning needs no meeting and
// earns nothing — no connections, no raffle; answered questions only count
// toward the audience-participation metric. Live during show & tell only, and
// only while someone is on stage.
func (s *ShowTell) Ask(p *Participant, text string) error {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > maxQuestionLength {
		return fmt.Errorf("%w: the question needs 1–%d characters", ErrInvalidQuestion, maxQuestionLength)
	}
	if phase := s.events.State(); !phase.IsShowTell() {
		return fmt.Errorf("%w: the event is in the %s phase", ErrShowTellClosed, phase)
	}
	presenter, err := s.OnStage()
	if err != nil {
		return err
	}
	if presenter == nil {
		return ErrNoPresenter
	}
	err = s.db.inTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO showtell_questions (participant_id, signup_id, text) VALUES (?, ?, ?)`,
			p.ID, presenter.ID, text)
		return err
	})
	if err != nil {
		return err
	}
	s.db.LogActivity(fmt.Sprintf("❓ %s %s asks: %q", p.PersonaSymbol, p.PersonaName, text))
	return nil
}

// Questions returns every question, in submission order — the host console's
// question queue.
func (s *ShowTell) Questions() ([]ShowTellQuestion, error) {
	return s.queryQuestions(`ORDER BY q.id`)
}

// queryQuestions selects the questions matching the given clause.
func (s *ShowTell) queryQuestions(clause string, args ...any) ([]ShowTellQuestion, error) {
	rows, err := s.db.db.Query(selectQuestion+" "+clause, args...)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)

	var out []ShowTellQuestion
	for rows.Next() {
		question, err := scanQuestion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *question)
	}
	return out, rows.Err()
}

// QuestionByID returns the question, or nil when the id is not in the queue.
func (s *ShowTell) QuestionByID(id int64) (*ShowTellQuestion, error) {
	return questionRow(s.db.db.QueryRow(selectQuestion+` WHERE q.id = ?`, id))
}

// CurrentQuestion returns the question being asked — the Big Screen's current
// question — or nil when none is.
func (s *ShowTell) CurrentQuestion() (*ShowTellQuestion, error) {
	return questionRow(s.db.db.QueryRow(selectQuestion+` WHERE q.status = ? LIMIT 1`, QuestionAsked))
}

// QuestionsForParticipant returns the Participant's questions, in submission
// order — the personal page's section and the recap's show & tell items.
func (s *ShowTell) QuestionsForParticipant(participantID string) ([]ShowTellQuestion, error) {
	return s.queryQuestions(`WHERE q.participant_id = ? ORDER BY q.id`, participantID)
}

// TransitionQuestion moves a question through the host's actions: asked puts
// it on the Big Screen (one current question at a time), answered marks it
// answered, dismissed drops it. Live during show & tell only.
func (s *ShowTell) TransitionQuestion(id int64, to QuestionStatus) (*ShowTellQuestion, error) {
	if phase := s.events.State(); !phase.IsShowTell() {
		return nil, fmt.Errorf("%w: the event is in the %s phase", ErrShowTellClosed, phase)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if to == QuestionAsked {
		current, err := s.CurrentQuestion()
		if err != nil {
			return nil, err
		}
		if current != nil {
			return nil, fmt.Errorf("%w: %s %s is being asked", ErrAlreadyAsked, current.PersonaSymbol, current.PersonaName)
		}
	}
	question, err := s.QuestionByID(id)
	if err != nil {
		return nil, err
	}
	if question == nil {
		return nil, ErrQuestionNotFound
	}
	if !canTransition(question.Status, to) {
		return nil, fmt.Errorf("%w: %s → %s", ErrInvalidQuestionTransition, question.Status, to)
	}
	err = s.db.inTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE showtell_questions SET status = ? WHERE id = ?`, to, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	question.Status = to
	switch to {
	case QuestionAsked:
		s.db.LogActivity(fmt.Sprintf("❓ On the big screen: %s %s's question", question.PersonaSymbol, question.PersonaName))
	case QuestionAnswered:
		s.db.LogActivity(fmt.Sprintf("✅ Answered: %s %s's question", question.PersonaSymbol, question.PersonaName))
	case QuestionDismissed:
		s.db.LogActivity(fmt.Sprintf("🗑 Dismissed: %s %s's question", question.PersonaSymbol, question.PersonaName))
	}
	return question, nil
}
