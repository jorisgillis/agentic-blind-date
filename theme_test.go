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

// TestSaveTheme_AFailedSaveIsAnError covers the save's failure path: the
// theme write fails in the database itself, and the response is a 500 —
// no redirect, the choice unapplied.
func TestSaveTheme_AFailedSaveIsAnError(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	breakOnFault(t, deps.db, "theme")

	resp := post(t, srv, "/user/theme/a", url.Values{"theme": {"daylight"}})
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("POST /user/theme/a: want 500, got %d", resp.StatusCode)
	}
	if got := reload(t, deps.db, "a").Theme; got != "midnight" {
		t.Errorf("the failed save must leave the theme alone, got %q", got)
	}
}

// picker is the theme-picker block of the settings section: the markup from
// the block's own label on.
func picker(body string) string {
	at := strings.Index(body, "🎨 Theme")
	if at < 0 {
		return ""
	}
	return body[at:]
}

// buttonFor is the picker's swatch button for the labeled theme, without the
// label line under it.
func buttonFor(p, label string) string {
	at := strings.Index(p, `aria-label="`+label+`"`)
	if at < 0 {
		return ""
	}
	return p[at : strings.Index(p[at:], "</button>")+at]
}

// TestSettings_ShowsTheThemePicker covers the settings section's theme picker
// (#174): one swatch form per registry entry — in the registry's stable order,
// posting to /user/theme/{id}, filled with each theme's literal colors — with
// the stored theme marked current: its button ringed and disabled, its label
// bold; every other label muted.
func TestSettings_ShowsTheThemePicker(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	if resp := post(t, srv, "/user/theme/a", url.Values{"theme": {"daylight"}}); resp.StatusCode != http.StatusOK {
		t.Fatalf("saving a's theme: want 200, got %d", resp.StatusCode)
	}

	p := picker(readBody(t, get(t, srv, "/user/settings/a")))

	// The block's own label and description.
	if !strings.Contains(p, "🎨 Theme") || !strings.Contains(p, "One click saves — the page reloads in its new look.") {
		t.Errorf("the picker carries its label and description, got: %s", p)
	}
	// One swatch form per registry entry, each posting to the theme save.
	if got := strings.Count(p, `hx-post="/user/theme/a"`); got != len(themes) {
		t.Errorf("want %d swatch forms posting to /user/theme/a, got %d in: %s", len(themes), got, p)
	}
	// The swatches in registry order — the registry's stable order is the
	// picker's order.
	last := -1
	for _, theme := range themes {
		at := strings.Index(p, `value="`+theme.Name+`"`)
		if at < 0 {
			t.Errorf("the picker shows the %s swatch, got: %s", theme.Name, p)
			continue
		}
		if at < last {
			t.Errorf("the %s swatch is out of registry order, got: %s", theme.Name, p)
		}
		last = at
	}
	// Every swatch shows its theme's literal colors — never the page's CSS
	// variables, which would render every swatch in the current theme.
	if !strings.Contains(p, `style="background: #f9fafb; border: 2px solid #e5e7eb"`) ||
		!strings.Contains(p, `style="background: #030712; border: 2px solid #1f2937"`) {
		t.Errorf("the swatches are filled with the themes' literal hexes, got: %s", p)
	}
	if strings.Count(p, `style="background: #4f46e5"`) != len(themes) {
		t.Errorf("each swatch carries its theme's literal accent dot, got: %s", p)
	}

	// The stored theme is marked current: ringed and disabled, its label bold.
	if marked := buttonFor(p, "Daylight"); !strings.Contains(marked, "disabled") || !strings.Contains(marked, "ring-2 ring-[var(--color-accent)]") {
		t.Errorf("the stored theme's swatch is ringed and disabled, got: %s", marked)
	}
	if !strings.Contains(p, `font-bold text-[var(--color-text)]">Daylight</p>`) {
		t.Errorf("the stored theme's label is bold in the text color, got: %s", p)
	}
	// Every other swatch is a plain button with a muted label.
	if plain := buttonFor(p, "Midnight"); strings.Contains(plain, "disabled") || strings.Contains(plain, "ring-2") {
		t.Errorf("only the stored theme is marked current, got: %s", plain)
	}
	if !strings.Contains(p, `text-[var(--color-text-muted)]">Midnight</p>`) {
		t.Errorf("the other labels sit muted, got: %s", p)
	}
	// No fragment wiring: the response to a swatch's post is an HX-Redirect, so
	// the forms carry no hx-target or hx-swap.
	for _, absent := range []string{"hx-target", "hx-swap", "hx-vals"} {
		if strings.Contains(p, absent) {
			t.Errorf("a swatch form carries %q — the save redirects the whole page, got: %s", absent, p)
		}
	}
}

// TestSettings_AnUnknownStoredThemeMarksNothingCurrent covers the lenient
// rule's render side: a stored name outside the registry matches no swatch, so
// nothing is marked current and every label sits muted.
func TestSettings_AnUnknownStoredThemeMarksNothingCurrent(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	if resp := post(t, srv, "/user/theme/a", url.Values{"theme": {"sepia"}}); resp.StatusCode != http.StatusOK {
		t.Fatalf("saving a's theme: want 200, got %d", resp.StatusCode)
	}

	p := picker(readBody(t, get(t, srv, "/user/settings/a")))

	if strings.Contains(p, "disabled") || strings.Contains(p, "ring-2") {
		t.Errorf("no swatch is current under an unknown stored theme, got: %s", p)
	}
	if !strings.Contains(p, `text-[var(--color-text-muted)]">Midnight</p>`) ||
		!strings.Contains(p, `text-[var(--color-text-muted)]">Daylight</p>`) {
		t.Errorf("every swatch label sits muted under an unknown stored theme, got: %s", p)
	}
}
