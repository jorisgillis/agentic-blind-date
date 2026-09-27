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
		for _, hardCoded := range []string{"0.5s ease-out", "2s ease-in-out", "duration-1000", "300ms ease"} {
			if strings.Contains(src, hardCoded) {
				t.Errorf("%s: hard-codes an animation duration (%q) instead of a Theme token", path, hardCoded)
			}
		}
	}
}

// TestGameShowMoments_HaveMotion covers #85: each of the Game Show's five
// moments (a Round opens, a Round closes, the countdown's last 3 seconds,
// the matchmaking drumroll, a pair revealed) animates through a Theme-token
// class or transition, which the reduced-motion guard above already
// collapses for every Theme — there's no separate per-Theme CSS to check.
func TestGameShowMoments_HaveMotion(t *testing.T) {
	fragments := readFile(t, "templates/fragments.html")
	if !strings.Contains(fragments, `id="round-answer" class="bg-overlay/10 rounded-2xl p-6 mt-4 space-y-4 abd-pop-in"`) {
		t.Error("a Round opening should animate the answer pad in")
	}
	if !strings.Contains(fragments, "abd-countdown-urgent") {
		t.Error("the countdown's last 3 seconds should animate")
	}

	screen := readFile(t, "templates/screen.html")
	if !strings.Contains(screen, "calloutEl.classList.add('abd-pop-in')") {
		t.Error("a Round closing (the callout) should animate in")
	}
	if !strings.Contains(screen, "drumrollEl.classList.add('abd-pop-in')") {
		t.Error("the matchmaking drumroll should animate in")
	}
	if !strings.Contains(screen, "transition: filter var(--t-duration-slow)") {
		t.Error("a pair revealed should ease its glow in, not snap it on")
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
