package main

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestThemeByName(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
	}{
		{"midnight", "midnight"}, // known: the registry entry
		{"daylight", "daylight"}, // known: the registry entry
		{"", "midnight"},         // no choice: the default
		{"sepia", "midnight"},    // unknown: the default
	} {
		if got := themeByName(tc.name); got.Name != tc.want {
			t.Errorf("themeByName(%q): want %s, got %s", tc.name, tc.want, got.Name)
		}
	}
}

func TestThemeCSSInjectsTheThemeAsVariables(t *testing.T) {
	if css := string(themeCSS("daylight")); !strings.Contains(css, "--color-bg:#f9fafb") || !strings.Contains(css, "--color-surface-hover:#e5e7eb") {
		t.Errorf("daylight: want its hexes as CSS variables, got %s", css)
	}
	// An empty or unknown name gives the default — never a broken block.
	for _, name := range []string{"", "sepia"} {
		if css := string(themeCSS(name)); !strings.HasPrefix(css, "<style>:root{") || !strings.Contains(css, "--color-bg:#030712") {
			t.Errorf("themeCSS(%q): want the default Midnight block, got %s", name, css)
		}
	}
}

// TestThemeCSS_EmitsTheWalkthroughVariables pins the landing page walkthrough's
// dedicated variables (#173): themeCSS emits them after the ten role variables,
// with each theme's locked per-theme values, in sorted key order — so the
// emitted block is stable across runs.
func TestThemeCSS_EmitsTheWalkthroughVariables(t *testing.T) {
	for _, tc := range []struct {
		theme string
		want  []string // sorted by variable name, as emitted
	}{
		{"midnight", []string{
			"--color-walk-amber-400:#fbbf24",
			"--color-walk-emerald-300:#6ee7b7",
			"--color-walk-fuchsia-400:#f0abfc",
			"--color-walk-gray-500:#6b7280",
			"--color-walk-gray-600:#4b5563",
			"--color-walk-green-500:#22c55e",
			"--color-walk-green-950:#052e16",
			"--color-walk-indigo-300:#a5b4fc",
			"--color-walk-indigo-500:#6366f1",
			"--color-walk-indigo-900:#312e81",
			"--color-walk-indigo-950:#1e1b4b",
		}},
		{"daylight", []string{
			"--color-walk-amber-400:#b45309",
			"--color-walk-emerald-300:#059669",
			"--color-walk-fuchsia-400:#c026d3",
			"--color-walk-gray-500:#6b7280",
			"--color-walk-gray-600:#4b5563",
			"--color-walk-green-500:#22c55e",
			"--color-walk-green-950:#052e16",
			"--color-walk-indigo-300:#4f46e5",
			"--color-walk-indigo-500:#6366f1",
			"--color-walk-indigo-900:#e0e7ff",
			"--color-walk-indigo-950:#eef2ff",
		}},
	} {
		css := string(themeCSS(tc.theme))

		// Every value present, after the role variables, in sorted key order.
		at := strings.Index(css, "--color-surface-hover:")
		if at < 0 {
			t.Fatalf("themeCSS(%q): want the role variables before the walk variables, got %s", tc.theme, css)
		}
		for _, want := range tc.want {
			next := strings.Index(css[at:], want)
			if next < 0 {
				t.Errorf("themeCSS(%q): want %s after the role variables, got %s", tc.theme, want, css)
				break
			}
			at += next + len(want)
		}
		// The walk variables close the block: nothing theme-colored follows.
		if tail := css[at:]; !strings.HasPrefix(tail, ";--color-walk-") && !strings.HasPrefix(tail, "}") {
			t.Errorf("themeCSS(%q): want the walk variables last, got tail %q", tc.theme, tail)
		}

		// The block is deterministic: two calls emit byte-identical CSS.
		if again := string(themeCSS(tc.theme)); again != css {
			t.Errorf("themeCSS(%q): want a stable block across calls", tc.theme)
		}
	}
}

// TestSaveTheme_SavesTheChoiceAndRedirectsToTheirPage covers the theme
// picker's save: the posted name is stored via the store, and the response
// is an HX-Redirect to the Participant's destination page — the page's head
// re-renders with the new theme; the response carries no body.
func TestSaveTheme_SavesTheChoiceAndRedirectsToTheirPage(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")

	resp := post(t, srv, "/user/theme/a", url.Values{"theme": {"daylight"}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /user/theme/a: want 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("HX-Redirect"); got != "/user/wait/a" {
		t.Errorf("HX-Redirect: want /user/wait/a, got %q", got)
	}
	if got := reload(t, deps.db, "a").Theme; got != "daylight" {
		t.Errorf("stored theme: want daylight, got %q", got)
	}
}

func TestSaveTheme_UnknownParticipantIsNotFound(t *testing.T) {
	srv, _ := newTestServer(t, nil, nil)
	if resp := post(t, srv, "/user/theme/nobody", url.Values{"theme": {"daylight"}}); resp.StatusCode != http.StatusNotFound {
		t.Errorf("POST /user/theme/nobody: want 404, got %d", resp.StatusCode)
	}
}
