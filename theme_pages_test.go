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

	participantPages := []string{
		"/user",
		"/user/onboard/c",
		"/user/wait/d",
		"/user/match/a",
		"/user/explore/a/b",
		"/admin",
		"/data",
	}
	for _, path := range participantPages {
		t.Run(path, func(t *testing.T) {
			body := readBody(t, get(t, srv, path))
			if !strings.Contains(body, "data-theme=") {
				t.Errorf("%s: missing <html data-theme>", path)
			}
			if !strings.Contains(body, "@theme inline") {
				t.Errorf("%s: missing the Theme catalogue's CSS", path)
			}
			if !strings.Contains(body, "fragment-theme-device-choice") && !strings.Contains(body, `localStorage.getItem('theme')`) {
				t.Errorf("%s: missing the device-choice script (#41)", path)
			}
			if !strings.Contains(body, `data-theme-option="classic"`) || !strings.Contains(body, `data-theme-option="neon"`) || !strings.Contains(body, `data-theme-option="paper"`) {
				t.Errorf("%s: picker should list exactly the 3 catalogue Themes", path)
			}
		})
	}

	t.Run("/bigscreen", func(t *testing.T) {
		body := readBody(t, get(t, srv, "/bigscreen"))
		if !strings.Contains(body, "data-theme=") || !strings.Contains(body, "@theme inline") {
			t.Error("the Big Screen should still get the Theme styles")
		}
		if strings.Contains(body, `localStorage.getItem('theme')`) {
			t.Error("the Big Screen must ignore any device-stored choice (#42)")
		}
		if strings.Contains(body, "data-theme-option=") {
			t.Error("the Big Screen should not show the Participant picker")
		}
	})
}
