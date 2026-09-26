package main

import (
	"strings"
	"testing"
)

// TestEveryPage_IncludesTheThemeStyles covers #40: every page renders the
// Theme catalogue as CSS and sets <html data-theme>, so nothing looks
// different yet but every page is ready for a later Theme switch.
func TestEveryPage_IncludesTheThemeStyles(t *testing.T) {
	llm := newFakeLLM().on("matchmaker", matchReply)
	srv, deps := newTestServer(t, llm, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	seed(t, deps.db, "c", "The Otter", "fetching_github")
	seed(t, deps.db, "d", "The Fox", "ready") // unmatched: stays on the wait page after the Reveal too
	pair(t, deps.db, "a", "b")
	deps.db.Reveal()

	pages := []string{
		"/user",
		"/user/onboard/c",
		"/user/wait/d",
		"/user/match/a",
		"/user/explore/a/b",
		"/bigscreen",
		"/admin",
		"/data",
	}
	for _, path := range pages {
		t.Run(path, func(t *testing.T) {
			body := readBody(t, get(t, srv, path))
			if !strings.Contains(body, "data-theme=") {
				t.Errorf("%s: missing <html data-theme>", path)
			}
			if !strings.Contains(body, "@theme inline") {
				t.Errorf("%s: missing the Theme catalogue's CSS", path)
			}
		})
	}
}
