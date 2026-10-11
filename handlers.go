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
}

// Handler handles HTTP requests for the web application.
type Handler struct {
	db          *DB
	store       *ParticipantStore
	events      *EventStore
	meetings    *Meetings
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
func NewHandler(db *DB, onboarding *Onboarding, interview *Interview, matcher *Matcher, relations *Relationships, matchmaking *Matchmaking, streams StreamObserver) *Handler {
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
	return &Handler{
		db: db, store: NewParticipantStore(db), events: events, meetings: NewMeetings(db, events),
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

	h.render(w, "wait.html", map[string]any{
		"Participant": p,
		"Profile":     profile,
		"QAPairs":     qaPairs,
		"Count":       h.db.ReadyCount(),
		"MeetStatus":  h.meetStatusData(p),
	})
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
	h.render(w, "fragment-wait-status.html", map[string]any{
		"Participant": p,
		"Count":       h.db.ReadyCount(),
	})
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

	match, result, err := h.relations.PartnerOf(p.ID)
	if err != nil {
		log.Printf("Match page for %s: %v", p.ID, err)
	}
	if result == nil {
		result = &matchResult{RedFlags: []string{}, GreenFlags: []string{}, Icebreakers: []string{}}
	}

	all, _ := h.store.All()
	var others []*Participant
	for _, op := range all {
		if op.ID != p.ID && op.ID != p.MatchedWith {
			others = append(others, op)
		}
	}

	h.render(w, "match.html", map[string]any{
		"Me":          p,
		"Match":       match,
		"Score":       result.Score,
		"Reason":      result.Reason,
		"RedFlags":    result.RedFlags,
		"GreenFlags":  result.GreenFlags,
		"Icebreakers": result.Icebreakers,
		"Others":      others,
		"MeetStatus":  h.meetStatusData(p),
	})
}

// GET /user/explore/{myId}/{otherId}
//
// Explore is between two different ready Participants only (see CONTEXT.md):
// an unknown Participant is 404, and exploring yourself or a Participant who
// isn't ready is 400 — neither calls the LLM or GitHub.
func (h *Handler) Explore(w http.ResponseWriter, r *http.Request) {
	me, err := h.store.Get(r.PathValue("myId"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	other, err := h.store.Get(r.PathValue("otherId"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if me.ID == other.ID {
		http.Error(w, "cannot explore yourself", 400)
		return
	}
	if me.PipelineStep != StepReady || other.PipelineStep != StepReady {
		http.Error(w, "both participants must be ready", 400)
		return
	}

	result, err := h.matcher.ScorePair(me, other)
	if err != nil {
		http.Error(w, "compatibility analysis failed: "+err.Error(), 500)
		return
	}

	h.render(w, "explore.html", map[string]any{
		"Me":          me,
		"Other":       other,
		"Score":       result.Score,
		"Reason":      result.Reason,
		"GreenFlags":  result.GreenFlags,
		"RedFlags":    result.RedFlags,
		"Icebreakers": result.Icebreakers,
	})
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
// phase-gated to the social and show & tell.
func (h *Handler) meetStatusData(p *Participant) map[string]any {
	type meetingView struct {
		Other  *Participant
		Status MeetingStatus
	}
	var meetings []meetingView
	if h.events.State().AllowsMeetings() {
		ms, _ := h.meetings.ForParticipant(p.ID)
		for _, m := range ms {
			other, err := h.store.Get(m.Other(p.ID))
			if err != nil {
				continue // the other side left the event; the row is debug-only
			}
			meetings = append(meetings, meetingView{Other: other, Status: m.StatusFor(p.ID)})
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
// Tells the match page to refresh its meet section when the Participant's
// meetings or the phase change.
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

// meetSignature is what the meet streams watch: the phase and the
// Participant's meetings (their count and how many are completed).
type meetSignature struct {
	phase     Phase
	meetings  int
	completed int
}

func (h *Handler) meetSignature(p *Participant) meetSignature {
	ms, _ := h.meetings.ForParticipant(p.ID)
	completed := 0
	for _, m := range ms {
		if m.Completed() {
			completed++
		}
	}
	return meetSignature{h.events.State(), len(ms), completed}
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

	return map[string]any{
		"phase":       phase,
		"phase_label": phase.Label(),
		"revealed":    phase.AtLeast(PhaseSocial),
		"nodes":       nodes,
		"edges":       edges,
		"activity":    activity,
	}
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
	writeJSON(w, participants)
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
	writeJSON(w, p)
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

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

// ── /admin ─────────────────────────────────────────────────────────────────

// GET /admin: the host console — the current Phase with its label and the
// participant counts, plus one transition button per reachable forward phase.
func (h *Handler) Admin(w http.ResponseWriter, r *http.Request) {
	phase := h.events.State()
	h.render(w, "admin.html", map[string]any{
		"Phase":   phase,
		"Count":   h.db.ParticipantCount(),
		"Ready":   h.db.ReadyCount(),
		"Matched": h.db.MatchedCount(),
		"Next":    phase.Next(),
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
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
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

// POST /admin/rematch
func (h *Handler) Rematch(w http.ResponseWriter, r *http.Request) {
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
