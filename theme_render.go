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
	return template.HTML(b.String())
}

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
