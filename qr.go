package main

import (
	"net/http"
	"strconv"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// renderQRCode renders data as a PNG QR code of size×size pixels. QR codes
// are generated server-side: the big screen and the personal pages must work
// on venue WiFi without any third-party request from the browser, and no
// participant URL ever leaves the server for a QR image service.
func renderQRCode(data string, size int) ([]byte, error) {
	qr, err := qrcode.New(data, qrcode.Medium)
	if err != nil {
		return nil, err
	}
	return qr.PNG(size)
}

// qrPayload absolutizes a relative QR target with the request's scheme and
// host, so a QR always encodes a scannable absolute URL. Absolute URLs and
// non-path values pass through unchanged.
func qrPayload(r *http.Request, data string) string {
	if !strings.HasPrefix(data, "/") {
		return data
	}
	scheme := "http"
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	} else if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host + data
}

// GET /qr?data=...&size=... — a QR code as a PNG, data absolutized when
// relative (see qrPayload). size defaults to 150 and is clamped to 32..1024.
func (h *Handler) QR(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("data")
	if raw == "" {
		http.Error(w, "missing data", http.StatusBadRequest)
		return
	}
	size := 150
	if s := r.URL.Query().Get("size"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 32 || n > 1024 {
			http.Error(w, "size must be a number between 32 and 1024", http.StatusBadRequest)
			return
		}
		size = n
	}
	png, err := renderQRCode(qrPayload(r, raw), size)
	if err != nil {
		logger("handler").Error("rendering the QR code", "err", err)
		http.Error(w, "could not render the QR code", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write(png)
}
