package main

import (
	"os"
	"strings"
	"testing"
)

// TestThemeStyleBlock_MotionUsesThemeTokensAndCollapsesUnderReducedMotion
// covers #43: the meaningful moments' CSS reads the current Theme's motion
// tokens (not a hard-coded duration), and every animation and transition
// collapses under prefers-reduced-motion.
func TestThemeStyleBlock_MotionUsesThemeTokensAndCollapsesUnderReducedMotion(t *testing.T) {
	css := string(themeStyleBlock())

	for _, want := range []string{
		"var(--t-duration-slow)",
		"var(--t-ease-emphasis)",
		"var(--t-duration-fast)",
		"var(--t-ease-standard)",
		"@media (prefers-reduced-motion: reduce)",
		"animation-duration: 0.001ms !important",
		"transition-duration: 0.001ms !important",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("motion CSS missing %q", want)
		}
	}
}

func TestEveryPage_HasTheReducedMotionGuard(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")

	for _, path := range []string{"/user", "/user/wait/a", "/bigscreen", "/admin"} {
		body := readBody(t, get(t, srv, path))
		if !strings.Contains(body, "prefers-reduced-motion: reduce") {
			t.Errorf("%s: missing the reduced-motion guard", path)
		}
	}
}

// TestTemplates_NoHardCodedAnimationDurations is a regression guard for
// #43: the meaningful moments must animate on Theme tokens, not a duration
// baked into a template.
func TestTemplates_NoHardCodedAnimationDurations(t *testing.T) {
	for _, path := range []string{
		"templates/match.html", "templates/explore.html", "templates/screen.html", "templates/fragments.html",
	} {
		src := readFile(t, path)
		for _, hardCoded := range []string{"0.5s ease-out", "2s ease-in-out"} {
			if strings.Contains(src, hardCoded) {
				t.Errorf("%s: hard-codes an animation duration (%q) instead of a Theme token", path, hardCoded)
			}
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
