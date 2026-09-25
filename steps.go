package main

import "errors"

// Step is a Participant's Pipeline Step.
type Step string

// The Pipeline Steps, in order. Relationship State is not a step.
const (
	StepFetchingGitHub  Step = "fetching_github"
	StepInterviewing    Step = "interviewing"
	StepCreatingPersona Step = "creating_persona"
	StepReady           Step = "ready"
)

// stepPtr is a convenience for ParticipantChange.PipelineStep, which needs a
// pointer to distinguish "advance to this step" from "leave it alone".
func stepPtr(s Step) *Step { return &s }

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

// ErrIllegalTransition is returned when a Participant is not at the step that precedes the target.
var ErrIllegalTransition = errors.New("illegal pipeline step transition")
