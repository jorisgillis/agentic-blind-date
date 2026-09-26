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
	store *ParticipantStore
	llm   LLM
	db    *DB // EventFormat only (#77): in a Game Show, the Lobby asks personalised questions only
}

// NewInterview creates the Interview module. The LLM generates Custom Questions.
func NewInterview(db *DB, llm LLM) *Interview {
	return &Interview{store: NewParticipantStore(db), llm: llm, db: db}
}

// QuestionData contains the data needed to render a single interview question.
type QuestionData struct {
	ParticipantID string
	Index         int
	Total         int
	Question      Question
}

// QAPair is one answered Question, for rendering (#59).
type QAPair struct {
	Question string
	Answer   string
}

// PairAnswers matches every question to its answer, in question order,
// skipping any question not yet answered.
func PairAnswers(questions []Question, answers map[string]string) []QAPair {
	var pairs []QAPair
	for _, q := range questions {
		if ans, ok := answers[q.ID]; ok {
			pairs = append(pairs, QAPair{Question: q.Text, Answer: ans})
		}
	}
	return pairs
}

// ErrInterviewOver is returned when an answer is submitted after the last question.
var ErrInterviewOver = errors.New("No more questions to answer")

// InvalidAnswerError reports an answer that does not fit the current question.
// Its message is safe to show to the Participant.
type InvalidAnswerError struct{ msg string }

func (e *InvalidAnswerError) Error() string { return e.msg }

// Start picks the Participant's question set, stores it with their profile, and
// only then opens the Interview. Until Start returns, the Participant is still
// being prepared. In the Classic flow every Participant answers 11 questions,
// the 6 Fixed Questions plus 5 personalised ones (#76); in a Game Show, the
// Lobby asks only the 5 personalised ones, leaving the Fixed Questions for
// the Rounds (#77):
//   - GitHub user: Custom Questions generated from their profile, asked for
//     5 and topped up with Extra Questions when the LLM returns fewer or
//     fails (ADR-0003, ADR-0006)
//   - Non-GitHub User: the 5 Extra Questions
func (iv *Interview) Start(p *Participant, profile *GitHubProfile) error {
	return iv.store.StartInterview(p.ID, profile, iv.questionSet(profile, p.HasGitHub))
}

func (iv *Interview) questionSet(profile *GitHubProfile, githubUser bool) []Question {
	personalized := iv.personalizedQuestions(profile, githubUser)
	if iv.db.EventFormat() == GameShow {
		return personalized
	}
	if !githubUser {
		return append(personalized, FixedQuestions...)
	}
	return append(append([]Question{}, FixedQuestions...), personalized...)
}

func (iv *Interview) personalizedQuestions(profile *GitHubProfile, githubUser bool) []Question {
	if !githubUser {
		return append([]Question{}, ExtraQuestions...)
	}
	custom, err := iv.customQuestions(profile)
	if err != nil {
		log.Printf("Custom questions error: %v", err)
	}
	return topUp(custom, PersonalizedQuestions)
}

// topUp fills questions with ExtraQuestions until it has n, for when the LLM
// returns fewer Custom Questions than asked, or fails outright (#76).
func topUp(questions []Question, n int) []Question {
	qs := append([]Question{}, questions...)
	for _, extra := range ExtraQuestions {
		if len(qs) >= n {
			break
		}
		qs = append(qs, extra)
	}
	return qs
}

// customQuestionsReply is the LLM's raw reply shape for Custom Questions.
type customQuestionsReply struct {
	Questions []string `json:"questions"`
}

func (iv *Interview) customQuestions(profile *GitHubProfile) ([]Question, error) {
	system := `You are an interviewer at a tech meetup blind date event.
Generate 5 fun, opinionated questions tailored to this developer's GitHub profile.
Questions should be conversational and tech-related.
Respond with ONLY valid JSON — no markdown:
{"questions": ["...", "...", "...", "...", "..."]}`

	result, err := AskStructured(iv.llm, system, "Generate 5 personalized questions for:\n\n"+profile.Summary(),
		func(r customQuestionsReply) error {
			if len(r.Questions) == 0 {
				return errors.New("empty questions")
			}
			return nil
		})
	if err != nil {
		return nil, err
	}
	var qs []Question
	for i, text := range result.Questions {
		qs = append(qs, Question{ID: "custom_" + strconv.Itoa(i), Text: text})
	}
	return qs, nil
}

// hasOpenQuestions reports whether the Participant's Interview has unanswered questions.
func hasOpenQuestions(p *Participant) bool {
	for _, q := range p.Questions {
		if _, answered := p.Answers[q.ID]; !answered {
			return true
		}
	}
	return false
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
	if err := iv.store.Change(ParticipantChange{ID: p.ID, Answers: answers}); err != nil {
		return false, err
	}
	p.Answers = answers
	if iv.Next(p) != nil {
		return false, nil
	}
	if err := iv.complete(p); err != nil {
		return false, err
	}
	return true, nil
}

// complete derives ExtraAnswers from the Extra Questions, when they were asked,
// so profile readers (Summary, Interests, persona, Pair score) can use them.
func (iv *Interview) complete(p *Participant) error {
	extra := extraAnswersFrom(p.Answers)
	if extra == nil {
		return nil
	}
	profile := p.Profile
	if profile == nil {
		profile = &GitHubProfile{}
	}
	profile.ExtraAnswers = extra
	p.Profile = profile
	return iv.store.Change(ParticipantChange{ID: p.ID, Profile: profile})
}

// extraAnswersFrom maps answers to the Extra Questions onto ExtraAnswers.
// It returns nil when none of the Extra Questions were answered.
func extraAnswersFrom(answers map[string]string) *ExtraAnswers {
	asked := false
	for _, q := range ExtraQuestions {
		if _, ok := answers[q.ID]; ok {
			asked = true
		}
	}
	if !asked {
		return nil
	}
	var languages []string
	if err := json.Unmarshal([]byte(answers["extra_0"]), &languages); err != nil && answers["extra_0"] != "" {
		languages = []string{answers["extra_0"]}
	}
	var devEnv []string
	if env := answers["extra_2"]; env != "" {
		devEnv = []string{env}
	}
	return &ExtraAnswers{
		Languages:      languages,
		ProjectType:    answers["extra_1"],
		DevEnvironment: devEnv,
		WeirdestBug:    answers["extra_3"],
		Keyboard:       answers["extra_4"],
	}
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
