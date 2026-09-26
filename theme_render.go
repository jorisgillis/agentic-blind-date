package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"strings"
)

// themeKeysJSON renders the catalogue's keys as a JSON array, for the
// inline device-choice script to validate a stored choice against (#41)
// without a round trip to the server.
func themeKeysJSON() template.JS {
	keys := make([]string, len(Themes))
	for i, t := range Themes {
		keys[i] = t.Key
	}
	data, _ := json.Marshal(keys) // keys are Go string literals: this cannot fail
	return template.JS(data)
}

// themeSoundsJSON renders every Theme's sound cues, keyed by Theme key then
// cue name, for the shared sound module to look up the current Theme's
// recipe for a cue without a round trip to the server (#44). A Theme that
// leaves a cue silent simply has no entry for it.
func themeSoundsJSON() template.JS {
	byTheme := make(map[string]map[SoundCue]ToneRecipe, len(Themes))
	for _, t := range Themes {
		byTheme[t.Key] = t.Sounds
	}
	data, _ := json.Marshal(byTheme) // Theme keys and cue recipes are Go literals: this cannot fail
	return template.JS(data)
}

// themeStyleBlock renders the Theme catalogue as CSS: one @theme inline
// mapping (the same for every Theme, so Tailwind compiles utilities that
// read a CSS variable) and, for each Theme, a [data-theme="…"] block setting
// that variable's value. Switching <html data-theme> switches every value at
// once; HTMX and SSE never touch <html>, so a Theme survives them (ADR-0009,
// ticket 01). Generated once from the catalogue, so it can't drift (#40).
func themeStyleBlock() template.HTML {
	var b strings.Builder
	b.WriteString(`<style type="text/tailwindcss">` + "\n@theme inline {\n")
	for _, m := range themeVarMappings {
		fmt.Fprintf(&b, "  --%s: var(--t-%s);\n", m.tailwind, m.token)
	}
	b.WriteString("}\n</style>\n<style>\n")
	for _, t := range Themes {
		fmt.Fprintf(&b, "[data-theme=%q] {\n", t.Key)
		for _, kv := range themeValues(t) {
			fmt.Fprintf(&b, "  --t-%s: %s;\n", kv.token, kv.value)
		}
		b.WriteString("}\n")
	}
	b.WriteString("</style>\n")
	b.WriteString(motionStyleBlock)
	return template.HTML(b.String())
}

// motionStyleBlock gives the meaningful moments (ticket 02) their motion
// using the current Theme's duration and easing tokens, and collapses every
// CSS animation and transition under prefers-reduced-motion (#43). JS-driven
// motion (the Big Screen's D3 force graph) checks reducedMotion() itself,
// since this media query only reaches CSS.
const motionStyleBlock = `<style>
@keyframes abd-pop-in {
  0% { transform: scale(0.8); opacity: 0; }
  70% { transform: scale(1.05); }
  100% { transform: scale(1); opacity: 1; }
}
.abd-pop-in { animation: abd-pop-in var(--t-duration-slow) var(--t-ease-emphasis) forwards; }

@keyframes abd-pulse-match {
  0%, 100% { stroke-opacity: 0.9; }
  50% { stroke-opacity: 0.3; }
}
.abd-pulse-match { animation: abd-pulse-match calc(var(--t-duration-slow) * 4) ease-in-out infinite; }

.abd-fade { transition: opacity var(--t-duration-normal) var(--t-ease-standard); }
.htmx-swapping { opacity: 0; transition: opacity var(--t-duration-fast) var(--t-ease-standard); }

@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after {
    animation-duration: 0.001ms !important;
    animation-delay: 0ms !important;
    animation-iteration-count: 1 !important;
    transition-duration: 0.001ms !important;
    scroll-behavior: auto !important;
  }
}
</style>
<script>
// Shared by every page's JS-driven motion (the Big Screen's D3 force graph):
// CSS's prefers-reduced-motion media query above doesn't reach JS animation.
function agenticBlindDateReducedMotion() {
  return window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
}
</script>
`

// themeVarMapping is one entry of the theme-agnostic @theme inline block:
// a Tailwind theme key (what a template's utility classes read, e.g.
// "color-accent" for bg-accent/text-accent/...) backed by a token (what
// each [data-theme] block sets, e.g. "accent").
type themeVarMapping struct{ tailwind, token string }

var themeVarMappings = []themeVarMapping{
	{"color-page", "surface-page"},
	{"color-raised", "surface-raised"},
	{"color-overlay", "surface-overlay"},
	{"color-ink", "text-normal"},
	{"color-ink-muted", "text-muted"},
	{"color-accent", "accent"},
	{"color-on-accent", "text-on-accent"},
	{"color-line", "border"},
	{"color-ok", "state-positive"},
	{"color-bad", "state-negative"},
	{"color-warn", "state-warning"},
	{"color-highlight-from", "highlight-from"},
	{"color-highlight-to", "highlight-to"},
	{"color-on-persona", "text-on-persona"},
	{"color-persona-line", "persona-outline"},
	{"font-sans", "font-body"},
	{"font-display", "font-display"},
	{"font-mono", "font-mono"},
	{"radius-sm", "radius-sm"},
	{"radius-md", "radius-md"},
	{"radius-lg", "radius-lg"},
	{"radius-pill", "radius-pill"},
}

type themeTokenValue struct{ token, value string }

// themeValues lists every token a Theme sets, including the ones with no
// Tailwind utility mapping (border width, glow, motion): pages that need
// them read the --t-* variable directly.
func themeValues(t Theme) []themeTokenValue {
	return []themeTokenValue{
		{"surface-page", t.SurfacePage},
		{"surface-raised", t.SurfaceRaised},
		{"surface-overlay", t.SurfaceOverlay},
		{"text-normal", t.TextNormal},
		{"text-muted", t.TextMuted},
		{"accent", t.Accent},
		{"text-on-accent", t.TextOnAccent},
		{"border", t.Border},
		{"state-positive", t.StatePositive},
		{"state-negative", t.StateNegative},
		{"state-warning", t.StateWarning},
		{"highlight-from", t.HighlightFrom},
		{"highlight-to", t.HighlightTo},
		{"text-on-persona", t.TextOnPersona},
		{"persona-outline", t.PersonaOutline},
		{"font-display", t.FontDisplay},
		{"font-body", t.FontBody},
		{"font-mono", t.FontMono},
		{"font-display-weight", t.FontDisplayWeight},
		{"radius-sm", t.RadiusSm},
		{"radius-md", t.RadiusMd},
		{"radius-lg", t.RadiusLg},
		{"radius-pill", t.RadiusPill},
		{"border-width", t.BorderWidth},
		{"glow", t.Glow},
		{"duration-fast", t.DurationFast},
		{"duration-normal", t.DurationNormal},
		{"duration-slow", t.DurationSlow},
		{"ease-standard", t.EasingStandard},
		{"ease-emphasis", t.EasingEmphasis},
	}
}
