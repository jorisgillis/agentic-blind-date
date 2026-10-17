package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Interview owns a Participant's progress through their interview questions:
// which question comes next, validating and recording answers, and noticing
// when the Interview is complete. Handlers only route and render.
type Interview struct {
	store *ParticipantStore
	llm   LLM
}

// NewInterview creates the Interview module. The LLM generates Custom Questions.
func NewInterview(db *DB, llm LLM) *Interview {
	return &Interview{store: NewParticipantStore(db), llm: llm}
}

// QuestionData contains the data needed to render a single interview question.
type QuestionData struct {
	ParticipantID string
	Index         int
	Total         int
	Question      Question
}

// ErrInterviewOver is returned when an answer is submitted after the last question.
var ErrInterviewOver = errors.New("no more questions to answer")

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
func (iv *Interview) Start(p *Participant, profile *GitHubProfile) error {
	return iv.store.StartInterview(p.ID, profile, iv.questionSet(profile, p.HasGitHub))
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
		logger("interview").Warn("generating custom questions, falling back to the extra questions", "err", err)
		return append(qs, ExtraQuestions...)
	}
	return append(qs, custom...)
}

// customQuestionsReply is the LLM's raw reply shape for Custom Questions.
type customQuestionsReply struct {
	Questions []string `json:"questions"`
}

func (iv *Interview) customQuestions(profile *GitHubProfile) ([]Question, error) {
	system := `You are an interviewer at a tech meetup blind date event.
Generate 3 fun, opinionated questions tailored to this developer's GitHub profile.
Questions should be conversational and tech-related.
Respond with ONLY valid JSON — no markdown:
{"questions": ["...", "...", "..."]}`

	result, err := AskStructured(iv.llm, system, "Generate 3 personalized questions for:\n\n"+profile.Summary(),
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
	answers := withAnswer(p.Answers, next.Question.ID, answer)
	if err := iv.store.Change(ParticipantChange{ID: p.ID, Answers: answers}); err != nil {
		return false, err
	}
	p.Answers = answers
	return iv.completeIfDone(p)
}

// withAnswer returns a copy of answers plus the given answer.
func withAnswer(answers map[string]string, id, answer string) map[string]string {
	out := make(map[string]string, len(answers)+1)
	for k, v := range answers {
		out[k] = v
	}
	out[id] = answer
	return out
}

// completeIfDone completes the Interview when the recorded answer was the
// last one.
func (iv *Interview) completeIfDone(p *Participant) (bool, error) {
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
	if !askedExtraQuestions(answers) {
		return nil
	}
	return &ExtraAnswers{
		Languages:      extraLanguages(answers["extra_0"]),
		ProjectType:    answers["extra_1"],
		DevEnvironment: nonEmptyList(answers["extra_2"]),
		WeirdestBug:    answers["extra_3"],
		Keyboard:       answers["extra_4"],
	}
}

// askedExtraQuestions reports whether any Extra Question was answered.
func askedExtraQuestions(answers map[string]string) bool {
	for _, q := range ExtraQuestions {
		if _, ok := answers[q.ID]; ok {
			return true
		}
	}
	return false
}

// extraLanguages parses the languages answer: a JSON array, or a single
// language when it is not valid JSON (an empty answer stays empty).
func extraLanguages(answer string) []string {
	var languages []string
	if err := json.Unmarshal([]byte(answer), &languages); err != nil && answer != "" {
		languages = []string{answer}
	}
	return languages
}

// nonEmptyList wraps a single answer in a one-element list; an empty answer
// stays nil.
func nonEmptyList(answer string) []string {
	if answer != "" {
		return []string{answer}
	}
	return nil
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

// freeTextAnswerIDs are the built-in questions answered in free text.
var freeTextAnswerIDs = func() map[string]bool {
	ids := map[string]bool{}
	for _, q := range append(append([]Question{}, FixedQuestions...), ExtraQuestions...) {
		if q.Options == nil {
			ids[q.ID] = true
		}
	}
	return ids
}()

// comparableAnswer reports whether two Participants' answers to the question id
// can be compared for equality. Free-text answers rarely match exactly, and
// Custom Questions differ per Participant under the same id.
func comparableAnswer(id string) bool {
	return !freeTextAnswerIDs[id] && !strings.HasPrefix(id, "custom_")
}

// interviewText renders the Participant's answers for an LLM as "question →
// answer" lines in interview order, so the answers make sense without the
// question ids. Answers with no stored question fall back to their id.
func interviewText(p *Participant) string {
	lines := answeredLines(p)
	if len(lines) == 0 {
		return "(none)"
	}
	return strings.Join(lines, "\n")
}

// answeredLines renders the answers as "question → answer" lines: stored
// questions in interview order first, then any remaining answers by id.
func answeredLines(p *Participant) []string {
	lines, seen := questionLines(p)
	return append(lines, unaskedLines(p, seen)...)
}

// questionLines renders the answers to stored questions, in interview order.
func questionLines(p *Participant) ([]string, map[string]bool) {
	seen := map[string]bool{}
	var lines []string
	for _, q := range p.Questions {
		if a, ok := p.Answers[q.ID]; ok {
			lines = append(lines, fmt.Sprintf("- %s → %s", q.Text, a))
			seen[q.ID] = true
		}
	}
	return lines, seen
}

// unaskedLines renders the answers with no stored question, by id.
func unaskedLines(p *Participant, seen map[string]bool) []string {
	var rest []string
	for id := range p.Answers {
		if !seen[id] {
			rest = append(rest, id)
		}
	}
	sort.Strings(rest)
	var lines []string
	for _, id := range rest {
		lines = append(lines, fmt.Sprintf("- %s → %s", id, p.Answers[id]))
	}
	return lines
}

// indent prefixes every line of s.
func indent(s, prefix string) string {
	return prefix + strings.ReplaceAll(s, "\n", "\n"+prefix)
}
