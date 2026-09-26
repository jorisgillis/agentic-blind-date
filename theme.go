package main

// Theme is one look: named design tokens for colour, typography, shape,
// motion and sound, applied through CSS custom properties under
// <html data-theme> (ADR-0009, ticket 02 in .scratch/ui-and-themes/). The
// Persona palette is not part of any Theme: a Persona's colour, symbol and
// (on light Themes) outline are the same regardless of which Theme is active.
type Theme struct {
	Key  string // the data-theme value and the picker's value: "classic", "neon", "paper"
	Name string // shown in the picker: "Classic", "Neon", "Paper"

	// Colour roles (14: ticket 02's 12 plus ticket 03's Persona-related two).
	SurfacePage    string
	SurfaceRaised  string
	SurfaceOverlay string
	TextNormal     string
	TextMuted      string
	Accent         string
	TextOnAccent   string
	Border         string
	StatePositive  string
	StateNegative  string
	StateWarning   string
	HighlightFrom  string // the Match/Reveal gradient
	HighlightTo    string
	TextOnPersona  string // readable on every Persona colour (ticket 03)
	PersonaOutline string // drawn around Persona-coloured elements; "transparent" on dark Themes (ticket 03)

	// Typography: at most one self-hosted web font per Theme (ticket 02).
	FontDisplay       string
	FontBody          string
	FontMono          string
	FontDisplayWeight string
	WebFontURL        string // "" when the Theme uses only built-in system fonts

	// Shape.
	RadiusSm    string
	RadiusMd    string
	RadiusLg    string
	RadiusPill  string
	BorderWidth string
	Glow        string // a CSS box-shadow value; "none" for a flat Theme

	// Motion (#43): every duration and easing collapses under
	// prefers-reduced-motion regardless of Theme.
	DurationFast   string
	DurationNormal string
	DurationSlow   string
	EasingStandard string
	EasingEmphasis string

	// Sound (#44): a cue is a synthesised tone recipe, never an audio file.
	// A Theme may leave any cue silent by omitting it from the map.
	Sounds map[SoundCue]ToneRecipe
}

// SoundCue names one of the moments ticket 02 allows a Theme to give sound.
type SoundCue string

const (
	CueAnswerSubmitted SoundCue = "answer_submitted" // phone
	CueInterviewDone   SoundCue = "interview_done"    // phone
	CueYourMatch       SoundCue = "your_match"         // phone: the Reveal / your Match
	CueBigScreenMatch  SoundCue = "bigscreen_match"    // Big Screen: a new Match
	CueBigScreenReveal SoundCue = "bigscreen_reveal"   // Big Screen: the Reveal
)

// ToneRecipe is a synthesised sound cue (ticket 02): waveform, pitch and
// length are enough for the Web Audio API to play it with no audio file.
type ToneRecipe struct {
	Waveform string // "sine", "triangle", "sawtooth" or "square"
	Hz       float64
	Seconds  float64
}

// DefaultThemeKey is Classic: today's look, with no sound (ticket 02, 03).
const DefaultThemeKey = "classic"

// Themes is the catalogue, in the picker's order.
var Themes = []Theme{classicTheme, neonTheme, paperTheme}

// ThemeByKey returns the Theme for key, or the default Theme when key is
// unknown or blank (ticket 01: "an unknown or blocked choice falls back to
// the default Theme"). Themes always holds the default, so this always finds one.
func ThemeByKey(key string) Theme {
	var fallback Theme
	for _, t := range Themes {
		if t.Key == key {
			return t
		}
		if t.Key == DefaultThemeKey {
			fallback = t
		}
	}
	return fallback
}

var classicTheme = Theme{
	Key: "classic", Name: "Classic",

	SurfacePage:    "#030712", // Tailwind gray-950
	SurfaceRaised:  "#111827", // gray-900
	SurfaceOverlay: "rgb(0 0 0 / 0.15)", // a translucent overlay that must work on every Persona colour (ticket 02)
	TextNormal:     "#ffffff",
	TextMuted:      "#9ca3af", // gray-400
	Accent:         "#6366f1", // indigo-500
	TextOnAccent:   "#ffffff",
	Border:         "#1f2937", // gray-800
	StatePositive:  "#4ade80", // green-400
	StateNegative:  "#f87171", // red-400
	StateWarning:   "#facc15", // yellow-400
	HighlightFrom:  "#6366f1", // indigo-500
	HighlightTo:    "#ec4899", // pink-500
	TextOnPersona:  "#ffffff",
	PersonaOutline: "transparent", // Classic is dark: Persona colours already pass 3:1 (ticket 03)

	FontDisplay:       "ui-sans-serif, system-ui, sans-serif",
	FontBody:          "ui-sans-serif, system-ui, sans-serif",
	FontMono:          "ui-monospace, SFMono-Regular, monospace",
	FontDisplayWeight: "700",
	WebFontURL:        "",

	RadiusSm: "0.375rem", RadiusMd: "0.5rem", RadiusLg: "0.75rem", RadiusPill: "9999px",
	BorderWidth: "1px",
	Glow:        "none",

	DurationFast: "100ms", DurationNormal: "200ms", DurationSlow: "400ms",
	EasingStandard: "cubic-bezier(0.4, 0, 0.2, 1)", EasingEmphasis: "cubic-bezier(0.34, 1.56, 0.64, 1)",

	Sounds: map[SoundCue]ToneRecipe{}, // Classic: no sound (ticket 03)
}

var neonTheme = Theme{
	Key: "neon", Name: "Neon",

	SurfacePage:    "#1a0b2e",
	SurfaceRaised:  "#2d1b4e",
	SurfaceOverlay: "rgb(0 0 0 / 0.15)",
	TextNormal:     "#f5f3ff",
	TextMuted:      "#c4b5fd", // violet-300
	Accent:         "#ff2fd4", // neon magenta
	TextOnAccent:   "#ffffff",
	Border:         "#4c1d95", // violet-800
	StatePositive:  "#39ff14", // neon green
	StateNegative:  "#ff2965", // neon red
	StateWarning:   "#ffe135", // neon yellow
	HighlightFrom:  "#ff2fd4", // neon magenta
	HighlightTo:    "#22d3ee", // cyan-400
	TextOnPersona:  "#ffffff",
	PersonaOutline: "transparent", // Neon is dark: Persona colours already pass 3:1 (ticket 03)

	FontDisplay:       "ui-sans-serif, system-ui, sans-serif",
	FontBody:          "ui-sans-serif, system-ui, sans-serif",
	FontMono:          "ui-monospace, SFMono-Regular, monospace",
	FontDisplayWeight: "900", // bolder than Classic, for a louder look
	WebFontURL:        "",

	RadiusSm: "0.75rem", RadiusMd: "1.25rem", RadiusLg: "2rem", RadiusPill: "9999px", // large radii (ticket 03)
	BorderWidth: "2px",
	Glow:        "0 0 20px rgb(255 47 212 / 0.6), 0 0 40px rgb(34 211 238 / 0.4)", // a double glow (ticket 03)

	DurationFast: "150ms", DurationNormal: "300ms", DurationSlow: "500ms",
	EasingStandard: "cubic-bezier(0.4, 0, 0.2, 1)",
	EasingEmphasis: "cubic-bezier(0.68, -0.55, 0.27, 1.55)", // bouncy, with overshoot (ticket 03)

	Sounds: map[SoundCue]ToneRecipe{
		CueAnswerSubmitted: {Waveform: "sawtooth", Hz: 440, Seconds: 0.15},
		CueInterviewDone:   {Waveform: "triangle", Hz: 660, Seconds: 0.3},
		CueYourMatch:       {Waveform: "sawtooth", Hz: 220, Seconds: 0.5},
		CueBigScreenMatch:  {Waveform: "triangle", Hz: 523.25, Seconds: 0.4},
		CueBigScreenReveal: {Waveform: "sawtooth", Hz: 110, Seconds: 0.8},
	},
}

var paperTheme = Theme{
	Key: "paper", Name: "Paper",

	SurfacePage:    "#faf7f0", // cream
	SurfaceRaised:  "#ffffff",
	SurfaceOverlay: "rgb(0 0 0 / 0.12)",
	TextNormal:     "#1c1917", // stone-900
	TextMuted:      "#78716c", // stone-500
	Accent:         "#4338ca", // indigo-700: dark enough to read on cream and white
	TextOnAccent:   "#ffffff",
	Border:         "#e7e5e4", // stone-200
	StatePositive:  "#15803d", // green-700
	StateNegative:  "#b91c1c", // red-700
	StateWarning:   "#a16207", // yellow-700
	HighlightFrom:  "#f59e0b", // amber-500
	HighlightTo:    "#ec4899", // pink-500
	TextOnPersona:  "#1c1917",
	// A light Theme: Persona colours (all Tailwind *-400s) are below 3:1 on
	// cream and white, so the outline itself must pass 3:1 instead (ticket 03).
	PersonaOutline: "#1c1917",

	FontDisplay:       "Georgia, 'Times New Roman', serif", // Paper's one serif "web font": a system serif, no file to vendor
	FontBody:          "ui-sans-serif, system-ui, sans-serif",
	FontMono:          "ui-monospace, SFMono-Regular, monospace",
	FontDisplayWeight: "600",
	WebFontURL:        "",

	RadiusSm: "0.25rem", RadiusMd: "0.375rem", RadiusLg: "0.625rem", RadiusPill: "9999px",
	BorderWidth: "1px",
	Glow:        "0 1px 3px rgb(28 25 23 / 0.08), 0 1px 2px rgb(28 25 23 / 0.06)", // soft shadows (ticket 03)

	DurationFast: "150ms", DurationNormal: "250ms", DurationSlow: "450ms", // gentle motion (ticket 03)
	EasingStandard: "cubic-bezier(0.4, 0, 0.2, 1)",
	EasingEmphasis: "cubic-bezier(0.16, 1, 0.3, 1)", // smooth deceleration, no overshoot

	Sounds: map[SoundCue]ToneRecipe{
		CueAnswerSubmitted: {Waveform: "sine", Hz: 523.25, Seconds: 0.2},
		CueInterviewDone:   {Waveform: "sine", Hz: 659.25, Seconds: 0.3},
		CueYourMatch:       {Waveform: "sine", Hz: 784, Seconds: 0.6},
		CueBigScreenMatch:  {Waveform: "sine", Hz: 659.25, Seconds: 0.4},
		CueBigScreenReveal: {Waveform: "sine", Hz: 880, Seconds: 0.8},
	},
}
