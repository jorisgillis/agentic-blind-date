package main

import (
	"bytes"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRenderQRCode(t *testing.T) {
	pngBytes, err := renderQRCode("http://localhost:8080/user", 110)
	if err != nil {
		t.Fatalf("renderQRCode: %v", err)
	}
	if !bytes.HasPrefix(pngBytes, []byte("\x89PNG\r\n\x1a\n")) {
		t.Error("the QR code should be a PNG")
	}
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("the QR code should decode as PNG: %v", err)
	}
	if got := img.Bounds().Dx(); got != 110 {
		t.Errorf("want a 110px wide QR code, got %dpx", got)
	}
	if got := img.Bounds().Dy(); got != 110 {
		t.Errorf("want a 110px high QR code, got %dpx", got)
	}
}

func TestRenderQRCode_ContentTooLong(t *testing.T) {
	if _, err := renderQRCode(strings.Repeat("x", 5000), 150); err == nil {
		t.Error("content beyond the QR capacity should fail")
	}
}

func TestQRPayload(t *testing.T) {
	newReq := func(url string) *http.Request {
		r := httptest.NewRequest("GET", url, nil)
		r.Host = "blinddate.example"
		return r
	}
	t.Run("relative data is absolutized with http by default", func(t *testing.T) {
		if got := qrPayload(newReq("http://blinddate.example/qr"), "/user"); got != "http://blinddate.example/user" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("relative data honors X-Forwarded-Proto", func(t *testing.T) {
		r := newReq("http://blinddate.example/qr")
		r.Header.Set("X-Forwarded-Proto", "https")
		if got := qrPayload(r, "/user/m/abc"); got != "https://blinddate.example/user/m/abc" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("relative data is https on a TLS request", func(t *testing.T) {
		r := newReq("https://blinddate.example/qr")
		if got := qrPayload(r, "/recap"); got != "https://blinddate.example/recap" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("absolute URLs pass through", func(t *testing.T) {
		abs := "https://other.example/join"
		if got := qrPayload(newReq("http://blinddate.example/qr"), abs); got != abs {
			t.Errorf("got %q", got)
		}
	})
}

func TestQRHandler(t *testing.T) {
	srv, _ := testServer(t)

	t.Run("serves a PNG", func(t *testing.T) {
		resp := get(t, srv, "/qr?data=/user&size=110")
		if resp.StatusCode != 200 {
			t.Fatalf("want 200, got %d", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
			t.Errorf("want image/png, got %q", ct)
		}
		if body := readBody(t, resp); !bytes.HasPrefix([]byte(body), []byte("\x89PNG\r\n\x1a\n")) {
			t.Error("the body should be a PNG")
		}
	})
	t.Run("size defaults to 150", func(t *testing.T) {
		resp := get(t, srv, "/qr?data=/user")
		if resp.StatusCode != 200 {
			t.Fatalf("want 200, got %d", resp.StatusCode)
		}
	})
	t.Run("missing data is a 400", func(t *testing.T) {
		if resp := get(t, srv, "/qr"); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("want 400, got %d", resp.StatusCode)
		}
	})
	t.Run("a non-numeric size is a 400", func(t *testing.T) {
		if resp := get(t, srv, "/qr?data=/user&size=abc"); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("want 400, got %d", resp.StatusCode)
		}
	})
	t.Run("an out-of-range size is a 400", func(t *testing.T) {
		for _, size := range []string{"10", "2000"} {
			if resp := get(t, srv, "/qr?data=/user&size="+size); resp.StatusCode != http.StatusBadRequest {
				t.Errorf("size %s: want 400, got %d", size, resp.StatusCode)
			}
		}
	})
	t.Run("unencodable data is a 500", func(t *testing.T) {
		resp := get(t, srv, "/qr?data="+strings.Repeat("x", 5000))
		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("want 500, got %d", resp.StatusCode)
		}
	})
}

// TestBigScreenServesTheJoinQRWithoutAnyQRCodeLibrary covers #126: the big
// screen's QR is rendered by the server, so no browser-side QR library (and
// no CDN request that can kill the whole screen script) is involved anymore.
func TestBigScreenServesTheJoinQRWithoutAnyQRCodeLibrary(t *testing.T) {
	srv, _ := testServer(t)

	body := readBody(t, get(t, srv, "/bigscreen"))

	if !strings.Contains(body, `src="/qr?data=/user&size=110"`) {
		t.Error("the join QR should be served by /qr")
	}
	for _, gone := range []string{"qrcodejs", "cdnjs", "api.qrserver.com"} {
		if strings.Contains(body, gone) {
			t.Errorf("the big screen should not reference %q", gone)
		}
	}
}

// TestPersonalPagesServeQRsFromTheServer covers #126: the wait and match
// pages and the meet fragment render their QRs through /qr — the participant
// URLs never leave the server for a third-party image service. (The recap
// link is a button since the personal pages became the recap when closed —
// recap_test.go covers it.)
func TestPersonalPagesServeQRsFromTheServer(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Bard", StepReady)

	t.Run("the wait page", func(t *testing.T) {
		body := readBody(t, get(t, srv, "/user/wait/a"))
		if !strings.Contains(body, `src="/qr?data=/user/wait/a&size=120"`) {
			t.Error("the wait page QR should be served by /qr")
		}
		if strings.Contains(body, "api.qrserver.com") {
			t.Error("the wait page should not reference api.qrserver.com")
		}
	})
	t.Run("the meet status fragment", func(t *testing.T) {
		if err := deps.events.Advance(PhaseSocial); err != nil {
			t.Fatal(err)
		}
		body := readBody(t, get(t, srv, "/user/meet-status/a"))
		if !strings.Contains(body, `src="/qr?data=/user/m/a&size=150"`) {
			t.Error("the meet QR should be served by /qr")
		}
	})
	t.Run("the match page", func(t *testing.T) {
		seed(t, deps.db, "b", "The Fox", StepReady)
		pair(t, deps.db, "a", "b")
		body := readBody(t, get(t, srv, "/user/match/a"))
		if !strings.Contains(body, `src="/qr?data=/user/match/a&size=150"`) {
			t.Error("the match page QR should be served by /qr")
		}
		if strings.Contains(body, "api.qrserver.com") {
			t.Error("the match page should not reference api.qrserver.com")
		}
	})
}
