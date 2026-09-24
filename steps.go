package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Step is a Participant's Pipeline Step.
type Step string

// The Pipeline Steps, in order. Relationship State is not a step.
const (
	StepFetchingGitHub  Step = "fetching_github"
	StepInterviewing    Step = "interviewing"
	StepCreatingPersona Step = "creating_persona"
	StepReady           Step = "ready"
)

// Step predicates, for templates (which cannot use the constants).
func (s Step) IsFetchingGitHub() bool  { return s == StepFetchingGitHub }
func (s Step) IsInterviewing() bool    { return s == StepInterviewing }
func (s Step) IsCreatingPersona() bool { return s == StepCreatingPersona }
func (s Step) IsReady() bool           { return s == StepReady }

// previousStep holds the only valid transitions: each step is reached from exactly one step.
var previousStep = map[Step]Step{
	StepInterviewing:    StepFetchingGitHub,
	StepCreatingPersona: StepInterviewing,
	StepReady:           StepCreatingPersona,
}

// StartInterview stores the profile and question set and opens the Interview,
// all in one transaction. It fails with ErrIllegalTransition, writing nothing,
// unless the Participant is still being prepared (so a second, concurrent
// preparation cannot swap the questions of a running Interview).
func (db *DB) StartInterview(id string, profile *GitHubProfile, questions []Question) error {
	profileJSON, _ := json.Marshal(profile)     // plain data: cannot fail
	questionsJSON, _ := json.Marshal(questions) // plain data: cannot fail
	return db.inTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`UPDATE participants SET pipeline_step = ?, profile_json = ?, questions = ?
			WHERE id = ? AND pipeline_step = ?`, StepInterviewing, string(profileJSON), string(questionsJSON), id, StepFetchingGitHub)
		if err != nil {
			return err
		}
		if rowsAffected(res) != 1 {
			return fmt.Errorf("%w: %s is no longer being prepared", ErrIllegalTransition, id)
		}
		return nil
	})
}

// ErrIllegalTransition is returned when a Participant is not at the step that precedes the target.
var ErrIllegalTransition = errors.New("illegal pipeline step transition")

// AdvanceStep moves a Participant to the next Pipeline Step. It fails with
// ErrIllegalTransition unless they are at the step directly before it, so a
// step is entered at most once (only Reset starts over).
func (db *DB) AdvanceStep(id string, to Step) error {
	from, ok := previousStep[to]
	if !ok {
		return fmt.Errorf("%w: nothing leads to %s", ErrIllegalTransition, to)
	}
	res, err := db.db.Exec(`UPDATE participants SET pipeline_step = ? WHERE id = ? AND pipeline_step = ?`, to, id, from)
	if err != nil {
		return err
	}
	if rowsAffected(res) != 1 {
		return fmt.Errorf("%w: %s is not at %s, cannot enter %s", ErrIllegalTransition, id, from, to)
	}
	db.changed()
	return nil
}
