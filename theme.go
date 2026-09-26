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
var Themes = []Theme{classicTheme}

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
