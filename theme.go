package main

import (
	"html/template"
	"sort"
)

// Theme is a page-level palette: ten color roles injected as CSS variables.
// A Participant picks one in Settings; the Host picks the Public theme for
// the public pages. Role values are hex colors.
type Theme struct {
	Name         string // registry key, lowercase: "midnight", "daylight"
	Label        string // display name for the pickers: "Midnight", "Daylight"
	Background   string // page background          --color-bg
	Surface      string // cards, panels            --color-surface
	SurfaceAlt   string // inputs, hover surfaces   --color-surface-alt
	Text         string // primary text             --color-text
	TextMuted    string // secondary text           --color-text-muted
	Border       string // borders, dividers        --color-border
	Accent       string // buttons, links           --color-accent
	AccentText   string // text on accent           --color-accent-text
	AccentMuted  string // accent-tinted text       --color-accent-muted
	SurfaceHover string // hover bg for surfaces    --color-surface-hover

	// Walk holds the landing page walkthrough's dedicated variables
	// (--color-walk-<tailwind-source>), keyed by the variable tail
	// ("walk-gray-600", ...). The walkthrough's aliased hexes ride the
	// role variables above; these are the hexes with no role counterpart.
	Walk map[string]string
}

// themeDefault names the Theme used when a Participant has made no choice,
// and for the host console.
const themeDefault = "midnight"

// themes is the one source of Themes, ordered for the pickers.
var themes = []Theme{
	{Name: "midnight", Label: "Midnight", Background: "#030712", Surface: "#111827", SurfaceAlt: "#1f2937",
		Text: "#ffffff", TextMuted: "#9ca3af", Border: "#1f2937", Accent: "#4f46e5", AccentText: "#ffffff",
		AccentMuted: "#818cf8", SurfaceHover: "#374151",
		Walk: map[string]string{
			"walk-gray-600":    "#4b5563",
			"walk-gray-500":    "#6b7280",
			"walk-indigo-300":  "#a5b4fc",
			"walk-indigo-500":  "#6366f1",
			"walk-indigo-900":  "#312e81",
			"walk-indigo-950":  "#1e1b4b",
			"walk-fuchsia-400": "#f0abfc",
			"walk-emerald-300": "#6ee7b7",
			"walk-amber-400":   "#fbbf24",
			"walk-green-500":   "#22c55e",
			"walk-green-950":   "#052e16",
		}},
	{Name: "daylight", Label: "Daylight", Background: "#f9fafb", Surface: "#ffffff", SurfaceAlt: "#f3f4f6",
		Text: "#111827", TextMuted: "#4b5563", Border: "#e5e7eb", Accent: "#4f46e5", AccentText: "#ffffff",
		AccentMuted: "#4f46e5", SurfaceHover: "#e5e7eb",
		Walk: map[string]string{
			"walk-gray-600":    "#4b5563",
			"walk-gray-500":    "#6b7280",
			"walk-indigo-300":  "#4f46e5",
			"walk-indigo-500":  "#6366f1",
			"walk-indigo-900":  "#e0e7ff",
			"walk-indigo-950":  "#eef2ff",
			"walk-fuchsia-400": "#c026d3",
			"walk-emerald-300": "#059669",
			"walk-amber-400":   "#b45309",
			"walk-green-500":   "#22c55e",
			"walk-green-950":   "#052e16",
		}},
}

// themeByName returns the named Theme; unknown names get the default —
// the registry's first entry, which themeDefault names.
func themeByName(name string) Theme {
	for _, t := range themes {
		if t.Name == name {
			return t
		}
	}
	return themes[0]
}

// themeCSS returns the inline <style> block that injects the named Theme's
// color roles as CSS variables on :root. An empty name gives the default.
// The walkthrough variables follow the ten role variables, in sorted key
// order, so the emitted block is stable across runs.
func themeCSS(name string) template.HTML {
	t := themeByName(name)
	css := "<style>:root{" +
		"--color-bg:" + t.Background +
		";--color-surface:" + t.Surface +
		";--color-surface-alt:" + t.SurfaceAlt +
		";--color-text:" + t.Text +
		";--color-text-muted:" + t.TextMuted +
		";--color-border:" + t.Border +
		";--color-accent:" + t.Accent +
		";--color-accent-text:" + t.AccentText +
		";--color-accent-muted:" + t.AccentMuted +
		";--color-surface-hover:" + t.SurfaceHover
	keys := make([]string, 0, len(t.Walk))
	for key := range t.Walk {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		css += ";--color-" + key + ":" + t.Walk[key]
	}
	return template.HTML(css + "}</style>") //nolint:gosec // G203: every value comes from the theme registry's constant hex colors, never from input
}
