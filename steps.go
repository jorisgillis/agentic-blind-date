package main

import (
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

// previousStep holds the only valid transitions: each step is reached from exactly one step.
var previousStep = map[Step]Step{
	StepInterviewing:    StepFetchingGitHub,
	StepCreatingPersona: StepInterviewing,
	StepReady:           StepCreatingPersona,
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
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("%w: %s is not at %s, cannot enter %s", ErrIllegalTransition, id, from, to)
	}
	return nil
}
