package main

// destination is the one navigation rule: the page a Participant belongs on,
// given their Pipeline Step, their Interview, their Relationship State,
// whether the admin has revealed the Matches, and the Event Format.
//
//   - onboarding page: while the GitHub profile and questions are prepared, and
//     while the Interview has open questions (the Lobby's, in a Game Show)
//   - match page: once matched, at or after their own pair's turn (#83)
//   - show status page: ready, in a Game Show, until then — including the
//     wildcard, who is never matched, so they'd otherwise never leave it (#83)
//   - wait page: otherwise (persona being created, ready, or matched before the Reveal)
func destination(p *Participant, revealed bool, format EventFormat) string {
	switch {
	case p.PipelineStep == StepFetchingGitHub:
		return "/user/onboard/" + p.ID
	case p.PipelineStep == StepInterviewing && hasOpenQuestions(p):
		return "/user/onboard/" + p.ID
	case p.PipelineStep == StepReady && p.IsMatched() && revealed:
		return "/user/match/" + p.ID
	case format == GameShow && p.PipelineStep == StepReady:
		return "/user/show/" + p.ID
	default:
		return "/user/wait/" + p.ID
	}
}

// destination applies the navigation rule with the current Event State and
// Format. In a Game Show, "revealed" narrows from the Event State's global
// flag to whether this Participant's own pair has had its turn in the
// pair-by-pair Reveal (#83): before that, they stay off the match page,
// however the room got to Revealed.
func (h *Handler) destination(p *Participant) string {
	revealed := h.db.EventState().IsRevealed()
	if revealed && h.db.EventFormat() == GameShow {
		revealed = h.show.PairRevealed(p.ID)
	}
	return destination(p, revealed, h.db.EventFormat())
}
