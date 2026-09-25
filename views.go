package main

// Typed page views (#59): each page's handler builds one of these instead of
// an untyped map, so the domain decisions (which Q&A pairs, which other
// Participants to show) are computed once, in Go, not re-derived in the
// template from raw data the handler happened to pass through.

// WaitView is /user/wait/{id}: a Participant's own profile and answers while
// they wait for a partner or the Reveal.
type WaitView struct {
	Participant *Participant
	Profile     GitHubProfile
	QAPairs     []QAPair
	Count       int
}

// WaitStatusView is the HTMX-polled fragment that redirects the wait page
// once a Participant is matched or the Reveal happens.
type WaitStatusView struct {
	Participant *Participant
	Count       int
}

// MatchView is /user/match/{id}: a Participant's revealed Match, and everyone
// else at the event for broader networking.
type MatchView struct {
	Me          *Participant
	Match       *Participant
	RedFlags    []string
	GreenFlags  []string
	Icebreakers []string
	Others      []*Participant
}

// ExploreView is /user/explore/{myId}/{otherId}: an on-demand compatibility
// assessment between two ready Participants who are not each other's Match.
type ExploreView struct {
	Me          *Participant
	Other       *Participant
	Score       int
	Reason      string
	GreenFlags  []string
	RedFlags    []string
	Icebreakers []string
}

// ScreenView is /bigscreen: the shell that the graph and state view render into.
type ScreenView struct {
	Palette map[string]string
}

// DataView is /data: the debug overview of the whole event.
type DataView struct {
	Event        EventState
	Participants []*Participant
	Activity     []string
	Count        int
	ReadyCount   int
}

// AdminView is /admin: the host panel.
type AdminView struct {
	Event EventState
	Count int
	Ready int
}
