package main

// destination is the one navigation rule: the page a Participant belongs on,
// given their Pipeline Step, their Interview, their Relationship State and
// the event's Phase.
//
//   - onboarding page: while the GitHub profile and questions are prepared, and
//     while the Interview has open questions
//   - match page: once matched, from the Social phase on
//   - wait page: otherwise (persona being created, ready, or matched before the Social)
func destination(p *Participant, phase Phase) string {
	switch {
	case p.PipelineStep == StepFetchingGitHub:
		return "/user/onboard/" + p.ID
	case p.PipelineStep == StepInterviewing && hasOpenQuestions(p):
		return "/user/onboard/" + p.ID
	case p.PipelineStep == StepReady && p.IsMatched() && phase.AtLeast(PhaseSocial):
		return "/user/match/" + p.ID
	default:
		return "/user/wait/" + p.ID
	}
}

// destination applies the navigation rule with the current Phase.
func (h *Handler) destination(p *Participant) string {
	return destination(p, h.events.State())
}
