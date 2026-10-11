package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Badge represents a visual badge displayed on participant cards.
type Badge struct {
	Icon  string
	Label string
	Color string
}

func computeBadges(p GitHubProfile) []Badge {
	var out []Badge
	years := p.AccountAgeDays / 365
	if years >= 10 {
		out = append(out, Badge{"🦕", "GitHub Dinosaur", "bg-yellow-200"})
	}
	if p.TotalStars >= 100 {
		out = append(out, Badge{"⭐", "GitHub Celebrity", "bg-amber-200"})
	}
	if p.PublicRepos >= 50 {
		out = append(out, Badge{"🗂", "The Hoarder", "bg-orange-200"})
	}
	if len(p.Languages) >= 5 {
		out = append(out, Badge{"🌍", "Polyglot", "bg-green-200"})
	}
	if p.HasProfileReadme {
		out = append(out, Badge{"📖", "Storyteller", "bg-blue-200"})
	}
	if p.AccountAgeDays > 0 && p.AccountAgeDays < 180 {
		out = append(out, Badge{"🆕", "Fresh Blood", "bg-pink-200"})
	}
	if p.Followers >= 100 && p.PublicRepos < 5 {
		out = append(out, Badge{"🔭", "Lurker", "bg-purple-200"})
	}
	if len(p.Languages) == 1 {
		out = append(out, Badge{"🎯", "Focused", "bg-teal-200"})
	}
	return out
}

type graphNode struct {
	ID          string `json:"id"`
	PersonaName string `json:"persona_name"`
	Color       string `json:"color"`
	Symbol      string `json:"symbol"`
	Step        string `json:"step"`
	Ready       bool   `json:"ready"`
	Matched     bool   `json:"matched"`
}

type graphEdge struct {
	Source  string `json:"source"`
	Target  string `json:"target"`
	Score   int    `json:"score"`
	Matched bool   `json:"matched"`
	Meeting bool   `json:"meeting"`
}

// Handler handles HTTP requests for the web application.
type Handler struct {
	db          *DB
	store       *ParticipantStore
	events      *EventStore
	meetings    *Meetings
	channels    *ContactChannels
	recaps      *Recaps
	missions    *Missions
	showtell    *ShowTell
	onboarding  *Onboarding
	interview   *Interview
	matcher     *Matcher
	relations   *Relationships
	matchmaking *Matchmaking
	tmpl        *template.Template
	streams     StreamObserver
}

// StreamObserver is told each time an SSE stream has looked at the current
// state, so a test can wait for a stream to catch up. Production wires nil:
// nothing observes streams outside tests.
type StreamObserver interface {
	OnStream(path string)
}

// NewHandler creates a new Handler with the given dependencies.
// It initializes the templates and takes the modules the pages use.
// streams may be nil: production observes nothing.
func NewHandler(db *DB, onboarding *Onboarding, interview *Interview, matcher *Matcher, relations *Relationships, matchmaking *Matchmaking, missions *Missions, showtell *ShowTell, streams StreamObserver) *Handler {
	funcs := template.FuncMap{
		"add":    func(a, b int) int { return a + b },
		"badges": func(p GitHubProfile) []Badge { return computeBadges(p) },
		// Only called with a non-empty question set and constant divisors.
		"percent":   func(n, total int) int { return n * 100 / total },
		"divInt":    func(a, b int) int { return a / b },
		"colorName": paletteName,
		"textColor": paletteText,
	}
	tmpl := template.Must(template.New("").Funcs(funcs).ParseGlob(filepath.Join("templates", "*.html")))
	events := NewEventStore(db)
	meetings := NewMeetings(db, events)
	return &Handler{
		db: db, store: NewParticipantStore(db), events: events, meetings: meetings,
		channels: NewContactChannels(db), recaps: NewRecaps(db), missions: missions, showtell: showtell,
		onboarding: onboarding, interview: interview, matcher: matcher, relations: relations, matchmaking: matchmaking,
		tmpl: tmpl, streams: streams,
	}
}

func (h *Handler) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("template %s error: %v", name, err)
		http.Error(w, "render error", 500)
	}
}

const cookieName = "participant_id"
const cookieMaxAge = 7 * 24 * 60 * 60 // 7 days

// ── /user ──────────────────────────────────────────────────────────────────

// GET /user
func (h *Handler) Landing(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		if p, err := h.store.Get(c.Value); err == nil {
			http.Redirect(w, r, "/user/onboard/"+p.ID, http.StatusSeeOther)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: cookieName, Path: "/", MaxAge: -1})
	}
	h.render(w, "landing.html", nil)
}

func setParticipantCookie(w http.ResponseWriter, id string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    id,
		Path:     "/",
		MaxAge:   cookieMaxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// POST /user/join
func (h *Handler) Join(w http.ResponseWriter, r *http.Request) {
	handle := strings.TrimSpace(r.FormValue("github"))
	handle = strings.TrimPrefix(handle, "@")
	handle = strings.TrimPrefix(handle, "https://github.com/")
	handle = strings.TrimSuffix(handle, "/")
	name := strings.TrimSpace(r.FormValue("name"))
	noGitHub := r.FormValue("no_github") == "on"

	if name == "" {
		http.Error(w, "Name is required", 400)
		return
	}

	if !noGitHub && handle == "" {
		http.Error(w, "GitHub handle required", 400)
		return
	}

	id, err := h.onboarding.Register(name, handle, !noGitHub)
	if err != nil {
		log.Printf("Registration failed for handle=%s, name=%s: %v", handle, name, err)
		http.Error(w, "registration failed: "+err.Error(), 500)
		return
	}
	setParticipantCookie(w, id)
	http.Redirect(w, r, "/user/onboard/"+id, http.StatusSeeOther)
}

// GET /user/onboard/{id}
func (h *Handler) Onboard(w http.ResponseWriter, r *http.Request) {
	p, err := h.store.Get(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if dest := h.destination(p); dest != r.URL.Path {
		http.Redirect(w, r, dest, http.StatusSeeOther)
		return
	}
	h.render(w, "onboard.html", p)
}

// GET /user/pipeline/{id}  — HTMX, refreshed when the pipeline stream says so
func (h *Handler) PipelineStatus(w http.ResponseWriter, r *http.Request) {
	p, err := h.store.Get(r.PathValue("id"))
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}

	if dest := h.destination(p); dest != "/user/onboard/"+p.ID {
		w.Header().Set("HX-Redirect", dest)
		return
	}
	if qd := h.interview.Next(p); qd != nil && p.PipelineStep == StepInterviewing {
		h.render(w, "fragment-question.html", qd)
		return
	}
	h.render(w, "fragment-pipeline-step.html", p)
}

// POST /user/answer/{id}
func (h *Handler) SubmitAnswer(w http.ResponseWriter, r *http.Request) {
	p, err := h.store.Get(r.PathValue("id"))
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}

	if p.PipelineStep != StepInterviewing {
		w.Header().Set("HX-Redirect", h.destination(p))
		return
	}

	done, err := h.onboarding.Answer(p, r.FormValue("answer"))
	var invalid *InvalidAnswerError
	switch {
	case errors.Is(err, ErrInterviewOver), errors.As(err, &invalid):
		http.Error(w, err.Error(), 400)
		return
	case err != nil:
		http.Error(w, "saving answer failed", 500)
		return
	}

	if done {
		w.Header().Set("HX-Redirect", h.destination(p))
		return
	}

	h.render(w, "fragment-question.html", h.interview.Next(p))
}

// GET /user/wait/{id}
func (h *Handler) Wait(w http.ResponseWriter, r *http.Request) {
	p, err := h.store.Get(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if dest := h.destination(p); dest != r.URL.Path {
		http.Redirect(w, r, dest, http.StatusSeeOther)
		return
	}

	profile := GitHubProfile{}
	if p.Profile != nil {
		profile = *p.Profile
	}
	answers := p.Answers // reading a nil map is fine
	questions := p.Questions

	type QAPair struct {
		Question string
		Answer   string
	}
	var qaPairs []QAPair
	for _, q := range questions {
		if ans, ok := answers[q.ID]; ok {
			qaPairs = append(qaPairs, QAPair{Question: q.Text, Answer: ans})
		}
	}

	data := h.waitStatusData(p)
	data["Profile"] = profile
	data["QAPairs"] = qaPairs
	data["MeetStatus"] = h.meetStatusData(p)
	data["MissionCard"] = h.missionCardData(p)
	data["ShowTell"] = h.showtellData(p)
	data["Settings"] = h.settingsData(p)
	data["RecapLink"] = h.recapLinkData(p)
	h.render(w, "wait.html", data)
}

// GET /user/wait-status/{id}  — HTMX, refreshed when the wait stream says so
func (h *Handler) WaitStatus(w http.ResponseWriter, r *http.Request) {
	p, err := h.store.Get(r.PathValue("id"))
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	if dest := h.destination(p); dest != "/user/wait/"+p.ID {
		w.Header().Set("HX-Redirect", dest)
		return
	}
	h.render(w, "fragment-wait-status.html", h.waitStatusData(p))
}

// waitStatusData is the wait page's status section. A matched Participant
// sees the teaser — their future Match's emoji and colour and a hint at what
// the pair disagreed on, never the partner's identity, score or dossier. An
// unmatched Participant sees the ready count while the event is still
// onboarding, and a clear no-match state once the Social has begun.
func (h *Handler) waitStatusData(p *Participant) map[string]any {
	data := map[string]any{
		"Participant": p,
		"Count":       h.db.ReadyCount(),
		"Revealed":    h.events.State().AtLeast(PhaseSocial),
	}
	partner, _, err := h.relations.PartnerOf(p.ID)
	if err != nil {
		log.Printf("Wait status for %s: %v", p.ID, err)
	}
	if partner != nil {
		data["Partner"] = partner
		data["Hint"] = teaserHint(p, partner)
	}
	return data
}

// teaserHint is the curiosity gap on the wait page: the first question the
// pair answered differently, phrased as a promise of the Reveal. It carries no
// identity, score or dossier content.
func teaserHint(me, partner *Participant) string {
	for _, q := range me.Questions {
		if ans, ok := me.Answers[q.ID]; ok {
			if other, ok := partner.Answers[q.ID]; ok && other != ans {
				return fmt.Sprintf("You disagreed on %q — reveal at the break", q.Text)
			}
		}
	}
	return "You seem to think alike — reveal at the break"
}

// GET /user/match/{id}
func (h *Handler) Match(w http.ResponseWriter, r *http.Request) {
	p, err := h.store.Get(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if dest := h.destination(p); dest != r.URL.Path {
		http.Redirect(w, r, dest, http.StatusSeeOther)
		return
	}

	dossier, match := h.dossierData(p)
	h.render(w, "match.html", map[string]any{
		"Me":          p,
		"Match":       match,
		"Score":       dossier["Score"],
		"Dossier":     dossier,
		"MeetStatus":  h.meetStatusData(p),
		"MissionCard": h.missionCardData(p),
		"ShowTell":    h.showtellData(p),
		"Settings":    h.settingsData(p),
		"RecapLink":   h.recapLinkData(p),
	})
}

// dossierData is the Match page's dossier: the fun payload of the Match — the
// reason, the flags and the icebreakers — locked until the pair's dossier
// unlocks (their Meeting completed, or the event closed). The score is not
// part of the dossier: it is revealed with the Match itself. The second
// return value is the partner, nil without a Match.
func (h *Handler) dossierData(p *Participant) (map[string]any, *Participant) {
	match, result, err := h.relations.PartnerOf(p.ID)
	if err != nil {
		log.Printf("Dossier for %s: %v", p.ID, err)
	}
	if result == nil {
		result = &matchResult{RedFlags: []string{}, GreenFlags: []string{}, Icebreakers: []string{}}
	}
	unlocked := false
	if match != nil {
		unlocked = h.dossierUnlocked(p)
	}
	return map[string]any{
		"Me":          p,
		"Match":       match,
		"Score":       result.Score,
		"Reason":      result.Reason,
		"RedFlags":    result.RedFlags,
		"GreenFlags":  result.GreenFlags,
		"Icebreakers": result.Icebreakers,
		"Unlocked":    unlocked,
	}, match
}

// GET /user/dossier/{id} — HTMX, the Match page's dossier section, refreshed
// when the match stream says so — the pair's Meeting completing unlocks it.
// Without a Match there is no dossier.
func (h *Handler) Dossier(w http.ResponseWriter, r *http.Request) {
	p, err := h.store.Get(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data, match := h.dossierData(p)
	if match == nil {
		http.NotFound(w, r)
		return
	}
	h.render(w, "fragment-dossier.html", data)
}

// ── mission cards ──────────────────────────────────────────────────────────

// GET /user/mission/{id} — HTMX, the personal page's mission card section,
// refreshed when the page's stream says so — the card appearing (generation)
// and meeting completions (the stamps) arrive by push.
func (h *Handler) MissionCard(w http.ResponseWriter, r *http.Request) {
	p, err := h.store.Get(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h.render(w, "fragment-mission-card.html", h.missionCardData(p))
}

// missionTileView is a Mission tile as the personal page and the debug
// endpoint show it: the tile plus its stamp state.
type missionTileView struct {
	MissionTile
	Stamped bool `json:"stamped"`
}

// missionCardData is the personal page's mission card section: the card's
// tiles with their stamp state (derived from completed meetings) and the
// live progress — connections and rank vs the top 5. Without a card it is
// the preparing placeholder while the event allows cards, and empty
// otherwise.
func (h *Handler) missionCardData(p *Participant) map[string]any {
	data := map[string]any{
		"Me":        p,
		"Preparing": h.events.State().MissionCardsLive(),
	}
	card, err := h.missions.Get(p.ID)
	if err != nil {
		log.Printf("Mission card for %s: %v", p.ID, err)
	}
	if card == nil {
		return data
	}
	tiles := h.stampedTiles(p, card)
	data["Tiles"] = tiles
	data["Grid"] = tileGrid(tiles)
	data["Connections"] = h.meetings.Connections(p.ID)
	board, err := h.meetings.Leaderboard()
	if err != nil {
		log.Printf("Connection leaderboard for %s: %v", p.ID, err)
	}
	data["Rank"] = rankOf(board, p.ID)
	top := board
	if len(top) > 5 {
		top = top[:5]
	}
	data["Top"] = top
	return data
}

// stampedTiles joins the card's tiles with the Participant's completed
// meetings: a tile is stamped when a completed meeting exists with that
// person. Stamps derive from the meetings — there is no stamped_at column.
func (h *Handler) stampedTiles(p *Participant, card *MissionCard) []missionTileView {
	stamped, err := h.missions.Stamped(p.ID)
	if err != nil {
		log.Printf("Mission card stamps for %s: %v", p.ID, err)
	}
	tiles := make([]missionTileView, 0, len(card.Tiles))
	for _, t := range card.Tiles {
		tiles = append(tiles, missionTileView{MissionTile: t, Stamped: stamped[t.PersonID]})
	}
	return tiles
}

// tileGrid lays the tiles out on the 3×3 grid by position; positions without
// a tile stay empty, so the grid keeps its shape.
func tileGrid(tiles []missionTileView) [9]*missionTileView {
	var grid [9]*missionTileView
	for i := range tiles {
		if tiles[i].Position >= 0 && tiles[i].Position < len(grid) {
			grid[tiles[i].Position] = &tiles[i]
		}
	}
	return grid
}

// rankOf is the Participant's 1-based rank on the connection leaderboard,
// or 0 when they are not on it.
func rankOf(board []ConnectionStanding, id string) int {
	for i, c := range board {
		if c.ID == id {
			return i + 1
		}
	}
	return 0
}

// ── meetings: the confirm handshake ────────────────────────────────────────

// GET /user/m/{id} — the meet page: the target's persona card and the confirm
// button. With ?me={code} the confirmer is resolved from their own meet code
// and the participant cookie is set — the same trust level as the code-based
// confirm itself.
func (h *Handler) Meet(w http.ResponseWriter, r *http.Request) {
	target, err := h.store.Get(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if code := normalizeMeetCode(r.URL.Query().Get("me")); code != "" {
		me, err := h.store.GetByMeetCode(code)
		if err != nil {
			http.Error(w, "Hmm, that meet code doesn't belong to anyone at this event.", 400)
			return
		}
		setParticipantCookie(w, me.ID)
		http.Redirect(w, r, "/user/m/"+target.ID, http.StatusSeeOther)
		return
	}

	var me *Participant
	if c, err := r.Cookie(cookieName); err == nil {
		if p, err := h.store.Get(c.Value); err == nil {
			me = p
		}
	}
	allowed := h.events.State().AllowsMeetings()

	var already, completed bool
	if me != nil && me.ID != target.ID {
		if m, err := h.meetings.Between(me.ID, target.ID); err == nil && m != nil {
			already = m.ConfirmedBy(me.ID)
			completed = m.Completed()
		}
	}

	h.render(w, "meet.html", map[string]any{
		"Target":    target,
		"Me":        me,
		"Allowed":   allowed,
		"Already":   already,
		"Completed": completed,
	})
}

// POST /user/met/{id} — confirm the meeting with the target named by the
// "code" form value. {id} is the confirmer's own id; the confirmer is
// identified by the participant cookie.
func (h *Handler) ConfirmMeeting(w http.ResponseWriter, r *http.Request) {
	confirmerID := r.PathValue("id")
	target, err := h.store.GetByMeetCode(normalizeMeetCode(r.FormValue("code")))
	if err != nil {
		http.Error(w, "Hmm, that meet code doesn't belong to anyone at this event.", 400)
		return
	}
	confirmer, err := h.store.Get(confirmerID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if c, err := r.Cookie(cookieName); err != nil || c.Value != confirmerID {
		http.Error(w, "We don't know who you are — open your meet page and enter your meet code.", 400)
		return
	}
	meeting, already, err := h.meetings.Confirm(confirmer, target, OriginOther)
	switch {
	case errors.Is(err, ErrSelfMeeting):
		http.Error(w, "You can't meet yourself — go find someone else!", 400)
	case errors.Is(err, ErrMeetingsClosed):
		http.Error(w, "Meeting confirmation is only open during the social and show & tell.", 400)
	case err != nil:
		http.Error(w, "confirming the meeting failed: "+err.Error(), 500)
	default:
		h.render(w, "fragment-meet-result.html", map[string]any{
			"Target":    target,
			"Already":   already,
			"Completed": meeting.Completed(),
		})
	}
}

// GET /user/meet-status/{id} — HTMX, the personal page's meet section,
// refreshed when the page's stream says so.
func (h *Handler) MeetStatus(w http.ResponseWriter, r *http.Request) {
	p, err := h.store.Get(r.PathValue("id"))
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	h.render(w, "fragment-meet-status.html", h.meetStatusData(p))
}

// meetStatusData is the personal page's meet section: the Participant's meet
// code and meet-link QR, the "I met someone" entry point and their meetings —
// phase-gated to the social and show & tell. A completed meeting carries the
// exchange prompt's data: what the other side shares with them and what they
// share with the other side — the mutual rule decides what is revealed.
func (h *Handler) meetStatusData(p *Participant) map[string]any {
	type meetingView struct {
		Other    *Participant
		Status   MeetingStatus
		Revealed []ContactChannel // what the other side shares with me
		Shared   []ContactChannel // what I share with them
	}
	var meetings []meetingView
	if h.events.State().AllowsMeetings() {
		ms, _ := h.meetings.ForParticipant(p.ID)
		for _, m := range ms {
			other, err := h.store.Get(m.Other(p.ID))
			if err != nil {
				continue // the other side left the event; the row is debug-only
			}
			view := meetingView{Other: other, Status: m.StatusFor(p.ID)}
			revealed, err := h.channels.Revealed(other.ID, p.ID)
			if err != nil {
				log.Printf("Channels revealed to %s by %s: %v", p.ID, other.ID, err)
			}
			shared, err := h.channels.Revealed(p.ID, other.ID)
			if err != nil {
				log.Printf("Channels shared by %s with %s: %v", p.ID, other.ID, err)
			}
			view.Revealed = revealed
			view.Shared = shared
			meetings = append(meetings, view)
		}
	}
	return map[string]any{
		"Me":          p,
		"Allowed":     h.events.State().AllowsMeetings(),
		"Meetings":    meetings,
		"Connections": h.meetings.Connections(p.ID),
	}
}

// GET /user/match-stream/{id}
// Tells the match page to refresh its meet section and its dossier when the
// Participant's meetings or the phase change — the pair's Meeting completing
// unlocks the dossier.
func (h *Handler) MatchStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	first, err := h.store.Get(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !sseHeaders(w) {
		return
	}
	last := h.meetSignature(first)
	h.watch(w, r, func() bool {
		p, err := h.store.Get(id)
		if err != nil {
			return true
		}
		if now := h.meetSignature(p); now != last {
			last = now
			sseEvent(w, "refresh")
		}
		return false
	})
}

// meetSignature is what the meet streams watch: the phase, the Participant's
// meetings (their count and how many are completed), whether their Mission
// card exists — a freshly generated card appears via push — and their show &
// tell sign-up state.
type meetSignature struct {
	phase     Phase
	meetings  int
	completed int
	card      bool
	showtell  string
}

func (h *Handler) meetSignature(p *Participant) meetSignature {
	ms, _ := h.meetings.ForParticipant(p.ID)
	completed := 0
	for _, m := range ms {
		if m.Completed() {
			completed++
		}
	}
	return meetSignature{h.events.State(), len(ms), completed, h.missions.Has(p.ID), h.showtell.Signature(p.ID)}
}

// ── contact exchange: settings + the post-meeting prompt ───────────────────

// GET /user/settings/{id} — HTMX, the personal page's settings section: the
// Participant's contact channels, entered once, off by default.
func (h *Handler) Settings(w http.ResponseWriter, r *http.Request) {
	p, err := h.store.Get(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h.render(w, "fragment-settings.html", h.settingsData(p))
}

// POST /user/settings/{id} — saves the Participant's contact channels.
func (h *Handler) SaveSettings(w http.ResponseWriter, r *http.Request) {
	p, err := h.store.Get(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var channels []ContactChannel
	for _, name := range ContactChannelNames {
		channels = append(channels, ContactChannel{
			Channel: name,
			Value:   strings.TrimSpace(r.FormValue(name)),
			Enabled: r.FormValue(name+"_on") == "on",
		})
	}
	if err := h.channels.Save(p.ID, channels); err != nil {
		http.Error(w, "saving your contact details failed: "+err.Error(), 500)
		return
	}
	data := h.settingsData(p)
	data["Saved"] = true
	h.render(w, "fragment-settings.html", data)
}

// settingsData is the personal page's settings section: the Participant's
// contact channels — entered once, off by default — with the github channel
// pre-filled from their GitHub profile.
func (h *Handler) settingsData(p *Participant) map[string]any {
	channels, err := h.channels.Channels(p.ID)
	if err != nil {
		log.Printf("Contact channels for %s: %v", p.ID, err)
	}
	for i := range channels {
		if channels[i].Channel == "github" && channels[i].Value == "" {
			channels[i].Value = githubHandle(p)
		}
	}
	return map[string]any{"Me": p, "Channels": channels}
}

// githubHandle is the Participant's GitHub handle, pre-filled into the github
// contact channel; empty for Non-GitHub Users.
func githubHandle(p *Participant) string {
	if !p.HasGitHub {
		return ""
	}
	if p.Profile != nil && p.Profile.Login != "" {
		return p.Profile.Login
	}
	return p.GitHubHandle
}

// ── the personal recap ─────────────────────────────────────────────────────

// GET /user/recap-link/{id} — HTMX, the personal page's recap link: during
// closed, the Participant's recap page as a QR — the unguessable link to
// their evening, persistent across restarts.
func (h *Handler) RecapLink(w http.ResponseWriter, r *http.Request) {
	p, err := h.store.Get(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h.render(w, "fragment-recap-link.html", h.recapLinkData(p))
}

// recapLinkData is the personal page's recap link: the Participant's recap
// token while the event is closed — before that there is nothing to show.
func (h *Handler) recapLinkData(p *Participant) map[string]any {
	data := map[string]any{"Me": p}
	if h.events.State() == PhaseClosed {
		token, err := h.recaps.TokenFor(p.ID)
		if err != nil {
			log.Printf("Recap token for %s: %v", p.ID, err)
		} else {
			data["Token"] = token
		}
	}
	return data
}

// GET /user/recap/{token} — the personal recap page: the Participant's
// evening, kept — their persona card, their match dossier (always unlocked
// here), every connection with the channels they shared, the channels they
// shared themselves, and their stats. The token is the unguessable UUID; an
// unknown token is a 404.
func (h *Handler) Recap(w http.ResponseWriter, r *http.Request) {
	p, err := h.recaps.ByToken(r.PathValue("token"))
	if err != nil {
		http.Error(w, "loading the recap failed: "+err.Error(), 500)
		return
	}
	if p == nil {
		http.NotFound(w, r)
		return
	}
	h.render(w, "recap.html", h.recapData(p))
}

// recapData is the recap page's data: the Participant's persona card, their
// match dossier (always unlocked here — the evening is theirs to keep), all
// their connections with the channels those connections shared with them,
// the channels they shared themselves, and their personal stats.
func (h *Handler) recapData(p *Participant) map[string]any {
	dossier, match := h.dossierData(p)
	dossier["Unlocked"] = true // the recap always shows the dossier

	type connectionView struct {
		Other    *Participant
		Revealed []ContactChannel
	}
	var connections []connectionView
	ms, err := h.meetings.ForParticipant(p.ID)
	if err != nil {
		log.Printf("Recap meetings for %s: %v", p.ID, err)
	}
	for _, m := range ms {
		if !m.Completed() {
			continue // a connection is a completed meeting
		}
		other, err := h.store.Get(m.Other(p.ID))
		if err != nil {
			continue // the other side left the event
		}
		revealed, err := h.channels.Revealed(other.ID, p.ID)
		if err != nil {
			log.Printf("Recap channels revealed to %s by %s: %v", p.ID, other.ID, err)
		}
		connections = append(connections, connectionView{Other: other, Revealed: revealed})
	}

	mine, err := h.channels.Channels(p.ID)
	if err != nil {
		log.Printf("Recap contact channels for %s: %v", p.ID, err)
	}

	board, err := h.meetings.Leaderboard()
	if err != nil {
		log.Printf("Recap leaderboard for %s: %v", p.ID, err)
	}
	champion, err := h.meetings.Champion()
	if err != nil {
		log.Printf("Recap champion for %s: %v", p.ID, err)
	}

	return map[string]any{
		"Me":              p,
		"Match":           match,
		"Dossier":         dossier,
		"Connections":     connections,
		"Shared":          shared(mine),
		"ConnectionCount": h.meetings.Connections(p.ID),
		"Rank":            rankOf(board, p.ID),
		"Champion":        champion != nil && champion.ID == p.ID,
	}
}

// ── show & tell: sign-up, the host queue, presenter mode ───────────────────

// POST /user/showtell/signup/{id} — sign up for show & tell (a title, a
// one-line description and an optional repo link), or update the sign-up.
// Open during the social and show & tell only.
func (h *Handler) ShowTellSignup(w http.ResponseWriter, r *http.Request) {
	p, err := h.store.Get(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	err = h.showtell.Signup(p, r.FormValue("title"), r.FormValue("description"), r.FormValue("repo"))
	switch {
	case errors.Is(err, ErrShowTellClosed):
		http.Error(w, "Show & tell sign-up is only open during the social and show & tell.", 400)
	case errors.Is(err, ErrInvalidSignup):
		http.Error(w, err.Error(), 400)
	case err != nil:
		http.Error(w, "signing up failed: "+err.Error(), 500)
	default:
		data := h.showtellData(p)
		data["Saved"] = true
		h.render(w, "fragment-showtell.html", data)
	}
}

// GET /user/showtell/{id} — HTMX, the personal page's show & tell section,
// refreshed when the page's stream says so.
func (h *Handler) ShowTellStatus(w http.ResponseWriter, r *http.Request) {
	p, err := h.store.Get(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h.render(w, "fragment-showtell.html", h.showtellData(p))
}

// showtellData is the personal page's show & tell section: the sign-up form —
// open during the social and show & tell — with the Participant's sign-up and
// its place in the host's queue, if any.
func (h *Handler) showtellData(p *Participant) map[string]any {
	data := map[string]any{
		"Me":   p,
		"Open": h.events.State().AllowsShowTell(),
	}
	signup, err := h.showtell.ForParticipant(p.ID)
	if err != nil {
		log.Printf("Show & tell sign-up for %s: %v", p.ID, err)
	}
	if signup != nil {
		data["Signup"] = signup
		data["Position"] = h.showtell.QueuePosition(p.ID)
	}
	return data
}

// POST /admin/showtell/stage — the host's presenter-mode actions on a sign-up
// (the "id" form value): "stage" puts it on stage with the Hype Agent's intro,
// "reveal" flips the presenter's card to their GitHub handle/name — the only
// place an identity leaves the server besides the owner's own recap — and
// "done" marks the talk presented.
func (h *Handler) ShowTellStage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "That sign-up is not in the queue.", 400)
		return
	}
	switch r.FormValue("action") {
	case "stage":
		_, err = h.showtell.Stage(id)
	case "reveal":
		_, err = h.showtell.RevealIdentity(id)
	case "done":
		_, err = h.showtell.Done(id)
	default:
		http.Error(w, "Unknown action — stage, reveal or done.", 400)
		return
	}
	switch {
	case errors.Is(err, ErrShowTellClosed):
		http.Error(w, "Presenter mode is only live during show & tell.", 400)
	case errors.Is(err, ErrSignupNotFound):
		http.Error(w, "That sign-up is not in the queue.", 404)
	case errors.Is(err, ErrAlreadyOnStage), errors.Is(err, ErrNotOnStage), errors.Is(err, ErrSignupNotQueued):
		http.Error(w, err.Error(), 400)
	case err != nil:
		http.Error(w, "updating the show & tell queue failed: "+err.Error(), 500)
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// POST /admin/showtell/move — reorder the queue: the "id" form value is the
// sign-up, "dir" is up or down.
func (h *Handler) ShowTellMove(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "That sign-up is not in the queue.", 400)
		return
	}
	if err := h.showtell.Move(id, r.FormValue("dir")); errors.Is(err, ErrInvalidDirection) {
		http.Error(w, err.Error(), 400)
	} else if err != nil {
		http.Error(w, "reordering the queue failed: "+err.Error(), 500)
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// POST /admin/showtell/remove — take a sign-up out of the queue.
func (h *Handler) ShowTellRemove(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "That sign-up is not in the queue.", 400)
		return
	}
	if err := h.showtell.Remove(id); err != nil {
		http.Error(w, "removing the sign-up failed: "+err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// GET /admin/showtell-queue — HTMX, the host console's show & tell queue,
// refreshed when the queue stream says so.
func (h *Handler) ShowTellQueue(w http.ResponseWriter, r *http.Request) {
	h.render(w, "fragment-showtell-queue.html", h.showtellQueueData())
}

// showtellQueueData is the host console's show & tell queue: every sign-up
// with its presenter's Persona and status — the queue, the stage and the
// already-presented talks.
func (h *Handler) showtellQueueData() map[string]any {
	queue, err := h.showtell.Queue()
	if err != nil {
		log.Printf("Show & tell queue: %v", err)
	}
	return map[string]any{
		"Open":  h.events.State().AllowsShowTell(),
		"Queue": queue,
	}
}

// GET /admin/showtell-stream — tells the host console to refresh the show &
// tell queue when it changes.
func (h *Handler) ShowTellStream(w http.ResponseWriter, r *http.Request) {
	if !sseHeaders(w) {
		return
	}
	last := h.showtellQueueSignature()
	h.watch(w, r, func() bool {
		if now := h.showtellQueueSignature(); now != last {
			last = now
			sseEvent(w, "refresh")
		}
		return false
	})
}

// showtellQueueSignature is what the queue stream watches: every sign-up's id,
// status, place and reveal state.
func (h *Handler) showtellQueueSignature() string {
	queue, err := h.showtell.Queue()
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, s := range queue {
		fmt.Fprintf(&b, "%d:%s:%d:%t;", s.ID, s.Status, s.Position, s.IdentityRevealed)
	}
	return b.String()
}

// ── /bigscreen ─────────────────────────────────────────────────────────────

// GET /bigscreen
func (h *Handler) Screen(w http.ResponseWriter, r *http.Request) {
	h.render(w, "screen.html", map[string]any{"Palette": paletteHex()})
}

func (h *Handler) buildGraphPayload() map[string]any {
	phase := h.events.State()
	participants, _ := h.store.All()
	activity, _ := h.db.GetRecentActivity(3)

	nodes := make([]graphNode, 0, len(participants))
	for _, p := range participants {
		// The Big Screen shows persona names only, in every phase — identities
		// never leave the server in the graph payload.
		nodes = append(nodes, graphNode{
			ID:          p.ID,
			PersonaName: p.PersonaName,
			Color:       p.PersonaColor,
			Symbol:      p.PersonaSymbol,
			Step:        string(p.PipelineStep),
			Ready:       p.PipelineStep.IsReady(),
			Matched:     p.IsMatched(),
		})
	}

	edges := make([]graphEdge, 0)
	seen := map[string]bool{}

	for _, p := range participants {
		if p.MatchedWith == "" {
			continue
		}
		key := p.ID + ":" + p.MatchedWith
		rev := p.MatchedWith + ":" + p.ID
		if seen[key] || seen[rev] {
			continue
		}
		seen[key] = true
		edges = append(edges, graphEdge{
			Source:  p.ID,
			Target:  p.MatchedWith,
			Score:   p.Assessment.Score,
			Matched: true,
		})
	}

	// Meeting edges: every completed meeting joins the graph alongside the
	// match edges. A pair that are matched and met keeps one edge, flagged as
	// both — the meeting is the stronger fact on the screen. The map holds
	// edge indexes, not pointers: appending below may reallocate the slice.
	byPair := map[string]int{}
	for i := range edges {
		byPair[edges[i].Source+":"+edges[i].Target] = i
		byPair[edges[i].Target+":"+edges[i].Source] = i
	}
	meetings, _ := h.meetings.All()
	for _, m := range meetings {
		if !m.Completed() {
			continue
		}
		key := m.A + ":" + m.B
		if i, ok := byPair[key]; ok {
			edges[i].Meeting = true
			continue
		}
		seen[key] = true
		edges = append(edges, graphEdge{Source: m.A, Target: m.B, Meeting: true})
	}

	type scored struct {
		id    string
		score int
	}
	topEdges := map[string][]scored{}
	for i, a := range participants {
		for j, b := range participants {
			if i >= j {
				continue
			}
			key := a.ID + ":" + b.ID
			rev := b.ID + ":" + a.ID
			if seen[key] || seen[rev] {
				continue
			}
			s := h.matcher.PairScore(a, b)
			topEdges[a.ID] = append(topEdges[a.ID], scored{b.ID, s})
			topEdges[b.ID] = append(topEdges[b.ID], scored{a.ID, s})
		}
	}
	for _, p := range participants {
		candidates := topEdges[p.ID]
		sort.Slice(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
		if len(candidates) > 3 {
			candidates = candidates[:3]
		}
		for _, c := range candidates {
			key := p.ID + ":" + c.id
			rev := c.id + ":" + p.ID
			if seen[key] || seen[rev] {
				continue
			}
			seen[key] = true
			edges = append(edges, graphEdge{
				Source:  p.ID,
				Target:  c.id,
				Score:   c.score,
				Matched: false,
			})
		}
	}

	payload := map[string]any{
		"phase":       phase,
		"phase_label": phase.Label(),
		"revealed":    phase.AtLeast(PhaseSocial),
		"nodes":       nodes,
		"edges":       edges,
		"activity":    activity,
	}

	// The social scoreboard: the top 5 by connections, from the social on.
	// During onboarding the Big Screen is the graph and the ticker only.
	if phase.AtLeast(PhaseSocial) {
		board, err := h.meetings.Leaderboard()
		if err != nil {
			log.Printf("Connection leaderboard: %v", err)
		} else {
			top := board
			if len(top) > 5 {
				top = top[:5]
			}
			payload["leaderboard"] = top
			// The champion is named at the close: the Merge Trophy winner,
			// the top of the final leaderboard. The closing ceremony screen
			// is a later ticket; this is the naming.
			if phase == PhaseClosed && len(top) > 0 {
				payload["champion"] = &top[0]
			}
		}
	}

	// Presenter mode: from show & tell on, the Big Screen shows the on-stage
	// presenter's card with the hype intro — personas only, until the host's
	// explicit identity reveal flips the card to the GitHub handle/name.
	if phase == PhaseShowTell {
		if presenter := h.presenterPayload(); presenter != nil {
			payload["presenter"] = presenter
		}
	}
	return payload
}

// presenterPayload is the Big Screen's presenter mode: the on-stage sign-up
// with its presenter's Persona, the talk and the hype intro. Personas only —
// the identity (the GitHub handle/name) is carried only once the host's
// explicit reveal action set IdentityRevealed; it is the only place a handle
// reaches the screen besides the owner's own recap.
func (h *Handler) presenterPayload() map[string]any {
	signup, err := h.showtell.OnStage()
	if err != nil {
		log.Printf("On-stage presenter: %v", err)
		return nil
	}
	if signup == nil {
		return nil
	}
	payload := map[string]any{
		"signup_id":         signup.ID,
		"title":             signup.Title,
		"description":       signup.Description,
		"repo_url":          signup.RepoURL,
		"persona_name":      signup.PersonaName,
		"persona_symbol":    signup.PersonaSymbol,
		"persona_color":     signup.PersonaColor,
		"persona_tagline":   signup.PersonaTagline,
		"hype":              signup.Hype,
		"identity_revealed": signup.IdentityRevealed,
	}
	if signup.IdentityRevealed {
		p, err := h.store.Get(signup.ParticipantID)
		if err != nil {
			log.Printf("Revealed presenter %s: %v", signup.ParticipantID, err)
			return payload
		}
		payload["handle"] = p.DisplayHandle()
		payload["name"] = p.Name
	}
	return payload
}

// GET /bigscreen/graph-data  — kept as fallback; big screen now uses /bigscreen/stream
func (h *Handler) GraphData(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.buildGraphPayload())
}

// ── /data ──────────────────────────────────────────────────────────────────

// GET /data
func (h *Handler) DataIndex(w http.ResponseWriter, r *http.Request) {
	phase := h.events.State()
	participants, _ := h.store.All()
	activity, _ := h.db.GetRecentActivity(20)
	h.render(w, "data.html", map[string]any{
		"Phase":        phase,
		"Participants": participants,
		"Activity":     activity,
		"Count":        len(participants),
		"ReadyCount":   h.db.ReadyCount(),
	})
}

// GET /data/participants
func (h *Handler) DataParticipants(w http.ResponseWriter, r *http.Request) {
	participants, err := h.store.All()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, h.scrubbedAll(participants))
}

// GET /data/participant/{id}
func (h *Handler) DataParticipant(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := h.store.Get(id)
	if err != nil {
		// Try by GitHub handle
		p, err = h.store.GetByHandle(id)
		if err != nil {
			http.Error(w, "not found", 404)
			return
		}
	}
	writeJSON(w, h.scrubbed(p))
}

// scrubbedAll scrubs the locked dossier from every Participant the JSON debug
// endpoints may show.
func (h *Handler) scrubbedAll(participants []*Participant) []*Participant {
	var out []*Participant
	for _, p := range participants {
		out = append(out, h.scrubbed(p))
	}
	return out
}

// scrubbed returns the Participant as a JSON endpoint may show them: the Match
// assessment — reason, flags, icebreakers — is scrubbed until the pair's
// dossier unlocks.
func (h *Handler) scrubbed(p *Participant) *Participant {
	if p.Assessment == nil || h.dossierUnlocked(p) {
		return p
	}
	cp := *p
	cp.Assessment = nil
	return &cp
}

// dossierUnlocked reports whether p's Match dossier may be shown — on the
// Match page and in the JSON debug endpoints: once the pair's Meeting is
// completed, or once the event is closed, after which the evening is over
// and the recap takes over.
func (h *Handler) dossierUnlocked(p *Participant) bool {
	if h.events.State() == PhaseClosed {
		return true
	}
	m, _ := h.meetings.Between(p.ID, p.MatchedWith)
	return m != nil && m.Completed()
}

// GET /data/activity
func (h *Handler) DataActivity(w http.ResponseWriter, r *http.Request) {
	msgs, err := h.db.GetRecentActivity(50)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, msgs)
}

// GET /data/state
func (h *Handler) DataState(w http.ResponseWriter, r *http.Request) {
	phase := h.events.State()
	writeJSON(w, map[string]any{
		"phase": phase,
		"count": h.db.ParticipantCount(),
		"ready": h.db.ReadyCount(),
	})
}

// GET /data/meetings
func (h *Handler) DataMeetings(w http.ResponseWriter, r *http.Request) {
	meetings, err := h.meetings.All()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, meetings)
}

// GET /data/leaderboard — debug: the connection ranking, derived from
// completed meetings. The top entry is the Champion.
func (h *Handler) DataLeaderboard(w http.ResponseWriter, r *http.Request) {
	board, err := h.meetings.Leaderboard()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, board)
}

// GET /data/channels — debug: per Participant, how many contact channels
// they opted in to. Counts only, never values — no contact data is visible
// without the completed meeting.
func (h *Handler) DataChannels(w http.ResponseWriter, r *http.Request) {
	counts, err := h.channels.Counts()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, counts)
}

// GET /data/missioncard/{id} — debug: the Participant's Mission card — tiles,
// reasons and stamp state.
func (h *Handler) DataMissionCard(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := h.store.Get(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	card, err := h.missions.Get(id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if card == nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, map[string]any{
		"participant_id": p.ID,
		"persona_name":   p.PersonaName,
		"tiles":          h.stampedTiles(p, card),
		"created_at":     card.CreatedAt,
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

// ── /admin ─────────────────────────────────────────────────────────────────

// GET /admin: the host console — the current Phase with its label and the
// participant counts, the show & tell queue, and one transition button per
// reachable forward phase.
func (h *Handler) Admin(w http.ResponseWriter, r *http.Request) {
	phase := h.events.State()
	h.render(w, "admin.html", map[string]any{
		"Phase":    phase,
		"Count":    h.db.ParticipantCount(),
		"Ready":    h.db.ReadyCount(),
		"Matched":  h.db.MatchedCount(),
		"Next":     phase.Next(),
		"ShowTell": h.showtellQueueData(),
	})
}

// POST /admin/phase: the host advances the event to a later phase (the "to"
// form value).
func (h *Handler) AdvancePhase(w http.ResponseWriter, r *http.Request) {
	h.advanceTo(w, r, Phase(r.FormValue("to")))
}

// POST /admin/reveal: the admin reveals every Match once everybody is seated —
// an alias for advancing to the Social phase.
func (h *Handler) TriggerReveal(w http.ResponseWriter, r *http.Request) {
	h.advanceTo(w, r, PhaseSocial)
}

// advanceTo advances the event to the given phase. A rejected transition — an
// unknown phase or one before the current one — is a client error; a database
// failure is not. Advancing to the current phase is a no-op: nothing is
// written or logged.
func (h *Handler) advanceTo(w http.ResponseWriter, r *http.Request, to Phase) {
	before := h.events.State()
	if err := h.events.Advance(to); err != nil {
		if errors.Is(err, ErrIllegalPhaseTransition) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "phase change failed: "+err.Error(), 500)
		return
	}
	if after := h.events.State(); after != before {
		h.db.LogActivity(after.Activity())
		// Mission cards go live at the social: generated in a background
		// worker pool, so the transition returns immediately.
		if after.MissionCardsLive() {
			h.missions.GenerateAll()
		}
		// The champion is named at the close; the closing ceremony screen
		// itself is a later ticket.
		if after == PhaseClosed {
			h.announceChampion()
		}
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// announceChampion names the Merge Trophy winner on the ticker when the event
// closes: the Participant with the most completed meetings, ties broken by
// persona name. With no Participants there is no champion to name.
func (h *Handler) announceChampion() {
	champion, err := h.meetings.Champion()
	if err != nil {
		log.Printf("Champion at the close: %v", err)
		return
	}
	if champion == nil {
		return
	}
	noun := "connections"
	if champion.Connections == 1 {
		noun = "connection"
	}
	h.db.LogActivity(fmt.Sprintf("👑 %s %s takes the Merge Trophy with %d %s!", champion.Symbol, champion.Name, champion.Connections, noun))
}

// POST /admin/reset
func (h *Handler) Reset(w http.ResponseWriter, r *http.Request) {
	if err := h.events.Reset(); err != nil {
		http.Error(w, "reset failed: "+err.Error(), 500)
		return
	}
	// Clear LLM cache when resetting event
	h.matcher.ClearCache()
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// POST /admin/rematch — rejected from the social onward: matching is frozen,
// pairs are final.
func (h *Handler) Rematch(w http.ResponseWriter, r *http.Request) {
	if phase := h.events.State(); phase.MatchingFrozen() {
		http.Error(w, "Matching is frozen from the social onward — pairs are final.", http.StatusBadRequest)
		return
	}
	h.db.LogActivity("🔄 Admin triggered full rematch")
	go func() {
		if err := h.matchmaking.Rematch(); err != nil {
			log.Printf("Rematch error: %v", err)
			h.db.LogActivity("⚠️ Rematch failed: " + err.Error())
		}
	}()
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// DELETE /data/participant/{id}
func (h *Handler) DeleteParticipant(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.relations.Remove(id); errors.Is(err, ErrParticipantNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if err := h.meetings.RemoveParticipant(id); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if err := h.missions.RemoveParticipant(id); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if err := h.channels.RemoveParticipant(id); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if err := h.recaps.RemoveParticipant(id); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if err := h.showtell.RemoveParticipant(id); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// ── SSE ────────────────────────────────────────────────────────────────────

func sseHeaders(w http.ResponseWriter) bool {
	if _, ok := w.(http.Flusher); !ok {
		http.Error(w, "streaming not supported", 500)
		return false
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	return true
}

func sseRedirect(w http.ResponseWriter, url string) {
	fmt.Fprintf(w, "event: redirect\ndata: %s\n\n", url)
	w.(http.Flusher).Flush()
}

// sseEvent sends a named event with no data.
func sseEvent(w http.ResponseWriter, name string) {
	fmt.Fprintf(w, "event: %s\ndata: \n\n", name)
	w.(http.Flusher).Flush()
}

// heartbeatEvery keeps idle SSE connections open through proxies (tests shorten it).
var heartbeatEvery = 25 * time.Second

// watch calls check now and again after every change, until check reports it is
// done or the client goes away. Nothing is polled: changes come from the change feed.
func (h *Handler) watch(w http.ResponseWriter, r *http.Request, check func() (done bool)) {
	changes, stop := h.db.Subscribe()
	defer stop()
	heartbeat := time.NewTicker(heartbeatEvery)
	defer heartbeat.Stop()
	for {
		if check() {
			return
		}
		if h.streams != nil {
			h.streams.OnStream(r.URL.Path)
		}
		select {
		case <-r.Context().Done():
			return
		case <-changes:
		case <-heartbeat.C:
			fmt.Fprint(w, ": ping\n\n")
			w.(http.Flusher).Flush()
		}
	}
}

// GET /user/pipeline-stream/{id}
// Tells the onboarding page to refresh when the Participant's step changes
// (the first question appears) and redirects once they belong elsewhere.
func (h *Handler) PipelineStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	first, err := h.store.Get(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !sseHeaders(w) {
		return
	}
	lastStep := first.PipelineStep
	h.watch(w, r, func() bool {
		p, err := h.store.Get(id)
		if err != nil {
			return true
		}
		if dest := h.destination(p); dest != "/user/onboard/"+id {
			sseRedirect(w, dest)
			return true
		}
		if p.PipelineStep != lastStep {
			lastStep = p.PipelineStep
			sseEvent(w, "refresh")
		}
		return false
	})
}

// GET /user/wait-stream/{id}
// Tells the wait page to refresh when what it shows changes (the ready count,
// being matched, the meet section) and redirects once the Participant belongs
// elsewhere.
func (h *Handler) WaitStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	first, err := h.store.Get(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !sseHeaders(w) {
		return
	}
	type shown struct {
		ready   int
		matched bool
		meet    meetSignature
	}
	last := shown{h.db.ReadyCount(), first.IsMatched(), h.meetSignature(first)}
	h.watch(w, r, func() bool {
		p, err := h.store.Get(id)
		if err != nil {
			return true
		}
		if dest := h.destination(p); dest != "/user/wait/"+id {
			sseRedirect(w, dest)
			return true
		}
		if now := (shown{h.db.ReadyCount(), p.IsMatched(), h.meetSignature(p)}); now != last {
			last = now
			sseEvent(w, "refresh")
		}
		return false
	})
}

// GET /bigscreen/stream
// Pushes the graph now and after every change.
func (h *Handler) ScreenStream(w http.ResponseWriter, r *http.Request) {
	if !sseHeaders(w) {
		return
	}
	h.watch(w, r, func() bool {
		data, _ := json.Marshal(h.buildGraphPayload())
		fmt.Fprintf(w, "data: %s\n\n", data)
		w.(http.Flusher).Flush()
		return false
	})
}
