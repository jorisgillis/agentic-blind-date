package main

// destination is the one navigation rule: the page a Participant belongs on,
// given their Pipeline Step, their Interview, their Relationship State and
// whether the admin has revealed the Matches.
//
//   - onboarding page: while the GitHub profile and questions are prepared, and
//     while the Interview has open questions
//   - match page: once matched, after the Reveal
//   - wait page: otherwise (persona being created, ready, or matched before the Reveal)
func destination(p *Participant, revealed bool) string {
	switch {
	case p.PipelineStep == StepFetchingGitHub:
		return "/user/onboard/" + p.ID
	case p.PipelineStep == StepInterviewing && hasOpenQuestions(p):
		return "/user/onboard/" + p.ID
	case p.PipelineStep == StepReady && p.IsMatched() && revealed:
		return "/user/match/" + p.ID
	default:
		return "/user/wait/" + p.ID
	}
}

// destination applies the navigation rule with the current Event State.
func (h *Handler) destination(p *Participant) string {
	return destination(p, h.phase() == "revealed")
}
