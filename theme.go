package main

import "html/template"

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
}

// themeDefault names the Theme used when a Participant has made no choice,
// and for the host console.
const themeDefault = "midnight"

// themes is the one source of Themes, ordered for the pickers.
var themes = []Theme{
	{Name: "midnight", Label: "Midnight", Background: "#030712", Surface: "#111827", SurfaceAlt: "#1f2937",
		Text: "#ffffff", TextMuted: "#9ca3af", Border: "#1f2937", Accent: "#4f46e5", AccentText: "#ffffff",
		AccentMuted: "#818cf8", SurfaceHover: "#374151"},
	{Name: "daylight", Label: "Daylight", Background: "#f9fafb", Surface: "#ffffff", SurfaceAlt: "#f3f4f6",
		Text: "#111827", TextMuted: "#4b5563", Border: "#e5e7eb", Accent: "#4f46e5", AccentText: "#ffffff",
		AccentMuted: "#4f46e5", SurfaceHover: "#e5e7eb"},
}

// themeByName returns the named Theme; unknown names get the default.
func themeByName(name string) Theme {
	for _, t := range themes {
		if t.Name == name {
			return t
		}
	}
	for _, t := range themes {
		if t.Name == themeDefault {
			return t
		}
	}
	return themes[0]
}

// themeCSS returns the inline <style> block that injects the named Theme's
// color roles as CSS variables on :root. An empty name gives the default.
func themeCSS(name string) template.HTML {
	t := themeByName(name)
	return template.HTML("<style>:root{" +
		"--color-bg:" + t.Background +
		";--color-surface:" + t.Surface +
		";--color-surface-alt:" + t.SurfaceAlt +
		";--color-text:" + t.Text +
		";--color-text-muted:" + t.TextMuted +
		";--color-border:" + t.Border +
		";--color-accent:" + t.Accent +
		";--color-accent-text:" + t.AccentText +
		";--color-accent-muted:" + t.AccentMuted +
		";--color-surface-hover:" + t.SurfaceHover +
		"}</style>")
}
