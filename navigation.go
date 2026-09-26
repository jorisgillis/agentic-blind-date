package main

// destination is the one navigation rule: the page a Participant belongs on,
// given their Pipeline Step, their Interview, their Relationship State,
// whether the admin has revealed the Matches, and the Event Format.
//
//   - onboarding page: while the GitHub profile and questions are prepared, and
//     while the Interview has open questions (the Lobby's, in a Game Show)
//   - match page: once matched, after the Reveal
//   - show status page: ready, in a Game Show, before the Reveal (#78)
//   - wait page: otherwise (persona being created, ready, or matched before the Reveal)
func destination(p *Participant, revealed bool, format EventFormat) string {
	switch {
	case p.PipelineStep == StepFetchingGitHub:
		return "/user/onboard/" + p.ID
	case p.PipelineStep == StepInterviewing && hasOpenQuestions(p):
		return "/user/onboard/" + p.ID
	case p.PipelineStep == StepReady && p.IsMatched() && revealed:
		return "/user/match/" + p.ID
	case format == GameShow && p.PipelineStep == StepReady && !revealed:
		return "/user/show/" + p.ID
	default:
		return "/user/wait/" + p.ID
	}
}

// destination applies the navigation rule with the current Event State and Format.
func (h *Handler) destination(p *Participant) string {
	return destination(p, h.db.EventState().IsRevealed(), h.db.EventFormat())
}
