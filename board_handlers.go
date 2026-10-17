package main

import (
	"errors"
	"net/http"
	"strconv"
)

// ── the personal page's Board ──────────────────────────────────────────────

// GET /user/board/{id} — HTMX, the personal page's Board section, refreshed
// when the page's stream says a Post was approved, taken down or deleted.
func (h *Handler) BoardSection(w http.ResponseWriter, r *http.Request) {
	p, err := h.store.Get(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h.render(w, "fragment-board.html", h.boardData(p))
}

// boardData is the personal page's Board section: the post form — open for a
// ready Participant until the event closes — and one list, newest first, of
// every approved Post plus the Participant's own other Posts with their
// status. The form says how many Posts the Participant has left.
func (h *Handler) boardData(p *Participant) map[string]any {
	posts, err := h.board.Visible(p.ID)
	if err != nil {
		logger("handler").Warn("board posts", "participant", p.ID, "err", err)
	}
	return map[string]any{
		"Me":        p,
		"Posts":     posts,
		"Open":      p.PipelineStep.IsReady() && !h.events.State().IsClosed(),
		"Remaining": max(maxPostsPerParticipant-heldPosts(posts, p), 0),
		"MaxLength": maxPostLength,
	}
}

// heldPosts is how many Posts the Participant holds — the pending and
// approved ones counting against their cap.
func heldPosts(posts []Post, p *Participant) int {
	held := 0
	for _, post := range posts {
		if post.ParticipantID == p.ID && post.Status != PostRejected {
			held++
		}
	}
	return held
}

// POST /user/board/{id} — the Participant's Post. It waits for the Host's
// approval; a refused Post renders the section again with the reason, status
// 400.
func (h *Handler) BoardPost(w http.ResponseWriter, r *http.Request) {
	p, err := h.store.Get(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	err = h.board.Post(p, r.FormValue("text"))
	reason := postRefusalReason(err)
	if err != nil && reason == "" {
		logger("handler").Error("posting to the board", "participant", p.ID, "err", err)
		http.Error(w, "posting failed: "+err.Error(), 500)
		return
	}
	data := h.boardData(p)
	if reason != "" {
		data["Error"] = reason
		h.renderStatus(w, http.StatusBadRequest, "fragment-board.html", data)
		return
	}
	data["Posted"] = true
	h.render(w, "fragment-board.html", data)
}

// postRefusalReason is why the Board refused the Post, as the section shows
// it; empty when the Post went through or the failure is not the
// Participant's.
func postRefusalReason(err error) string {
	switch {
	case errors.Is(err, ErrInvalidPost):
		return err.Error()
	case errors.Is(err, ErrBoardClosed):
		return "The board is closed: the evening is over."
	case errors.Is(err, ErrNotReady):
		return "You can post once your persona is ready."
	case errors.Is(err, ErrPostCap):
		return "You have used all your posts: delete one to post again."
	}
	return ""
}

// POST /user/board/{id}/delete — the Participant takes one of their own Posts
// back (the "post" form value).
func (h *Handler) BoardDelete(w http.ResponseWriter, r *http.Request) {
	p, err := h.store.Get(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.ParseInt(r.FormValue("post"), 10, 64)
	if err != nil {
		http.Error(w, "unknown post", http.StatusBadRequest)
		return
	}
	err = h.board.Delete(p.ID, id)
	if errors.Is(err, ErrPostNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		logger("handler").Error("deleting a post", "participant", p.ID, "post", id, "err", err)
		http.Error(w, "deleting the post failed: "+err.Error(), 500)
		return
	}
	h.render(w, "fragment-board.html", h.boardData(p))
}

// ── the host console's Board ───────────────────────────────────────────────

// boardModerationData is the host console's Board section: the pending Posts
// to decide on, then the approved ones, each with its poster's Persona.
func (h *Handler) boardModerationData() map[string]any {
	posts, err := h.board.Moderation()
	if err != nil {
		logger("handler").Warn("board moderation", "err", err)
	}
	pending := 0
	for _, post := range posts {
		if post.Status == PostPending {
			pending++
		}
	}
	return map[string]any{"Posts": posts, "Pending": pending}
}

// POST /admin/board — the Host's decision on a Post (the "id" form value):
// "approved" puts it on the Board, "rejected" refuses it or takes an approved
// one down.
func (h *Handler) AdminBoard(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "That post is not on the board.", http.StatusBadRequest)
		return
	}
	to, ok := moderationStatus(r.FormValue("action"))
	if !ok {
		http.Error(w, "Unknown action — approved or rejected.", http.StatusBadRequest)
		return
	}
	if _, err := h.board.Moderate(id, to); err != nil {
		h.moderateError(w, id, err)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// moderationStatus maps the Host's decision to the Post status it moves the
// Post to; ok is false for an unknown decision.
func moderationStatus(action string) (to PostStatus, ok bool) {
	switch action {
	case "approved":
		return PostApproved, true
	case "rejected":
		return PostRejected, true
	}
	return PostApproved, false
}

// moderateError renders a moderation decision's failure.
func (h *Handler) moderateError(w http.ResponseWriter, id int64, err error) {
	switch {
	case errors.Is(err, ErrPostNotFound):
		http.Error(w, "That post is not on the board.", http.StatusNotFound)
	case errors.Is(err, ErrInvalidPostTransition):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		logger("handler").Error("moderating a post", "post", id, "err", err)
		http.Error(w, "updating the board failed: "+err.Error(), 500)
	}
}
