package main

import (
	"regexp"
	"testing"

	"github.com/dop251/goja"
)

// inlineScriptRe matches inline <script> blocks — external scripts (with a
// src attribute) never match.
var inlineScriptRe = regexp.MustCompile(`(?s)<script>(.*?)</script>`)

// inlineScripts extracts the inline script blocks from a rendered page.
func inlineScripts(html string) []string {
	matches := inlineScriptRe.FindAllStringSubmatch(html, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}

// TestInlineScripts_Parse guards the failure mode behind #129: the big
// screen's script declared `const panel` twice, a SyntaxError that killed the
// whole script block — no graph, no stream, no QR swap, a silently dead
// screen. Every inline script on every rendered page must parse.
func TestInlineScripts_Parse(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	seed(t, deps.db, "c", "The Lone Wolf", "ready")
	pair(t, deps.db, "a", "b")
	if err := deps.db.CreateParticipant("o", "o", "O", true); err != nil {
		t.Fatal(err)
	}
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	token, err := NewRecaps(deps.db).TokenFor("a")
	if err != nil {
		t.Fatal(err)
	}

	pages := []string{
		"/bigscreen",
		"/user",
		"/user/wait/c",
		"/user/onboard/o",
		"/user/match/a",
		"/user/recap/" + token,
		"/admin",
	}
	for _, path := range pages {
		body := readBody(t, get(t, srv, path))
		scripts := inlineScripts(body)
		if len(scripts) == 0 {
			t.Errorf("%s: expected at least one inline script", path)
			continue
		}
		for i, src := range scripts {
			if _, err := goja.Compile(path, src, false); err != nil {
				t.Errorf("%s inline script %d does not parse: %v", path, i, err)
			}
		}
	}
}

// TestRenderedPages_DoNotCache guards the mid-event update case: pages are
// revalidated on every load, so a restarted server with new templates is
// never masked by a cached page running its old scripts.
func TestRenderedPages_DoNotCache(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")

	for _, path := range []string{"/bigscreen", "/user", "/user/wait/a", "/admin"} {
		if cc := get(t, srv, path).Header.Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("%s: want Cache-Control no-cache, got %q", path, cc)
		}
	}
}
