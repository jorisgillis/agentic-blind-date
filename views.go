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
	Palette  map[string]string
	ThemeKey string // the admin's Big Screen Theme choice (#42); never a device-stored one
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
	Event          EventState
	Count          int
	Ready          int
	BigScreenTheme string // the currently chosen Big Screen Theme's key (#42)
	Format         EventFormat
	FormatLocked   bool // Participants are present: the Event Format can't change (#77)
}

// HostConsoleView is /admin/host: the Host's phone-friendly console (#78).
type HostConsoleView struct {
	Active           bool
	Segment          ShowSegment
	Round            int // 1-based, for display
	TotalRounds      int
	NextAction       string // "" once there is nothing left to advance to (the Afterparty)
	RoundOpen        bool   // #79
	RoundAnswerCount int
	RoundSecondsLeft int
}

// ShowStatusView is /user/show/{id}: a Participant's phone between Rounds
// in a Game Show, before their Match is revealed (#78). Question and
// SecondsLeft are only meaningful while RoundOpen (#79).
type ShowStatusView struct {
	Participant *Participant
	Segment     ShowSegment
	Round       int // 1-based, for display
	TotalRounds int
	RoundOpen   bool
	Question    Question
	SecondsLeft int
}
