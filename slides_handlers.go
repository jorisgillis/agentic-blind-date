package main

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strconv"
)

// ── the personal page's Slide Decks ────────────────────────────────────────

// GET /user/slides/{id} — HTMX, the personal page's Slide Decks section,
// refreshed when the stream says a deck was added or removed.
func (h *Handler) Slides(w http.ResponseWriter, r *http.Request) {
	p, err := h.store.Get(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h.render(w, "fragment-slides.html", h.slidesData(p))
}

// slidesData is the personal page's Slide Decks section: every deck the Host
// uploaded, in upload order, in every phase. With no decks the section is empty.
func (h *Handler) slidesData(p *Participant) map[string]any {
	decks, err := h.slides.List()
	if err != nil {
		logger("handler").Warn("slide decks", "participant", p.ID, "err", err)
	}
	return map[string]any{"Me": p, "Decks": decks}
}

// GET /slides/{id} — the PDF, shown inline in the browser.
func (h *Handler) SlideDeckOpen(w http.ResponseWriter, r *http.Request) {
	h.serveSlideDeck(w, r, "inline")
}

// GET /slides/{id}/download — the same PDF, as a download.
func (h *Handler) SlideDeckDownload(w http.ResponseWriter, r *http.Request) {
	h.serveSlideDeck(w, r, "attachment")
}

func (h *Handler) serveSlideDeck(w http.ResponseWriter, r *http.Request, disposition string) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	deck, file, err := h.slides.Open(id)
	if errors.Is(err, ErrSlideDeckNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		logger("handler").Error("opening a slide deck", "deck", id, "err", err)
		http.Error(w, "opening the slides failed", 500)
		return
	}
	// Serving has read or errored by then; the close cannot matter.
	defer func() { _ = file.Close() }()

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": deck.Filename}))
	http.ServeContent(w, r, deck.Filename, deck.CreatedAt, file)
}

// ── the host console's Slide Decks ─────────────────────────────────────────

// GET /admin/slides — the Host's Slide Decks page: the upload form and every
// deck with its remove button. A page of its own, because the host console
// reloads itself every few seconds, which would wipe a chosen file.
func (h *Handler) AdminSlides(w http.ResponseWriter, r *http.Request) {
	h.render(w, "admin-slides.html", h.adminSlidesData(r.URL.Query().Get("notice")))
}

func (h *Handler) adminSlidesData(notice string) map[string]any {
	decks, err := h.slides.List()
	if err != nil {
		logger("handler").Warn("slide decks", "err", err)
	}
	return map[string]any{"Decks": decks, "Notice": notice, "MaxMB": maxSlideDeckBytes >> 20}
}

// POST /admin/slides — the Host uploads a PDF with a title and the speaker's
// name. A rejected upload re-renders the page with the reason, status 400.
func (h *Handler) AdminSlidesUpload(w http.ResponseWriter, r *http.Request) {
	reject := func(reason string) {
		h.renderStatus(w, http.StatusBadRequest, "admin-slides.html", h.adminSlidesData(reason))
	}

	// The cap on the whole request leaves room for the form fields around the file.
	r.Body = http.MaxBytesReader(w, r.Body, maxSlideDeckBytes+1<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil { //nolint:gosec // G120: bounded twice — the MaxBytesReader above caps the request, the 8 MB is the in-memory spool
		reject(slideUploadError(err))
		return
	}
	// Cleanup of the spooled temp files; the close error cannot matter.
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	file, header, err := r.FormFile("file")
	if err != nil {
		reject("Choose a PDF to upload.")
		return
	}
	// Add has consumed the file by then; the close cannot matter.
	defer func() { _ = file.Close() }()

	deck, err := h.slides.Add(r.FormValue("title"), r.FormValue("speaker"), header.Filename, file)
	var invalid *InvalidSlideDeckError
	if errors.As(err, &invalid) {
		reject(invalid.Error())
		return
	}
	if err != nil {
		logger("handler").Error("storing a slide deck", "err", err)
		http.Error(w, "storing the slides failed: "+err.Error(), 500)
		return
	}
	logger("handler").Info("slide deck uploaded", "deck", deck.ID, "size", deck.Size)
	http.Redirect(w, r, "/admin/slides?notice="+url.QueryEscape("📑 Uploaded “"+deck.Title+"”."), http.StatusSeeOther)
}

// slideUploadError is why the upload's multipart form could not be read, as
// the upload page shows it.
func slideUploadError(err error) string {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return fmt.Sprintf("That file is too large (the limit is %d MB).", maxSlideDeckBytes>>20)
	}
	return "The upload could not be read."
}

// POST /admin/slides/remove — the Host takes a deck down (the "id" form value).
func (h *Handler) AdminSlidesRemove(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "unknown slide deck", http.StatusBadRequest)
		return
	}
	err = h.slides.Remove(id)
	if errors.Is(err, ErrSlideDeckNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		logger("handler").Error("removing a slide deck", "deck", id, "err", err)
		http.Error(w, "removing the slides failed: "+err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/admin/slides?notice="+url.QueryEscape("🗑 Slides removed."), http.StatusSeeOther)
}

// formatFileSize renders a byte count for the Slide Decks lists.
func formatFileSize(n int64) string {
	const mb = 1 << 20
	if n >= mb {
		return fmt.Sprintf("%.1f MB", float64(n)/mb)
	}
	return fmt.Sprintf("%d KB", (n+1023)/1024)
}
