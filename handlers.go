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

	"github.com/google/uuid"
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
	Matched     bool   `json:"matched"`
	Handle      string `json:"handle"`
}

type graphEdge struct {
	Source  string `json:"source"`
	Target  string `json:"target"`
	Score   int    `json:"score"`
	Matched bool   `json:"matched"`
}

// Handler handles HTTP requests for the web application.
type Handler struct {
	db        *DB
	agents    *AgentPipeline
	interview *Interview
	matcher   *Matcher
	relations *Relationships
	tmpl      *template.Template
}

// NewHandler creates a new Handler with the given dependencies.
// It initializes the templates with the provided database, AgentPipeline, Interview module, Matcher and Relationship module.
func NewHandler(db *DB, agents *AgentPipeline, interview *Interview, matcher *Matcher, relations *Relationships) *Handler {
	funcs := template.FuncMap{
		"add":    func(a, b int) int { return a + b },
		"badges": func(p GitHubProfile) []Badge { return computeBadges(p) },
		"percent": func(n, total int) int {
			if total == 0 {
				return 0
			}
			return n * 100 / total
		},
		"divInt": func(a, b int) int {
			if b == 0 {
				return 0
			}
			return a / b
		},
		"colorName": func(c string) string {
			// "bg-teal-400" -> "teal"
			c = strings.TrimPrefix(c, "bg-")
			if idx := strings.LastIndex(c, "-"); idx != -1 {
				return c[:idx]
			}
			return c
		},
		"textColor": paletteText,
	}
	tmpl := template.Must(template.New("").Funcs(funcs).ParseGlob(filepath.Join("templates", "*.html")))
	return &Handler{db: db, agents: agents, interview: interview, matcher: matcher, relations: relations, tmpl: tmpl}
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
		if p, err := h.db.GetParticipant(c.Value); err == nil {
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

	if noGitHub {
		id := uuid.New().String()
		// For non-GitHub users, generate a unique handle to avoid UNIQUE constraint violation
		handle = "no-github-" + id[:8]
		if err := h.db.CreateParticipant(id, handle, name, false); err != nil {
			log.Printf("CreateParticipant failed for non-GitHub user name=%s: %v", name, err)
			http.Error(w, "registration failed: "+err.Error(), 500)
			return
		}

		// Non-GitHub users will get ExtraQuestions during interview
		go h.agents.RunSetup(id)
		setParticipantCookie(w, id)
		http.Redirect(w, r, "/user/onboard/"+id, http.StatusSeeOther)
		return
	}

	if existing, err := h.db.GetParticipantByHandle(handle); err == nil {
		setParticipantCookie(w, existing.ID)
		http.Redirect(w, r, "/user/onboard/"+existing.ID, http.StatusSeeOther)
		return
	} else if err != nil {
		log.Printf("GetParticipantByHandle failed for handle=%s: %v", handle, err)
	}

	id := uuid.New().String()
	if err := h.db.CreateParticipant(id, handle, name, true); err != nil {
		log.Printf("CreateParticipant failed for handle=%s, name=%s: %v", handle, name, err)
		http.Error(w, "registration failed: "+err.Error(), 500)
		return
	}

	go h.agents.RunSetup(id)
	setParticipantCookie(w, id)
	http.Redirect(w, r, "/user/onboard/"+id, http.StatusSeeOther)
}

// GET /user/onboard/{id}
func (h *Handler) Onboard(w http.ResponseWriter, r *http.Request) {
	p, err := h.db.GetParticipant(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch p.PipelineStep {
	case "ready":
		http.Redirect(w, r, h.matchOrWait(p), http.StatusSeeOther)
	default:
		h.render(w, "onboard.html", p)
	}
}

// GET /user/pipeline/{id}  — HTMX polled every 2s
func (h *Handler) PipelineStatus(w http.ResponseWriter, r *http.Request) {
	p, err := h.db.GetParticipant(r.PathValue("id"))
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}

	switch p.PipelineStep {
	case "ready":
		w.Header().Set("HX-Redirect", h.matchOrWait(p))
	case "interviewing":
		if qd := h.interview.Next(p); qd != nil {
			h.render(w, "fragment-question.html", qd)
			return
		}
		// Every question is answered and the persona is being crafted: keep polling.
		h.render(w, "fragment-pipeline-step.html", p)
	default:
		h.render(w, "fragment-pipeline-step.html", p)
	}
}

// POST /user/answer/{id}
func (h *Handler) SubmitAnswer(w http.ResponseWriter, r *http.Request) {
	p, err := h.db.GetParticipant(r.PathValue("id"))
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}

	if p.PipelineStep != "interviewing" {
		w.Header().Set("HX-Redirect", "/user/wait/"+p.ID)
		return
	}

	done, err := h.interview.Submit(p, r.FormValue("answer"))
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
		go h.agents.RunFinalSetup(p.ID)
		w.Header().Set("HX-Redirect", "/user/wait/"+p.ID)
		return
	}

	h.render(w, "fragment-question.html", h.interview.Next(p))
}

// GET /user/wait/{id}
func (h *Handler) Wait(w http.ResponseWriter, r *http.Request) {
	p, err := h.db.GetParticipant(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if h.matchRevealed(p) {
		http.Redirect(w, r, "/user/match/"+p.ID, http.StatusSeeOther)
		return
	}

	profile := *p.Profile
	answers := p.Answers
	if answers == nil {
		answers = map[string]string{}
	}
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
		"Phase":       h.phase(),
	})
}

// GET /user/wait-status/{id}  — HTMX polled every 3s
func (h *Handler) WaitStatus(w http.ResponseWriter, r *http.Request) {
	p, err := h.db.GetParticipant(r.PathValue("id"))
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	if h.matchRevealed(p) {
		w.Header().Set("HX-Redirect", "/user/match/"+p.ID)
		return
	}
	h.render(w, "fragment-wait-status.html", map[string]any{
		"Participant": p,
		"Count":       h.db.ReadyCount(),
		"Phase":       h.phase(),
	})
}

// GET /user/match/{id}
func (h *Handler) Match(w http.ResponseWriter, r *http.Request) {
	p, err := h.db.GetParticipant(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !h.matchRevealed(p) {
		http.Redirect(w, r, "/user/wait/"+p.ID, http.StatusSeeOther)
		return
	}

	match, result, err := h.relations.PartnerOf(p.ID)
	if err != nil {
		log.Printf("Match page for %s: %v", p.ID, err)
	}
	if result == nil {
		result = &matchResult{RedFlags: []string{}, GreenFlags: []string{}, Icebreakers: []string{}}
	}

	all, _ := h.db.GetAllParticipants()
	var others []*Participant
	for _, op := range all {
		if op.ID != p.ID && op.ID != p.MatchedWith {
			others = append(others, op)
		}
	}

	h.render(w, "match.html", map[string]any{
		"Me":          p,
		"Match":       match,
		"RedFlags":    result.RedFlags,
		"GreenFlags":  result.GreenFlags,
		"Icebreakers": result.Icebreakers,
		"Others":      others,
	})
}

// GET /user/explore/{myId}/{otherId}
func (h *Handler) Explore(w http.ResponseWriter, r *http.Request) {
	me, err := h.db.GetParticipant(r.PathValue("myId"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	other, err := h.db.GetParticipant(r.PathValue("otherId"))
	if err != nil {
		http.NotFound(w, r)
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

// ── /bigscreen ─────────────────────────────────────────────────────────────

// GET /bigscreen
func (h *Handler) Screen(w http.ResponseWriter, r *http.Request) {
	h.render(w, "screen.html", map[string]any{"Palette": paletteHex()})
}

func (h *Handler) buildGraphPayload() map[string]any {
	phase, _ := h.db.GetPhase()
	participants, _ := h.db.GetAllParticipants()
	activity, _ := h.db.GetRecentActivity(3)

	nodes := make([]graphNode, 0, len(participants))
	for _, p := range participants {
		nodes = append(nodes, graphNode{
			ID:          p.ID,
			PersonaName: p.PersonaName,
			Color:       p.PersonaColor,
			Symbol:      p.PersonaSymbol,
			Step:        p.PipelineStep,
			Matched:     p.IsMatched(),
			Handle:      p.DisplayHandle(),
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
			Score:   p.CompatScore,
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
		"phase":    phase,
		"nodes":    nodes,
		"edges":    edges,
		"activity": activity,
	}
}

// GET /bigscreen/graph-data  — kept as fallback; big screen now uses /bigscreen/stream
func (h *Handler) GraphData(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.buildGraphPayload())
}

// GET /bigscreen/state  — HTMX polled every 3s
func (h *Handler) ScreenState(w http.ResponseWriter, r *http.Request) {
	phase, _ := h.db.GetPhase()
	participants, _ := h.db.GetAllParticipants()
	activity, _ := h.db.GetRecentActivity(8)
	h.render(w, "fragment-screen-state.html", map[string]any{
		"Phase":        phase,
		"Participants": participants,
		"Activity":     activity,
		"Count":        len(participants),
		"ReadyCount":   h.db.ReadyCount(),
	})
}

// ── /data ──────────────────────────────────────────────────────────────────

// GET /data
func (h *Handler) DataIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/data" && r.URL.Path != "/data/" {
		http.NotFound(w, r)
		return
	}
	phase, _ := h.db.GetPhase()
	participants, _ := h.db.GetAllParticipants()
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
	participants, err := h.db.GetAllParticipants()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, participants)
}

// GET /data/participant/{id}
func (h *Handler) DataParticipant(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := h.db.GetParticipant(id)
	if err != nil {
		// Try by GitHub handle
		p, err = h.db.GetParticipantByHandle(id)
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
	phase, _ := h.db.GetPhase()
	writeJSON(w, map[string]any{
		"phase": phase,
		"count": h.db.ParticipantCount(),
		"ready": h.db.ReadyCount(),
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

// ── /admin ─────────────────────────────────────────────────────────────────

// GET /admin
func (h *Handler) Admin(w http.ResponseWriter, r *http.Request) {
	phase, _ := h.db.GetPhase()
	h.render(w, "admin.html", map[string]any{
		"Phase": phase,
		"Count": h.db.ParticipantCount(),
		"Ready": h.db.ReadyCount(),
	})
}

// POST /admin/reveal: the admin reveals every Match once everybody is seated.
func (h *Handler) TriggerReveal(w http.ResponseWriter, r *http.Request) {
	h.db.SetPhase("revealed")
	h.db.LogActivity("🎉 The matches are revealed!")
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// phase returns the Event State: "onboarding" before the Reveal, "revealed" after.
func (h *Handler) phase() string {
	phase, _ := h.db.GetPhase()
	return phase
}

// matchRevealed reports whether p may see their Match: they have one and the admin revealed.
func (h *Handler) matchRevealed(p *Participant) bool {
	return p.IsMatched() && h.phase() == "revealed"
}

// matchOrWait is the page for a matched Participant: their Match after the Reveal, else the wait page.
func (h *Handler) matchOrWait(p *Participant) string {
	if h.matchRevealed(p) {
		return "/user/match/" + p.ID
	}
	return "/user/wait/" + p.ID
}

// POST /admin/reset
func (h *Handler) Reset(w http.ResponseWriter, r *http.Request) {
	if err := h.db.Reset(); err != nil {
		http.Error(w, "reset failed: "+err.Error(), 500)
		return
	}
	// Clear LLM cache when resetting event
	h.matcher.ClearCache()
	h.db.SetPhase("onboarding")
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// POST /admin/rematch
func (h *Handler) Rematch(w http.ResponseWriter, r *http.Request) {
	h.db.LogActivity("🔄 Admin triggered full rematch")
	go func() {
		if err := h.agents.Rematch(); err != nil {
			log.Printf("Rematch error: %v", err)
		}
	}()
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// DELETE /data/participant/{id}
func (h *Handler) DeleteParticipant(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.relations.Remove(id); err != nil {
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

// GET /user/pipeline-stream/{id}
// Pushes a redirect event when the participant transitions to ready or matched.
// Complements HTMX polling (which handles the spinner HTML updates).
func (h *Handler) PipelineStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.db.GetParticipant(id); err != nil {
		http.NotFound(w, r)
		return
	}
	if !sseHeaders(w) {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			p, err := h.db.GetParticipant(id)
			if err != nil {
				return
			}
			switch p.PipelineStep {
			case "ready":
				sseRedirect(w, h.matchOrWait(p))
				return
			}
		}
	}
}

// GET /user/wait-stream/{id}
// Pushes a redirect event when the participant is matched.
func (h *Handler) WaitStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.db.GetParticipant(id); err != nil {
		http.NotFound(w, r)
		return
	}
	if !sseHeaders(w) {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			p, err := h.db.GetParticipant(id)
			if err != nil {
				return
			}
			if h.matchRevealed(p) {
				sseRedirect(w, "/user/match/"+p.ID)
				return
			}
		}
	}
}

// GET /bigscreen/stream
// Pushes graph-data JSON for D3, replacing the JS setTimeout poll.
func (h *Handler) ScreenStream(w http.ResponseWriter, r *http.Request) {
	if !sseHeaders(w) {
		return
	}
	flusher := w.(http.Flusher)
	sendData := func() {
		data, _ := json.Marshal(h.buildGraphPayload())
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
	sendData()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			sendData()
		}
	}
}
