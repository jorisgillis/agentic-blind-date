package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"path/filepath"
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

// Handler handles HTTP requests for the web application.
type Handler struct {
	db          *DB
	store       *ParticipantStore
	events      *EventStore
	evening     *Evening
	meetings    *Meetings
	channels    *ContactChannels
	recaps      *Recaps
	missions    *Missions
	showtell    *ShowTell
	slides      *SlideDecks
	board       *Board
	closing     *Closing
	screen      *Screen
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
func NewHandler(db *DB, onboarding *Onboarding, interview *Interview, matcher *Matcher, relations *Relationships, matchmaking *Matchmaking, missions *Missions, showtell *ShowTell, slides *SlideDecks, streams StreamObserver) *Handler {
	funcs := template.FuncMap{
		"add":    func(a, b int) int { return a + b },
		"badges": func(p GitHubProfile) []Badge { return computeBadges(p) },
		// Only called with a non-empty question set and constant divisors.
		"percent":   func(n, total int) int { return n * 100 / total },
		"divInt":    func(a, b int) int { return a / b },
		"colorName": paletteName,
		"textColor": paletteText,
		"themeCSS":  themeCSS,
		"fileSize":  formatFileSize,
		"linkify":   linkify,
	}
	tmpl := template.Must(template.New("").Funcs(funcs).ParseGlob(filepath.Join("templates", "*.html")))
	events := NewEventStore(db)
	meetings := NewMeetings(db, events)
	channels := NewContactChannels(db)
	closing := NewClosing(db, meetings, channels, showtell)
	return &Handler{
		db: db, store: NewParticipantStore(db), events: events, meetings: meetings,
		evening:  NewEvening(db, missions, meetings, slides, matcher),
		channels: channels, recaps: NewRecaps(db), missions: missions, showtell: showtell, slides: slides, board: NewBoard(db, events),
		closing:    closing,
		screen:     NewScreen(db, matcher, meetings, showtell, closing),
		onboarding: onboarding, interview: interview, matcher: matcher, relations: relations, matchmaking: matchmaking,
		tmpl: tmpl, streams: streams,
	}
}

func (h *Handler) render(w http.ResponseWriter, name string, data any) {
	h.renderStatus(w, http.StatusOK, name, data)
}

// renderStatus renders a page with the given status: a rejected form re-renders
// its page with the reason.
func (h *Handler) renderStatus(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Pages are revalidated on every load: the app can be updated while an
	// event is running, and a cached page would keep running its old scripts.
	w.Header().Set("Cache-Control", "no-cache")
	// Render into a buffer first: a template error must not leak a partial
	// page with status 200 to the participant.
	var buf bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		logger("handler").Error("rendering the template", "template", name, "err", err)
		http.Error(w, "render error", 500)
		return
	}
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
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
	h.render(w, "landing.html", map[string]any{"PublicTheme": h.events.PublicTheme()})
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
		logger("handler").Error("registration failed", "handle", handle, "name", name, "err", err)
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
	h.render(w, "onboard.html", onboardPage{Participant: p, Question: h.openQuestion(p), Pipeline: h.pipelineStepDataFor(p)})
}

// onboardPage is the Onboard page's data: the Participant plus the Interview
// question to show right away when the Interview has already started, so a
// page loaded mid-Interview (a reload, a restart) shows the question instead
// of the preparing placeholder.
type onboardPage struct {
	*Participant
	Question *QuestionData
	Pipeline pipelineStepData
}

// pipelineStepData is the pipeline-step fragment's data: the Participant and
// their position in the agent queue — how many Participants registered before
// them are still waiting on an agent — so the waiting carries feedback instead
// of a silent spinner.
type pipelineStepData struct {
	*Participant
	QueueAhead int
}

func (h *Handler) pipelineStepDataFor(p *Participant) pipelineStepData {
	return pipelineStepData{Participant: p, QueueAhead: h.store.AgentQueueAhead(p.ID)}
}

// openQuestion is the Interview question the pipeline area shows right away:
// the first unanswered one while the Interview is running, nil otherwise.
func (h *Handler) openQuestion(p *Participant) *QuestionData {
	if qd := h.interview.Next(p); qd != nil && p.PipelineStep == StepInterviewing {
		return qd
	}
	return nil
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
	if qd := h.openQuestion(p); qd != nil {
		h.render(w, "fragment-question.html", qd)
		return
	}
	h.render(w, "fragment-pipeline-step.html", h.pipelineStepDataFor(p))
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

	data := h.waitPageData(p)
	data["Profile"] = profile
	data["QAPairs"] = qaPairs
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

// GET /user/persona/{id} — HTMX, the wait page's persona card, refreshed when
// the wait stream says so: the persona is crafted in the background after
// the last interview answer, so the page may have rendered before it exists.
func (h *Handler) PersonaCard(w http.ResponseWriter, r *http.Request) {
	p, err := h.store.Get(r.PathValue("id"))
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	if dest := h.destination(p); dest != "/user/wait/"+p.ID {
		w.Header().Set("HX-Redirect", dest)
		return
	}
	h.render(w, "fragment-persona-card.html", p)
}

// ── phase header ───────────────────────────────────────────────────────────

// GET /user/phase-header/{id} — HTMX, the personal pages' persistent phase
// header: the evening's progress strip, the one-line "Tonight: …" summary and
// the Participant's own status, refreshed when the page's stream says so — on
// 'refresh' like every other fragment, and on the 'phase' event when the host
// advances the evening.
func (h *Handler) PhaseHeader(w http.ResponseWriter, r *http.Request) {
	p, err := h.store.Get(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h.render(w, "fragment-phase-header.html", h.phaseHeaderData(p, h.missionView(p)))
}

// phaseHeaderStep is one step of the phase header's progress strip: the phase
// itself, its short label, and whether it is done, current or upcoming.
type phaseHeaderStep struct {
	Phase   Phase
	Label   string
	Done    bool
	Current bool
}

// phaseHeaderBadge is the Participant's own show & tell queue position in the
// phase header: "You're up in N" while their talk waits in the host's queue,
// a distinct live state once the host has staged them. The queue itself stays
// on the Big Screen; only the number rides the header (ADR 0019, decision 1).
type phaseHeaderBadge struct {
	Text    string
	OnStage bool
}

// phaseHeaderData is the personal pages' phase header. It renders from the
// phase machine's current state — a late joiner simply sees the earlier steps
// as done — and weaves the Participant's own status into the current phase.
// The mission view is the caller's single pass over the Participant's mission
// data, shared with the mission card section on full-page renders.
func (h *Handler) phaseHeaderData(p *Participant, mission *MissionView) map[string]any {
	phase := h.events.State()
	steps := make([]phaseHeaderStep, 0, len(phases))
	for _, q := range phases {
		steps = append(steps, phaseHeaderStep{
			Phase:   q,
			Label:   q.StepLabel(),
			Done:    q.rank() < phase.rank(),
			Current: q == phase,
		})
	}
	return map[string]any{
		"Me":         p,
		"Phase":      phase,
		"Steps":      steps,
		"Summary":    phase.Tonight(),
		"Status":     h.phaseHeaderStatus(p, phase, mission),
		"QueueBadge": h.phaseHeaderQueueBadge(p, phase),
	}
}

// phaseHeaderQueueBadge is the Participant's show & tell queue badge for the
// phase header, computed only during the show & tell phase: their 1-based
// place in the host's queue, or the distinct on-stage state once the host has
// staged them. A Participant without a sign-up — or whose talk is already
// presented, out of the queue — sees no badge. The badge rides the header
// fragment on the personal pages' existing 'refresh' events: the show & tell
// signature both streams watch carries the sign-up's position and status.
func (h *Handler) phaseHeaderQueueBadge(p *Participant, phase Phase) *phaseHeaderBadge {
	if phase != PhaseShowTell {
		return nil
	}
	signup, err := h.showtell.ForParticipant(p.ID)
	if err != nil {
		logger("handler").Warn("phase header sign-up", "participant", p.ID, "err", err)
		return nil
	}
	if signup == nil {
		return nil
	}
	switch signup.Status {
	case SignupOnStage:
		return &phaseHeaderBadge{Text: "On stage now", OnStage: true}
	case SignupQueued:
		return &phaseHeaderBadge{Text: fmt.Sprintf("You're up in %d", h.showtell.QueuePosition(p.ID))}
	}
	return nil
}

// phaseHeaderStatus is the Participant's own status line in the phase header,
// per phase: the Interview's progress during onboarding, the Mission card's
// stamped tiles during the social, a show & tell pointer during show & tell,
// and the recap pointer once the event is closed.
func (h *Handler) phaseHeaderStatus(p *Participant, phase Phase, mission *MissionView) string {
	switch phase {
	case PhaseSocial:
		if mission == nil {
			return "Mission: your card is being prepared"
		}
		stamped := 0
		for _, tile := range stampedTiles(mission) {
			if tile.Stamped {
				stamped++
			}
		}
		return fmt.Sprintf("Mission: %d of %d tiles stamped", stamped, len(mission.Card.Tiles))
	case PhaseShowTell:
		return "Show & tell: audience questions open"
	case PhaseClosed:
		return "Your recap is kept — scan the recap QR at the bottom of the page"
	}
	answered := 0
	for _, q := range p.Questions {
		if _, ok := p.Answers[q.ID]; ok {
			answered++
		}
	}
	return fmt.Sprintf("Interview: %d of %d answered", answered, len(p.Questions))
}

// waitStatusData is the wait page's status section. A matched Participant
// sees the teaser — their future Match's emoji and colour and a hint at what
// the pair disagreed on, never the partner's identity, score or dossier. An
// unmatched Participant sees the ready count while the event is still
// onboarding, and a clear no-match state once the Social has begun. While the
// Participant themselves waits on an agent (persona, matching), the queue
// position rides along, so the wait carries feedback.
func (h *Handler) waitStatusData(p *Participant) map[string]any {
	data := map[string]any{
		"Participant": p,
		"Count":       h.db.ReadyCount(),
		"Revealed":    h.events.State().AtLeast(PhaseSocial),
		"QueueAhead":  h.store.AgentQueueAhead(p.ID),
	}
	partner, _, err := h.relations.PartnerOf(p.ID)
	if err != nil {
		logger("handler").Warn("wait status", "participant", p.ID, "err", err)
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

	// The page's data map derives from the card registry: the body's cards
	// and the evergreen cards riding at the page's top level for the "More"
	// drawer (ADR 0019, step 3) — outside the phase-content region, so they
	// survive phase swaps and keep their own refresh wiring.
	h.render(w, "match.html", h.matchPageData(p))
}

// ── the match page's phase-content region ──────────────────────────────────

// GET /user/phase-content/{id} — HTMX, the match page's phase-content region:
// the composition of per-card fragments the current phase calls for. The
// region is swapped on the match stream's 'phase' event — a phase change is
// the one moment a bigger visual beat is wanted — while the per-card fragments
// inside it keep their own 'refresh' wiring, so a refresh never re-animates
// the region.
func (h *Handler) PhaseContent(w http.ResponseWriter, r *http.Request) {
	p, err := h.store.Get(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if dest := h.destination(p); dest != "/user/match/"+p.ID {
		w.Header().Set("HX-Redirect", dest)
		return
	}
	h.render(w, "fragment-phase-content.html", h.phaseContentData(p, h.missionView(p)))
}

// phaseContentData is the match page's phase-content region: the per-card
// data the current phase calls for, derived from the card registry — every
// card placed in the region contributes its data, and the match reveal, the
// one card the region renders from its own data, merges into the region's
// map. The composition itself — which cards the current phase calls for, in
// order — lives in the region's template. The evergreen cards — the dossier,
// the settings, the share-profile QR and the recap link — live in the page's
// "More" drawer instead (ADR 0019, step 3, #138), outside this region. The
// mission view is the caller's single pass over the Participant's mission
// data, shared with the phase header on full-page renders.
func (h *Handler) phaseContentData(p *Participant, mission *MissionView) map[string]any {
	data := map[string]any{"Me": p, "Phase": h.events.State()}
	for _, c := range pageCards {
		if c.OnMatch != regionPhase {
			continue
		}
		landCardData(data, c, c.Data(h, p, mission))
	}
	return data
}

// revealData is the match reveal's data: the region's hero — the "It's a
// match!" card with the score and the partner's persona card, or the
// no-match state. The reveal has no key of its own: the region renders it
// from its own map, so its data merges in at the top level.
func (h *Handler) revealData(p *Participant) map[string]any {
	dossier, match := h.dossierData(p)
	return map[string]any{"Me": p, "Match": match, "Score": dossier["Score"]}
}

// dossierData is the Match page's dossier: the fun payload of the Match — the
// reason, the flags and the icebreakers — locked until the pair's dossier
// unlocks (their Meeting completed, or the event closed). The score is not
// part of the dossier: it is revealed with the Match itself. The second
// return value is the partner, nil without a Match.
func (h *Handler) dossierData(p *Participant) (map[string]any, *Participant) {
	match, result, err := h.relations.PartnerOf(p.ID)
	if err != nil {
		logger("handler").Warn("dossier", "participant", p.ID, "err", err)
	}
	if result == nil {
		result = &matchResult{RedFlags: []string{}, GreenFlags: []string{}, Icebreakers: []string{}}
	}
	unlocked := false
	if match != nil {
		unlocked = h.meetings.DossierUnlocked(p.ID, p.MatchedWith)
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

// missionCardData is the personal page's mission card section from a fresh
// single pass over the Participant's mission data.
func (h *Handler) missionCardData(p *Participant) map[string]any {
	return h.missionCardSection(p, h.missionView(p))
}

// missionView is the personal page's single pass over the Participant's
// mission data: the card, its stamps and the connection count together. A
// read failure degrades to nil, so the sections show the placeholder —
// unreadable missions never break the page.
func (h *Handler) missionView(p *Participant) *MissionView {
	view, err := h.missions.View(p.ID)
	if err != nil {
		logger("handler").Warn("mission view", "participant", p.ID, "err", err)
		return nil
	}
	return view
}

// missionCardSection is the personal page's mission card section from an
// already-computed mission view: the card's tiles with their stamp state and
// the live progress — connections and rank vs the top 5. Without a card it is
// the preparing placeholder while the event allows cards, and empty
// otherwise.
func (h *Handler) missionCardSection(p *Participant, mission *MissionView) map[string]any {
	data := map[string]any{
		"Me":        p,
		"Preparing": h.events.State().MissionCardsLive(),
	}
	if mission == nil {
		return data
	}
	tiles := stampedTiles(mission)
	data["Tiles"] = tiles
	data["Grid"] = tileGrid(tiles)
	data["Connections"] = mission.Connections
	board, err := h.meetings.Leaderboard()
	if err != nil {
		logger("handler").Warn("connection leaderboard", "participant", p.ID, "err", err)
	}
	data["Rank"] = rankOf(board, p.ID)
	top := board
	if len(top) > 5 {
		top = top[:5]
	}
	data["Top"] = top
	return data
}

// stampedTiles joins the mission view's tiles with its stamps: a tile is
// stamped when the Participant has a completed meeting with that person.
// Stamps derive from the meetings — there is no stamped_at column.
func stampedTiles(mission *MissionView) []missionTileView {
	tiles := make([]missionTileView, 0, len(mission.Card.Tiles))
	for _, t := range mission.Card.Tiles {
		tiles = append(tiles, missionTileView{MissionTile: t, Stamped: mission.Stamped[t.PersonID]})
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

// championOf names the Merge Trophy winner from the leaderboard the caller
// already fetched — the board's top, reusing its order exactly (the same
// deterministic tie-break the board was ordered by), never re-sorting and
// never re-querying. Nil when the board is empty.
func championOf(board []ConnectionStanding) *ConnectionStanding {
	if len(board) == 0 {
		return nil
	}
	return &board[0]
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

	// The viewer's theme surrounds the target's business card: the viewer
	// logged-in gets their choice, an anonymous viewer gets the default.
	// The handler resolves the name because Me is a possibly-nil
	// *Participant and field access on nil errors at template-execution
	// time — the template gets a plain string.
	themeName := ""
	if me != nil {
		themeName = me.Theme // themeByName absorbs ""
	}

	h.render(w, "meet.html", map[string]any{
		"Target":    target,
		"Me":        me,
		"Allowed":   allowed,
		"Already":   already,
		"Completed": completed,
		"ThemeName": themeName,
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
			ex, err := h.channels.Exchanged(&m, p.ID)
			if err != nil {
				logger("handler").Warn("channels exchanged", "participant", p.ID, "with", other.ID, "err", err)
			}
			meetings = append(meetings, meetingView{
				Other:    other,
				Status:   m.StatusFor(p.ID),
				Revealed: ex.Revealed,
				Shared:   ex.Shared,
			})
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
// Tells the match page to refresh its cards when what they show changes —
// the stream's diff key is the card registry's per-card signature sources
// composed together (#155), so every pushed card on the page contributes
// what its own push depends on: the phase, the meetings (the meet section,
// the dossier's unlock), the Mission card appearing, the show & tell
// sign-up state, the Slide Decks, the Board, the recap link. A phase
// transition additionally emits a distinct 'phase' event, so the phase
// header updates live and the region swaps. Like the pipeline stream, it
// syncs once on connect, so a page that rendered before the stream opened
// catches up.
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
	last := h.pageSignature(first, onMatchPage)
	lastPhase := h.events.State()
	synced := false
	h.watch(w, r, func() bool {
		p, err := h.store.Get(id)
		if err != nil {
			return true
		}
		if now := h.events.State(); now != lastPhase {
			lastPhase = now
			sseEvent(w, "phase")
		}
		// One sync refresh on connect, like the pipeline stream: a page that
		// rendered before the stream opened may have missed the only change
		// it will ever see (a meeting completing between render and connect),
		// and the opening signature already carries it — push would never
		// fire on its own.
		if !synced {
			synced = true
			sseEvent(w, "refresh")
			return false
		}
		if now := h.pageSignature(p, onMatchPage); now != last {
			last = now
			sseEvent(w, "refresh")
		}
		return false
	})
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

// POST /user/theme/{id} — saves the Participant's theme choice.
func (h *Handler) SaveTheme(w http.ResponseWriter, r *http.Request) {
	p, err := h.store.Get(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimSpace(r.FormValue("theme"))
	if err := h.store.Change(ParticipantChange{ID: p.ID, Theme: &name}); err != nil {
		http.Error(w, "saving your theme failed: "+err.Error(), 500)
		return
	}
	w.Header().Set("HX-Redirect", h.destination(p))
}

// settingsData is the personal page's settings section: the Participant's
// contact channels — entered once, off by default — with the github channel
// pre-filled from their GitHub profile, and the theme registry for the
// section's theme picker.
func (h *Handler) settingsData(p *Participant) map[string]any {
	channels, err := h.channels.Channels(p.ID)
	if err != nil {
		logger("handler").Warn("contact channels", "participant", p.ID, "err", err)
	}
	for i := range channels {
		if channels[i].Channel == "github" && channels[i].Value == "" {
			channels[i].Value = githubHandle(p)
		}
	}
	return map[string]any{"Me": p, "Channels": channels, "Themes": themes}
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
			logger("handler").Warn("recap token", "participant", p.ID, "err", err)
		} else {
			data["Token"] = token
		}
	}
	return data
}

// GET /user/recap/{token} — the personal recap page: the Participant's
// evening, kept — their persona card, their match dossier (always unlocked
// here), every connection with the channels they shared, the channels they
// shared themselves, their show & tell items (their sign-up and their
// questions), and their stats. The token is the unguessable UUID; an
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
// the channels they shared themselves, their show & tell items — their sign-up
// and their audience questions — and their personal stats.
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
		logger("handler").Warn("recap meetings", "participant", p.ID, "err", err)
	}
	for _, m := range ms {
		if !m.Completed() {
			continue // a connection is a completed meeting
		}
		other, err := h.store.Get(m.Other(p.ID))
		if err != nil {
			continue // the other side left the event
		}
		ex, err := h.channels.Exchanged(&m, p.ID)
		if err != nil {
			logger("handler").Warn("recap channels revealed", "participant", p.ID, "by", other.ID, "err", err)
		}
		connections = append(connections, connectionView{Other: other, Revealed: ex.Revealed})
	}

	mine, err := h.channels.Channels(p.ID)
	if err != nil {
		logger("handler").Warn("recap contact channels", "participant", p.ID, "err", err)
	}

	// The Participant's show & tell items: their sign-up and the questions
	// they asked, with how each ended up.
	signup, err := h.showtell.ForParticipant(p.ID)
	if err != nil {
		logger("handler").Warn("recap show & tell sign-up", "participant", p.ID, "err", err)
	}
	questions, err := h.showtell.QuestionsForParticipant(p.ID)
	if err != nil {
		logger("handler").Warn("recap show & tell questions", "participant", p.ID, "err", err)
	}

	board, err := h.meetings.Leaderboard()
	if err != nil {
		logger("handler").Warn("recap leaderboard", "participant", p.ID, "err", err)
	}
	champion := championOf(board)

	return map[string]any{
		"Me":                p,
		"Match":             match,
		"Dossier":           dossier,
		"Connections":       connections,
		"Shared":            shared(mine),
		"ShowTellSignup":    signup,
		"ShowTellQuestions": questions,
		"ConnectionCount":   h.meetings.Connections(p.ID),
		"Rank":              rankOf(board, p.ID),
		"Champion":          champion != nil && champion.ID == p.ID,
	}
}

// ── the closing ceremony: the public event recap + the host export pack ────

// GET /recap — the public event recap: the evening's stats, the top
// connectors, the persona gallery and the show & tell list with links.
// Available only once the event is closed — before that there is no recap.
// Personas only: no contact data, no identities.
func (h *Handler) EventRecap(w http.ResponseWriter, r *http.Request) {
	if !h.events.State().IsClosed() {
		http.NotFound(w, r)
		return
	}
	// The Public theme rides alongside the recap's own data: the handler
	// resolves the name so the template's head stays one line.
	recap := h.closing.Recap()
	h.render(w, "event-recap.html", struct {
		EventRecap
		PublicTheme string
	}{recap, h.events.PublicTheme()})
}

// GET /admin/export — the host's export pack, downloaded as JSON (the
// default) or Markdown (?format=md) and pasted into the post-meetup blast.
// Personas and counts only — the pack never carries contact-channel values
// or GitHub identity mappings.
func (h *Handler) AdminExport(w http.ResponseWriter, r *http.Request) {
	pack := h.closing.Pack()
	if r.URL.Query().Get("format") == "md" {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="agentic-blind-date-export.md"`)
		fmt.Fprint(w, renderExportMarkdown(pack))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="agentic-blind-date-export.json"`)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(pack)
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
// its place in the host's queue, if any, and the question form — live during
// show & tell — bound to whoever is on stage, with the Participant's questions.
func (h *Handler) showtellData(p *Participant) map[string]any {
	data := map[string]any{
		"Me":      p,
		"Open":    h.events.State().AllowsShowTell(),
		"AskOpen": h.events.State().IsShowTell(),
	}
	signup, err := h.showtell.ForParticipant(p.ID)
	if err != nil {
		logger("handler").Warn("show & tell sign-up", "participant", p.ID, "err", err)
	}
	if signup != nil {
		data["Signup"] = signup
		data["Position"] = h.showtell.QueuePosition(p.ID)
	}
	// The question form binds to whoever is on stage; its button is disabled
	// while the stage is empty.
	presenter, err := h.showtell.OnStage()
	if err != nil {
		logger("handler").Warn("on-stage presenter", "err", err)
	}
	if presenter != nil {
		data["Presenter"] = presenter
	}
	questions, err := h.showtell.QuestionsForParticipant(p.ID)
	if err != nil {
		logger("handler").Warn("show & tell questions", "participant", p.ID, "err", err)
	}
	if len(questions) > 0 {
		data["MyQuestions"] = questions
	}
	return data
}

// POST /user/showtell/question/{id} — ask the presenter on stage a question,
// through the asker's persona ("{symbol} asks: …"). Questioning needs no
// meeting and earns nothing; it is live during show & tell and only while
// someone is on stage.
func (h *Handler) ShowTellQuestion(w http.ResponseWriter, r *http.Request) {
	p, err := h.store.Get(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	err = h.showtell.Ask(p, r.FormValue("question"))
	switch {
	case errors.Is(err, ErrShowTellClosed):
		http.Error(w, "Questions are only open during show & tell.", 400)
	case errors.Is(err, ErrNoPresenter):
		http.Error(w, "Nobody is on stage — questions open when a presenter is up.", 400)
	case errors.Is(err, ErrInvalidQuestion):
		http.Error(w, err.Error(), 400)
	case err != nil:
		http.Error(w, "asking your question failed: "+err.Error(), 500)
	default:
		data := h.showtellData(p)
		data["Asked"] = true
		h.render(w, "fragment-showtell.html", data)
	}
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
	default:
		// Only the success path redirects — an http.Error above already
		// wrote the response, and a second WriteHeader would be superfluous.
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
	}
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

// POST /admin/showtell/question — the host's question-queue actions on a
// question (the "id" form value): "asked" puts it on the Big Screen — one
// current question at a time — "answered" marks it answered, "dismissed"
// drops it.
func (h *Handler) ShowTellQuestionAction(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "That question is not in the queue.", 400)
		return
	}
	var to QuestionStatus
	switch r.FormValue("action") {
	case "asked":
		to = QuestionAsked
	case "answered":
		to = QuestionAnswered
	case "dismissed":
		to = QuestionDismissed
	default:
		http.Error(w, "Unknown action — asked, answered or dismissed.", 400)
		return
	}
	_, err = h.showtell.TransitionQuestion(id, to)
	switch {
	case errors.Is(err, ErrShowTellClosed):
		http.Error(w, "The question queue is only live during show & tell.", 400)
	case errors.Is(err, ErrQuestionNotFound):
		http.Error(w, "That question is not in the queue.", 404)
	case errors.Is(err, ErrAlreadyAsked), errors.Is(err, ErrInvalidQuestionTransition):
		http.Error(w, err.Error(), 400)
	case err != nil:
		http.Error(w, "updating the question queue failed: "+err.Error(), 500)
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
		logger("handler").Warn("show & tell queue", "err", err)
	}
	return map[string]any{
		"Open":  h.events.State().AllowsShowTell(),
		"Queue": queue,
	}
}

// GET /admin/showtell-stream — tells the host console to refresh the show &
// tell queue and the audience-question queue when either changes.
func (h *Handler) ShowTellStream(w http.ResponseWriter, r *http.Request) {
	if !sseHeaders(w) {
		return
	}
	lastQueue := h.showtellQueueSignature()
	lastQuestions := h.showtellQuestionsSignature()
	h.watch(w, r, func() bool {
		if now := h.showtellQueueSignature(); now != lastQueue {
			lastQueue = now
			sseEvent(w, "refresh")
		}
		if now := h.showtellQuestionsSignature(); now != lastQuestions {
			lastQuestions = now
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

// GET /admin/showtell-questions — HTMX, the host console's audience-question
// queue, refreshed when the queue stream says so.
func (h *Handler) ShowTellQuestions(w http.ResponseWriter, r *http.Request) {
	h.render(w, "fragment-showtell-questions.html", h.showtellQuestionsData())
}

// showtellQuestionsData is the host console's audience-question queue: every
// question with its asker's Persona and status — personas only, never
// identities.
func (h *Handler) showtellQuestionsData() map[string]any {
	questions, err := h.showtell.Questions()
	if err != nil {
		logger("handler").Warn("show & tell questions", "err", err)
	}
	return map[string]any{
		"Open":      h.events.State().IsShowTell(),
		"Questions": questions,
	}
}

// showtellQuestionsSignature is what the queue stream also watches: every
// question's id and status.
func (h *Handler) showtellQuestionsSignature() string {
	questions, err := h.showtell.Questions()
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, q := range questions {
		fmt.Fprintf(&b, "%d:%s;", q.ID, q.Status)
	}
	return b.String()
}

// ── /bigscreen ─────────────────────────────────────────────────────────────

// GET /bigscreen
func (h *Handler) Screen(w http.ResponseWriter, r *http.Request) {
	h.render(w, "screen.html", map[string]any{"Palette": paletteHex(), "PublicTheme": h.events.PublicTheme()})
}

// GET /bigscreen/graph-data  — kept as fallback; big screen now uses /bigscreen/stream
func (h *Handler) GraphData(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.screen.Payload())
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
	if p.Assessment == nil || h.meetings.DossierUnlocked(p.ID, p.MatchedWith) {
		return p
	}
	cp := *p
	cp.Assessment = nil
	return &cp
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
	view, err := h.missions.View(id)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if view == nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, map[string]any{
		"participant_id": p.ID,
		"persona_name":   p.PersonaName,
		"tiles":          stampedTiles(view),
		"created_at":     view.Card.CreatedAt,
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
		"Phase":             phase,
		"Count":             h.db.ParticipantCount(),
		"Ready":             h.db.ReadyCount(),
		"Matched":           h.db.MatchedCount(),
		"Waiting":           h.store.AgentQueueWaiting(),
		"Next":              phase.Next(),
		"Notice":            r.URL.Query().Get("notice"),
		"ShowTell":          h.showtellQueueData(),
		"ShowTellQuestions": h.showtellQuestionsData(),
		"Board":             h.boardModerationData(),
		"Themes":            themes,
		"PublicTheme":       h.events.PublicTheme(),
	})
}

// POST /admin/public-theme: the Host picks the Public theme — the theme of
// the public pages (the Big Screen, the landing page, the event recap). The
// value is stored as-is (the lenient rule — themeByName absorbs unknown
// names at render), and the console reloads via the 303.
func (h *Handler) AdminPublicTheme(w http.ResponseWriter, r *http.Request) {
	if err := h.events.SetPublicTheme(strings.TrimSpace(r.FormValue("theme"))); err != nil {
		http.Error(w, "saving the public theme failed: "+err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
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

// advanceTo advances the event to the given phase: the Evening module owns
// the transition and its consequences. A rejected transition — an unknown
// phase or one before the current one — is a client error; a database
// failure is not.
func (h *Handler) advanceTo(w http.ResponseWriter, r *http.Request, to Phase) {
	if err := h.evening.Advance(to); err != nil {
		if errors.Is(err, ErrIllegalPhaseTransition) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "phase change failed: "+err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// POST /admin/reset: the Evening module owns the reset and its consequences.
func (h *Handler) Reset(w http.ResponseWriter, r *http.Request) {
	if err := h.evening.Reset(); err != nil {
		http.Error(w, "reset failed: "+err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// POST /admin/rematch — rejected from the social onward: matching is frozen,
// pairs are final. Either way the host lands back on the console with a
// notice: a bare 400 left the host guessing whether the click registered.
func (h *Handler) Rematch(w http.ResponseWriter, r *http.Request) {
	if phase := h.events.State(); phase.MatchingFrozen() {
		http.Redirect(w, r, "/admin?notice="+url.QueryEscape("Matching is frozen from the social onward — pairs are final."), http.StatusSeeOther)
		return
	}
	h.db.LogActivity("🔄 Admin triggered full rematch")
	go func() {
		if err := h.matchmaking.Rematch(); err != nil {
			logger("handler").Error("rematch", "err", err)
			h.db.LogActivity("⚠️ Rematch failed: " + err.Error())
		}
	}()
	http.Redirect(w, r, "/admin?notice="+url.QueryEscape("🔄 Rematch started — the matchmaker agents are at work."), http.StatusSeeOther)
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
	if err := h.board.RemoveParticipant(id); err != nil {
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
// Tells the onboarding page to refresh when the stream opens — the page may
// have rendered before the Participant's step settled — and when the step
// changes (the first question appears), and redirects once they belong
// elsewhere.
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
	synced := false
	h.watch(w, r, func() bool {
		p, err := h.store.Get(id)
		if err != nil {
			return true
		}
		if dest := h.destination(p); dest != "/user/onboard/"+id {
			sseRedirect(w, dest)
			return true
		}
		if !synced || p.PipelineStep != lastStep {
			synced = true
			lastStep = p.PipelineStep
			sseEvent(w, "refresh")
		}
		return false
	})
}

// GET /user/wait-stream/{id}
// Tells the wait page to refresh its cards when what they show changes —
// the stream's diff key is the card registry's per-card signature sources
// composed together (#155), so every pushed card on the page contributes
// what its own push depends on: the ready count and the Match (the status
// card), the persona being crafted, the phase, the meetings, the Mission
// card, the show & tell sign-up state, the Slide Decks, the Board, the
// recap link — and redirects once the Participant belongs elsewhere. A
// phase transition additionally emits a distinct 'phase' event, so the
// phase header updates live. Like the pipeline stream, it syncs once on
// connect, so a page that rendered before the stream opened catches up.
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
	last := h.pageSignature(first, onWaitPage)
	lastPhase := h.events.State()
	synced := false
	h.watch(w, r, func() bool {
		p, err := h.store.Get(id)
		if err != nil {
			return true
		}
		if now := h.events.State(); now != lastPhase {
			lastPhase = now
			sseEvent(w, "phase")
		}
		if dest := h.destination(p); dest != "/user/wait/"+id {
			sseRedirect(w, dest)
			return true
		}
		// One sync refresh on connect, like the pipeline stream: a page that
		// rendered before the stream opened may have missed the only change
		// it will ever see (the persona landing between render and connect),
		// and the opening snapshot already carries it — push would never
		// fire on its own.
		if !synced {
			synced = true
			sseEvent(w, "refresh")
			return false
		}
		if now := h.pageSignature(p, onWaitPage); now != last {
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
		data, _ := json.Marshal(h.screen.Payload())
		fmt.Fprintf(w, "data: %s\n\n", data)
		w.(http.Flusher).Flush()
		return false
	})
}
