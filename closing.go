package main

import (
	"fmt"
	"sort"
	"strings"
)

// Closing owns the closing ceremony's derived data: the evening's headline
// stats, the public event recap and the host's export pack. Everything it
// produces is personas and counts — never contact-channel values, never
// GitHub identity mappings.
type Closing struct {
	db       *DB
	store    *ParticipantStore
	meetings *Meetings
	channels *ContactChannels
	showtell *ShowTell
	board    *Board
}

// NewClosing creates the Closing module.
func NewClosing(db *DB, meetings *Meetings, channels *ContactChannels, showtell *ShowTell) *Closing {
	return &Closing{db: db, store: NewParticipantStore(db), meetings: meetings, channels: channels, showtell: showtell, board: NewBoard(db, NewEventStore(db))}
}

// EventStats is the evening's headline numbers — counts only, derived from
// the tables at read time.
type EventStats struct {
	Participants      int `json:"participants"`
	Ready             int `json:"ready"`
	Matched           int `json:"matched"`
	Meetings          int `json:"meetings"`          // completed meetings
	ContactExchanges  int `json:"contact_exchanges"` // contact details exchanged across completed meetings
	Questions         int `json:"questions"`         // audience questions asked
	QuestionsAnswered int `json:"questions_answered"`
}

// Stats derives the evening's headline numbers. A number that cannot be read
// comes back as zero — the closing view is a dashboard, not a ledger.
func (c *Closing) Stats() EventStats {
	var s EventStats
	s.Participants = c.db.ParticipantCount()
	s.Ready = c.db.ReadyCount()
	s.Matched = c.db.MatchedCount()
	c.meetingStats(&s)
	c.questionStats(&s)
	return s
}

// meetingStats counts the completed meetings and the contact details that
// changed hands on them. A number that cannot be read comes back as zero.
func (c *Closing) meetingStats(s *EventStats) {
	meetings, err := c.meetings.All()
	if err != nil {
		logger("closing").Warn("closing stats, meetings", "err", err)
		return
	}
	for _, m := range meetings {
		c.countMeeting(s, m)
	}
}

// countMeeting counts one meeting: a completed meeting is a connection, and
// a contact exchange is a contact detail that changed hands — an opted-in
// channel with a value, on a completed meeting. The count only — the values
// never leave their owners' recap pages.
func (c *Closing) countMeeting(s *EventStats, m Meeting) {
	if !m.Completed() {
		return // a connection is a completed meeting
	}
	s.Meetings++
	for _, side := range []string{m.A, m.B} {
		channels, err := c.channels.Channels(side)
		if err != nil {
			logger("closing").Warn("closing stats, channels", "participant", side, "err", err)
			continue
		}
		s.ContactExchanges += len(shared(channels))
	}
}

// questionStats counts the audience questions asked and answered. A number
// that cannot be read comes back as zero.
func (c *Closing) questionStats(s *EventStats) {
	questions, err := c.showtell.Questions()
	if err != nil {
		logger("closing").Warn("closing stats, questions", "err", err)
		return
	}
	s.Questions = len(questions)
	for _, q := range questions {
		if q.Status == QuestionAnswered {
			s.QuestionsAnswered++
		}
	}
}

// audienceParticipationTarget is the audience-participation success metric:
// the audience participated when at least ten questions were answered
// (ADR-0015).
const audienceParticipationTarget = 10

// SuccessMetrics is the evening's scorecard against the v2 goals — derived,
// never stored.
type SuccessMetrics struct {
	Meetings              int  `json:"meetings"`
	Connections           int  `json:"connections"` // one per side per completed meeting
	QuestionsAnswered     int  `json:"questions_answered"`
	AudienceParticipation bool `json:"audience_participation"`
}

// successMetrics derives the evening's success metrics from the headline
// stats and the connection leaderboard: how much interaction the evening
// forced, and whether the audience participated.
func successMetrics(stats EventStats, board []ExportStanding) SuccessMetrics {
	m := SuccessMetrics{
		Meetings:          stats.Meetings,
		QuestionsAnswered: stats.QuestionsAnswered,
	}
	for _, standing := range board {
		m.Connections += standing.Connections
	}
	m.AudienceParticipation = m.QuestionsAnswered >= audienceParticipationTarget
	return m
}

// ExportPersona is one Persona as the closing surfaces carry them — the
// anonymous identity only: name, symbol, colour, tagline. Never a handle,
// never a Participant id.
type ExportPersona struct {
	Name    string `json:"name"`
	Symbol  string `json:"symbol"`
	Color   string `json:"color"`
	Tagline string `json:"tagline"`
}

// personaOf reduces a Participant to their Persona — the only identity the
// closing surfaces carry.
func personaOf(p *Participant) ExportPersona {
	return ExportPersona{Name: p.PersonaName, Symbol: p.PersonaSymbol, Color: p.PersonaColor, Tagline: p.PersonaTagline}
}

// ExportStanding is one row of the closing surfaces' connection ranking:
// rank, Persona and connection count.
type ExportStanding struct {
	Rank        int    `json:"rank"`
	Name        string `json:"name"`
	Symbol      string `json:"symbol"`
	Color       string `json:"color"`
	Connections int    `json:"connections"`
}

// standings renders the connection leaderboard persona-only, ranked from 1.
func standings(board []ConnectionStanding) []ExportStanding {
	out := make([]ExportStanding, 0, len(board))
	for i, s := range board {
		out = append(out, ExportStanding{
			Rank:        i + 1,
			Name:        s.Name,
			Symbol:      s.Symbol,
			Color:       s.Color,
			Connections: s.Connections,
		})
	}
	return out
}

// Leaderboard is the evening's full connection ranking, persona-only — the
// export pack's leaderboard. An unreadable ranking comes back empty.
func (c *Closing) Leaderboard() []ExportStanding {
	board, err := c.meetings.Leaderboard()
	if err != nil {
		logger("closing").Warn("closing leaderboard", "err", err)
		return nil
	}
	return standings(board)
}

// Gallery is the persona gallery: every Participant as their Persona, in
// registration order — the public recap's gallery. Personas only.
func (c *Closing) Gallery() []ExportPersona {
	participants, err := c.store.All()
	if err != nil {
		logger("closing").Warn("closing gallery", "err", err)
		return nil
	}
	out := make([]ExportPersona, 0, len(participants))
	for _, p := range participants {
		out = append(out, personaOf(p))
	}
	return out
}

// ExportTalk is one show & tell sign-up as the closing surfaces carry it:
// the talk, its repo link and the presenter's Persona.
type ExportTalk struct {
	Title       string        `json:"title"`
	Description string        `json:"description"`
	RepoURL     string        `json:"repo_url"`
	Status      SignupStatus  `json:"status"`
	Presenter   ExportPersona `json:"presenter"`
}

// Talks is the show & tell list, presenter Personas only.
func (c *Closing) Talks() []ExportTalk {
	queue, err := c.showtell.Queue()
	if err != nil {
		logger("closing").Warn("closing show & tell", "err", err)
		return nil
	}
	out := make([]ExportTalk, 0, len(queue))
	for _, s := range queue {
		out = append(out, ExportTalk{
			Title:       s.Title,
			Description: s.Description,
			RepoURL:     s.RepoURL,
			Status:      s.Status,
			Presenter:   ExportPersona{Name: s.PersonaName, Symbol: s.PersonaSymbol, Color: s.PersonaColor, Tagline: s.PersonaTagline},
		})
	}
	return out
}

// ExportPost is one approved Post on the Board as the closing surfaces carry it:
// the text and the poster's Persona.
type ExportPost struct {
	Text   string        `json:"text"`
	Poster ExportPersona `json:"poster"`
}

// Posts is the Board's approved Posts, newest first, poster Personas only.
// Posts that are pending, rejected, deleted or taken down are not here.
func (c *Closing) Posts() []ExportPost {
	posts, err := c.board.Approved(-1)
	if err != nil {
		logger("closing").Warn("closing board", "err", err)
		return nil
	}
	out := make([]ExportPost, 0, len(posts))
	for _, p := range posts {
		out = append(out, ExportPost{
			Text:   p.Text,
			Poster: ExportPersona{Name: p.PersonaName, Symbol: p.PersonaSymbol, Color: p.PersonaColor},
		})
	}
	return out
}

// ExportMatch is one of the best LLM-generated matches, anonymized to the
// pair's Personas: the score, the reason and the icebreakers.
type ExportMatch struct {
	Score       int           `json:"score"`
	Reason      string        `json:"reason"`
	Icebreakers []string      `json:"icebreakers"`
	A           ExportPersona `json:"a"`
	B           ExportPersona `json:"b"`
}

// bestMatchesLimit caps the export pack's best-matches list.
const bestMatchesLimit = 3

// BestMatches ranks the event's LLM-generated match assessments by score —
// the best reasons and icebreakers, anonymized to the pair's Personas. One
// entry per pair (the lexicographically lower id speaks for it); a Match
// whose partner has left the event is skipped. An unreadable Participant
// list comes back empty.
func (c *Closing) BestMatches() []ExportMatch {
	participants, err := c.store.All()
	if err != nil {
		logger("closing").Warn("closing best matches", "err", err)
		return nil
	}
	byID := participantsByID(participants)
	out := []ExportMatch{}
	for _, p := range participants {
		if m, ok := exportMatchFor(p, byID); ok {
			out = append(out, m)
		}
	}
	sortBestMatches(out)
	if len(out) > bestMatchesLimit {
		out = out[:bestMatchesLimit]
	}
	return out
}

// participantsByID indexes the Participants by id, for the pair joins.
func participantsByID(participants []*Participant) map[string]*Participant {
	byID := make(map[string]*Participant, len(participants))
	for _, p := range participants {
		byID[p.ID] = p
	}
	return byID
}

// exportMatchFor renders one matched Participant's pair as an anonymized
// export match: the score, the reason and the icebreakers. One entry per
// pair — the lexicographically lower id speaks for it — and a Match whose
// partner has left the event is skipped. ok is false when the Participant
// does not name a pair to show.
func exportMatchFor(p *Participant, byID map[string]*Participant) (ExportMatch, bool) {
	if !p.IsMatched() || p.Assessment == nil {
		return ExportMatch{}, false
	}
	partner, ok := byID[p.MatchedWith]
	if !ok {
		return ExportMatch{}, false // the partner left the event; the pair is no longer nameable
	}
	if p.ID > partner.ID {
		return ExportMatch{}, false // the partner's row already spoke for the pair
	}
	return ExportMatch{
		Score:       p.Assessment.Score,
		Reason:      p.Assessment.Reason,
		Icebreakers: p.Assessment.Icebreakers,
		A:           personaOf(p),
		B:           personaOf(partner),
	}, true
}

// sortBestMatches ranks the export matches best-first, with the pair's
// Persona names breaking score ties.
func sortBestMatches(matches []ExportMatch) {
	sort.Slice(matches, func(i, j int) bool { return betterExportMatch(matches[i], matches[j]) })
}

// betterExportMatch orders by score first, then the pair's Persona names.
func betterExportMatch(a, b ExportMatch) bool {
	if a.Score != b.Score {
		return a.Score > b.Score
	}
	if a.A.Name != b.A.Name {
		return a.A.Name < b.A.Name
	}
	return a.B.Name < b.B.Name
}

// ExportPack is the host's post-meetup blast pack: the evening's headline
// stats, the full leaderboard, the top Personas, the best LLM-generated
// match reasons and icebreakers (anonymized to Personas), the show & tell
// list and the question count, with the success metrics derived. Personas
// and counts only — never contact-channel values, never GitHub identity
// mappings.
type ExportPack struct {
	Stats       EventStats       `json:"stats"`
	Success     SuccessMetrics   `json:"success"`
	Leaderboard []ExportStanding `json:"leaderboard"`
	TopPersonas []ExportPersona  `json:"top_personas"`
	BestMatches []ExportMatch    `json:"best_matches"`
	ShowTell    []ExportTalk     `json:"show_tell"`
	Board       []ExportPost     `json:"board"`
}

// topPersonasLimit caps the export pack's top-personas list.
const topPersonasLimit = 5

// Pack builds the host's export pack.
func (c *Closing) Pack() ExportPack {
	stats := c.Stats()
	board, err := c.meetings.Leaderboard()
	if err != nil {
		logger("closing").Warn("closing pack, leaderboard", "err", err)
	}
	ranked := standings(board)
	return ExportPack{
		Stats:       stats,
		Success:     successMetrics(stats, ranked),
		Leaderboard: ranked,
		TopPersonas: c.topPersonas(board),
		BestMatches: c.BestMatches(),
		ShowTell:    c.Talks(),
		Board:       c.Posts(),
	}
}

// topPersonas is the pack's top-personas list: the top of the leaderboard,
// with their taglines. The raw board carries the Participant ids for the
// join; the pack itself keeps personas only.
func (c *Closing) topPersonas(board []ConnectionStanding) []ExportPersona {
	taglines := c.personaTaglines()
	top := board
	if len(top) > topPersonasLimit {
		top = top[:topPersonasLimit]
	}
	out := make([]ExportPersona, 0, len(top))
	for _, s := range top {
		out = append(out, ExportPersona{
			Name:    s.Name,
			Symbol:  s.Symbol,
			Color:   s.Color,
			Tagline: taglines[s.ID],
		})
	}
	return out
}

// personaTaglines maps the Participants' ids to their Persona taglines; an
// unreadable Participant list comes back empty.
func (c *Closing) personaTaglines() map[string]string {
	taglines := map[string]string{}
	participants, err := c.store.All()
	if err != nil {
		logger("closing").Warn("closing pack, participants", "err", err)
		return taglines
	}
	for _, p := range participants {
		taglines[p.ID] = p.PersonaTagline
	}
	return taglines
}

// EventRecap is the public event recap's content: the evening's stats, the
// top connectors, the persona gallery and the show & tell list. Personas
// only — no contact data, no identities.
type EventRecap struct {
	Stats         EventStats
	TopConnectors []ExportStanding
	Gallery       []ExportPersona
	ShowTell      []ExportTalk
	Board         []ExportPost
}

// topConnectorsLimit caps the public recap's top-connectors list.
const topConnectorsLimit = 5

// Recap assembles the public event recap's content.
func (c *Closing) Recap() EventRecap {
	board := c.Leaderboard()
	top := board
	if len(top) > topConnectorsLimit {
		top = top[:topConnectorsLimit]
	}
	return EventRecap{
		Stats:         c.Stats(),
		TopConnectors: top,
		Gallery:       c.Gallery(),
		ShowTell:      c.Talks(),
		Board:         c.Posts(),
	}
}

// plural picks the singular or plural noun for a count.
func plural(singular string, n int) string {
	if n == 1 {
		return singular
	}
	return singular + "s"
}

// renderExportMarkdown renders the export pack as Markdown, ready to paste
// into the post-meetup blast. The same personas-and-counts rule holds: the
// Markdown carries no contact values and no identity mappings.
func renderExportMarkdown(pack ExportPack) string {
	var b strings.Builder
	b.WriteString("# Agentic Blind Date — the evening, recapped\n\n")
	fmt.Fprintf(&b, "%d participants · %d completed meetings · %d contact details exchanged · %d questions asked (%d answered)\n\n",
		pack.Stats.Participants, pack.Stats.Meetings, pack.Stats.ContactExchanges, pack.Stats.Questions, pack.Stats.QuestionsAnswered)
	renderSuccessMarkdown(&b, pack.Success)
	renderLeaderboardMarkdown(&b, pack.Leaderboard)
	renderTopPersonasMarkdown(&b, pack.TopPersonas)
	renderBestMatchesMarkdown(&b, pack.BestMatches)
	renderShowTellMarkdown(&b, pack.ShowTell)
	renderBoardMarkdown(&b, pack.Board)
	b.WriteString("\n---\n\n_Personas and counts only — no contact details, no handles._\n")
	return b.String()
}

// renderSuccessMarkdown renders the "How the evening went" section.
func renderSuccessMarkdown(b *strings.Builder, s SuccessMetrics) {
	b.WriteString("## How the evening went\n\n")
	fmt.Fprintf(b, "- 🤝 %d completed %s, %d %s made\n",
		s.Meetings, plural("meeting", s.Meetings),
		s.Connections, plural("connection", s.Connections))
	fmt.Fprintf(b, "- ❓ %d questions answered", s.QuestionsAnswered)
	if s.AudienceParticipation {
		b.WriteString(" — the audience-participation goal (10+) is met ✅\n")
	} else {
		b.WriteString(" — the audience-participation goal (10+) is not met yet\n")
	}
}

// renderLeaderboardMarkdown renders the leaderboard section, with the crown
// on the top entry.
func renderLeaderboardMarkdown(b *strings.Builder, board []ExportStanding) {
	b.WriteString("\n## 🏆 Leaderboard\n\n")
	if len(board) == 0 {
		b.WriteString("No connections were made.\n")
		return
	}
	for i, s := range board {
		trophy := ""
		if i == 0 {
			trophy = " 👑"
		}
		fmt.Fprintf(b, "%d. %s %s — %d %s%s\n",
			s.Rank, s.Symbol, s.Name, s.Connections, plural("connection", s.Connections), trophy)
	}
}

// renderTopPersonasMarkdown renders the top-personas section, taglines
// included when they exist.
func renderTopPersonasMarkdown(b *strings.Builder, personas []ExportPersona) {
	b.WriteString("\n## 🌟 Top personas\n\n")
	if len(personas) == 0 {
		b.WriteString("No participants.\n")
		return
	}
	for _, p := range personas {
		fmt.Fprintf(b, "- %s **%s**", p.Symbol, p.Name)
		if p.Tagline != "" {
			fmt.Fprintf(b, " — %s", p.Tagline)
		}
		b.WriteString("\n")
	}
}

// renderBestMatchesMarkdown renders the best-matches section, with the
// reason and the icebreakers when they exist.
func renderBestMatchesMarkdown(b *strings.Builder, matches []ExportMatch) {
	b.WriteString("\n## 💘 Best matches\n\n")
	if len(matches) == 0 {
		b.WriteString("No matches were scored.\n")
		return
	}
	for _, m := range matches {
		fmt.Fprintf(b, "- %s %s 💘 %s %s — %d%%\n", m.A.Symbol, m.A.Name, m.B.Symbol, m.B.Name, m.Score)
		if m.Reason != "" {
			fmt.Fprintf(b, "  > %s\n", m.Reason)
		}
		for _, ice := range m.Icebreakers {
			fmt.Fprintf(b, "  - %s\n", ice)
		}
	}
}

// renderShowTellMarkdown renders the show & tell section.
func renderShowTellMarkdown(b *strings.Builder, talks []ExportTalk) {
	b.WriteString("\n## 🎤 Show & tell\n\n")
	if len(talks) == 0 {
		b.WriteString("No talks were signed up.\n")
		return
	}
	for _, talk := range talks {
		renderTalkMarkdown(b, talk)
	}
}

// renderTalkMarkdown renders one talk: its presented state, and its
// description and repo link when they exist.
func renderTalkMarkdown(b *strings.Builder, talk ExportTalk) {
	fmt.Fprintf(b, "- **%s** — %s %s", talk.Title, talk.Presenter.Symbol, talk.Presenter.Name)
	if talk.Status == SignupPresented {
		b.WriteString(" (presented)")
	}
	b.WriteString("\n")
	if talk.Description != "" {
		fmt.Fprintf(b, "  %s\n", talk.Description)
	}
	if talk.RepoURL != "" {
		fmt.Fprintf(b, "  <%s>\n", talk.RepoURL)
	}
}

// renderBoardMarkdown renders the board section, one line per Post.
func renderBoardMarkdown(b *strings.Builder, posts []ExportPost) {
	b.WriteString("\n## 📣 Board\n\n")
	if len(posts) == 0 {
		b.WriteString("Nothing was posted.\n")
		return
	}
	for _, post := range posts {
		// One line per Post, so a multi-line Post cannot break the list.
		fmt.Fprintf(b, "- %s **%s**: %s\n", post.Poster.Symbol, post.Poster.Name, strings.Join(strings.Fields(post.Text), " "))
	}
}
