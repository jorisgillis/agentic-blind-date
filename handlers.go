package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

type Handler struct {
	db     *DB
	agents *AgentPipeline
	tmpl   *template.Template
}

func newHandler(db *DB, github *GitHubClient, mistral *MistralClient) *Handler {
	agents := &AgentPipeline{db: db, github: github, mistral: mistral}
	funcs := template.FuncMap{
		"add": func(a, b int) int { return a + b },
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
		"textColor": func(bgClass string) string {
			m := map[string]string{
				"bg-teal-400":   "text-teal-900",
				"bg-red-400":    "text-red-900",
				"bg-purple-400": "text-purple-900",
				"bg-amber-400":  "text-amber-900",
				"bg-blue-400":   "text-blue-900",
			}
			if c, ok := m[bgClass]; ok {
				return c
			}
			return "text-gray-900"
		},
	}
	tmpl := template.Must(template.New("").Funcs(funcs).ParseGlob(filepath.Join("templates", "*.html")))
	return &Handler{db: db, agents: agents, tmpl: tmpl}
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

	if handle == "" {
		http.Error(w, "GitHub handle required", 400)
		return
	}

	if existing, err := h.db.GetParticipantByHandle(handle); err == nil {
		setParticipantCookie(w, existing.ID)
		http.Redirect(w, r, "/user/onboard/"+existing.ID, http.StatusSeeOther)
		return
	}

	id := uuid.New().String()
	if err := h.db.CreateParticipant(id, handle, name); err != nil {
		http.Error(w, "registration failed", 500)
		return
	}

	go h.agents.RunSetup(id, handle)
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
		http.Redirect(w, r, "/user/wait/"+p.ID, http.StatusSeeOther)
	case "matched":
		http.Redirect(w, r, "/user/match/"+p.ID, http.StatusSeeOther)
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
		w.Header().Set("HX-Redirect", "/user/wait/"+p.ID)
	case "matched":
		w.Header().Set("HX-Redirect", "/user/match/"+p.ID)
	case "interviewing":
		qd := h.buildQuestionData(p)
		if qd == nil {
			h.db.UpdatePipelineStep(p.ID, "ready")
			w.Header().Set("HX-Redirect", "/user/wait/"+p.ID)
			return
		}
		h.render(w, "fragment-question.html", qd)
	default:
		h.render(w, "fragment-pipeline-step.html", p)
	}
}

type QuestionData struct {
	ParticipantID string
	Index         int
	Total         int
	Question      Question
}

func (h *Handler) buildQuestionData(p *Participant) *QuestionData {
	var answers map[string]string
	json.Unmarshal([]byte(p.AnswersJSON), &answers)
	if answers == nil {
		answers = map[string]string{}
	}

	idx := len(answers)
	if idx >= TotalQuestions {
		return nil
	}

	var q Question
	if idx < TotalFixedQuestions {
		q = FixedQuestions[idx]
	} else {
		var customQs []string
		json.Unmarshal([]byte(p.CustomQuestions), &customQs)
		ci := idx - TotalFixedQuestions
		if ci < len(customQs) {
			q = Question{ID: strconv.Itoa(idx), Text: customQs[ci]}
		}
	}

	return &QuestionData{
		ParticipantID: p.ID,
		Index:         idx,
		Total:         TotalQuestions,
		Question:      q,
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

	answer := strings.TrimSpace(r.FormValue("answer"))

	var answers map[string]string
	json.Unmarshal([]byte(p.AnswersJSON), &answers)
	if answers == nil {
		answers = map[string]string{}
	}

	idx := len(answers)
	answers[strconv.Itoa(idx)] = answer
	answersJSON, _ := json.Marshal(answers)
	h.db.UpdateAnswers(p.ID, string(answersJSON))

	if idx+1 >= TotalQuestions {
		h.db.UpdatePipelineStep(p.ID, "ready")
		h.db.LogActivity(fmt.Sprintf("✅ %s finished the interview!", p.PersonaName))
		// Trigger continuous matching - match this participant against existing ready pool
		go func() {
			pFull, err := h.db.GetParticipant(p.ID)
			if err == nil {
				if err := h.agents.RunContinuousMatching(pFull); err != nil {
					log.Printf("Continuous matching error for %s: %v", p.PersonaName, err)
				}
			}
		}()
		w.Header().Set("HX-Redirect", "/user/wait/"+p.ID)
		return
	}

	p.AnswersJSON = string(answersJSON)
	h.render(w, "fragment-question.html", h.buildQuestionData(p))
}

// GET /user/wait/{id}
func (h *Handler) Wait(w http.ResponseWriter, r *http.Request) {
	p, err := h.db.GetParticipant(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if p.PipelineStep == "matched" {
		http.Redirect(w, r, "/user/match/"+p.ID, http.StatusSeeOther)
		return
	}

	var profile GitHubProfile
	json.Unmarshal([]byte(p.ProfileJSON), &profile)

	var answers map[string]string
	json.Unmarshal([]byte(p.AnswersJSON), &answers)
	var customQs []string
	json.Unmarshal([]byte(p.CustomQuestions), &customQs)

	type QAPair struct {
		Question string
		Answer   string
	}
	var qaPairs []QAPair
	for i := 0; i < TotalQuestions; i++ {
		ans, ok := answers[strconv.Itoa(i)]
		if !ok {
			break
		}
		var qText string
		if i < TotalFixedQuestions {
			qText = FixedQuestions[i].Text
		} else if ci := i - TotalFixedQuestions; ci < len(customQs) {
			qText = customQs[ci]
		}
		if qText != "" {
			qaPairs = append(qaPairs, QAPair{Question: qText, Answer: ans})
		}
	}

	h.render(w, "wait.html", map[string]any{
		"Participant": p,
		"Profile":     profile,
		"QAPairs":     qaPairs,
		"Count":       h.db.ReadyCount(),
	})
}

// GET /user/wait-status/{id}  — HTMX polled every 3s
func (h *Handler) WaitStatus(w http.ResponseWriter, r *http.Request) {
	p, err := h.db.GetParticipant(r.PathValue("id"))
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	if p.PipelineStep == "matched" {
		w.Header().Set("HX-Redirect", "/user/match/"+p.ID)
		return
	}
	phase, _ := h.db.GetPhase()
	h.render(w, "fragment-wait-status.html", map[string]any{
		"Participant": p,
		"Count":       h.db.ReadyCount(),
		"Phase":       phase,
	})
}

// GET /user/match/{id}
func (h *Handler) Match(w http.ResponseWriter, r *http.Request) {
	p, err := h.db.GetParticipant(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if p.PipelineStep != "matched" {
		http.Redirect(w, r, "/user/wait/"+p.ID, http.StatusSeeOther)
		return
	}

	var match *Participant
	if p.MatchedWith != "" {
		match, _ = h.db.GetParticipant(p.MatchedWith)
	}

	var redFlags, greenFlags, icebreakers []string
	json.Unmarshal([]byte(p.RedFlags), &redFlags)
	json.Unmarshal([]byte(p.GreenFlags), &greenFlags)
	json.Unmarshal([]byte(p.Icebreakers), &icebreakers)

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
		"RedFlags":    redFlags,
		"GreenFlags":  greenFlags,
		"Icebreakers": icebreakers,
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

	result, err := h.agents.generateMatch(me, other)
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
	h.render(w, "screen.html", nil)
}

// GET /bigscreen/graph-data  — polled by D3 frontend every 5s
func (h *Handler) GraphData(w http.ResponseWriter, r *http.Request) {
	phase, _ := h.db.GetPhase()
	participants, _ := h.db.GetAllParticipants()
	activity, _ := h.db.GetRecentActivity(3)

	type graphNode struct {
		ID          string `json:"id"`
		PersonaName string `json:"persona_name"`
		Color       string `json:"color"`
		Symbol      string `json:"symbol"`
		Step        string `json:"step"`
		Handle      string `json:"handle"`
	}
	type graphEdge struct {
		Source  string `json:"source"`
		Target  string `json:"target"`
		Score   int    `json:"score"`
		Matched bool   `json:"matched"`
	}

	nodes := make([]graphNode, 0, len(participants))
	for _, p := range participants {
		nodes = append(nodes, graphNode{
			ID:          p.ID,
			PersonaName: p.PersonaName,
			Color:       p.PersonaColor,
			Symbol:      p.PersonaSymbol,
			Step:        p.PipelineStep,
			Handle:      p.GitHubHandle,
		})
	}

	edges := make([]graphEdge, 0)
	seen := map[string]bool{}

	// Always show match edges when participants are matched (regardless of phase)
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

	// Show heuristic runner-up edges for all participants (top-2 after their actual match)
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
			s := pairScore(a, b)
			topEdges[a.ID] = append(topEdges[a.ID], scored{b.ID, s})
			topEdges[b.ID] = append(topEdges[b.ID], scored{a.ID, s})
		}
	}
	for _, p := range participants {
		candidates := topEdges[p.ID]
		sort.Slice(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
		if len(candidates) > 2 {
			candidates = candidates[:2]
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

	writeJSON(w, map[string]any{
		"phase":    phase,
		"nodes":    nodes,
		"edges":    edges,
		"activity": activity,
	})
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

// POST /admin/reveal - Deprecated with continuous matching, but kept for compatibility
func (h *Handler) TriggerReveal(w http.ResponseWriter, r *http.Request) {
	// With continuous matching, this endpoint is no longer needed
	// But we keep it for backward compatibility - it just redirects back
	h.db.LogActivity("ℹ️ Admin triggered reveal (continuous matching is active)")
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// POST /admin/reset
func (h *Handler) Reset(w http.ResponseWriter, r *http.Request) {
	if err := h.db.Reset(); err != nil {
		http.Error(w, "reset failed: "+err.Error(), 500)
		return
	}
	h.db.SetPhase("onboarding")
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// POST /admin/rematch
func (h *Handler) Rematch(w http.ResponseWriter, r *http.Request) {
	if err := h.db.UnmatchAll(); err != nil {
		http.Error(w, "rematch failed: "+err.Error(), 500)
		return
	}
	h.db.SetPhase("matching")
	h.db.LogActivity("🔄 Admin triggered full rematch")
	go func() {
		if err := h.agents.RunMatching(); err != nil {
			log.Printf("Rematch error: %v", err)
		}
	}()
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// DELETE /data/participant/{id}
func (h *Handler) DeleteParticipant(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.db.DeleteParticipant(id); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(http.StatusOK)
}
