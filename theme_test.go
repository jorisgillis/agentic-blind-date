package main

import (
	"os"
	"strings"
	"testing"
)

func TestThemeByKey_FallsBackToDefaultForUnknownOrBlankKey(t *testing.T) {
	for _, key := range []string{"", "nonsense", "blocked-by-a-browser-setting"} {
		got := ThemeByKey(key)
		if got.Key != DefaultThemeKey {
			t.Errorf("ThemeByKey(%q): want the default Theme, got %q", key, got.Key)
		}
	}
}

// TestThemes_HoldsExactlyThreeThemes covers #41: the catalogue holds
// exactly three Themes, Classic, Neon and Paper, with Classic the default.
func TestThemes_HoldsExactlyThreeThemes(t *testing.T) {
	if len(Themes) != 3 {
		t.Fatalf("want 3 Themes, got %d: %+v", len(Themes), Themes)
	}
	want := []string{"classic", "neon", "paper"}
	for i, key := range want {
		if Themes[i].Key != key {
			t.Errorf("Themes[%d]: want %q, got %q", i, key, Themes[i].Key)
		}
	}
	if Themes[0].Key != DefaultThemeKey {
		t.Errorf("want Classic first and default, got %q as the default", DefaultThemeKey)
	}
}

// TestThemes_EveryThemeDefinesEveryToken covers #40/#41: no Theme leaves a
// token blank. WebFontURL may be "" (a Theme need not add a web font), and
// Sounds may be empty (a Theme may leave every cue silent) — every other
// field must be set.
func TestThemes_EveryThemeDefinesEveryToken(t *testing.T) {
	for _, th := range Themes {
		t.Run(th.Name, func(t *testing.T) {
			fields := map[string]string{
				"SurfacePage": th.SurfacePage, "SurfaceRaised": th.SurfaceRaised, "SurfaceOverlay": th.SurfaceOverlay,
				"TextNormal": th.TextNormal, "TextMuted": th.TextMuted,
				"Accent": th.Accent, "TextOnAccent": th.TextOnAccent,
				"Border": th.Border,
				"StatePositive": th.StatePositive, "StateNegative": th.StateNegative, "StateWarning": th.StateWarning,
				"HighlightFrom": th.HighlightFrom, "HighlightTo": th.HighlightTo,
				"TextOnPersona": th.TextOnPersona, "PersonaOutline": th.PersonaOutline,
				"FontDisplay": th.FontDisplay, "FontBody": th.FontBody, "FontMono": th.FontMono, "FontDisplayWeight": th.FontDisplayWeight,
				"RadiusSm": th.RadiusSm, "RadiusMd": th.RadiusMd, "RadiusLg": th.RadiusLg, "RadiusPill": th.RadiusPill,
				"BorderWidth": th.BorderWidth, "Glow": th.Glow,
				"DurationFast": th.DurationFast, "DurationNormal": th.DurationNormal, "DurationSlow": th.DurationSlow,
				"EasingStandard": th.EasingStandard, "EasingEmphasis": th.EasingEmphasis,
			}
			for name, v := range fields {
				if v == "" {
					t.Errorf("%s.%s is blank", th.Name, name)
				}
			}
		})
	}
}

// TestPersonaSource_NeverMentionsThemes covers #41: a Persona's Tailwind
// colour class (e.g. bg-purple-400) must render identically under every
// Theme. persona.go is the one source of Persona colours (CONTEXT.md), so
// that holds as long as it has no structural dependency on the Theme
// catalogue at all — checked here at the source level, since the colour
// values themselves can coincidentally collide (Persona red and a Theme's
// state-negative both happen to be Tailwind's red-400).
func TestPersonaSource_NeverMentionsThemes(t *testing.T) {
	src, err := os.ReadFile("persona.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(src)), "theme") {
		t.Error("persona.go should have no dependency on the Theme catalogue")
	}
}

func TestThemeByKey_FindsAKnownTheme(t *testing.T) {
	got := ThemeByKey("classic")
	if got.Name != "Classic" {
		t.Errorf("want Classic, got %+v", got)
	}
}

func TestThemeStyleBlock_RendersEveryThemesDataThemeSelector(t *testing.T) {
	css := string(themeStyleBlock())
	if !strings.Contains(css, "@theme inline") {
		t.Error("want the @theme inline mapping block")
	}
	for _, theme := range Themes {
		if !strings.Contains(css, `[data-theme="`+theme.Key+`"]`) {
			t.Errorf("want a [data-theme=%q] block for %s", theme.Key, theme.Name)
		}
		if !strings.Contains(css, theme.SurfacePage) {
			t.Errorf("want %s's surface-page value %q in the generated CSS", theme.Name, theme.SurfacePage)
		}
	}
}

func TestContrastRatio_KnownValues(t *testing.T) {
	cases := []struct {
		a, b string
		want float64
	}{
		{"#000000", "#ffffff", 21},
		{"#ffffff", "#ffffff", 1},
	}
	for _, c := range cases {
		got, err := contrastRatio(c.a, c.b)
		if err != nil {
			t.Fatal(err)
		}
		if diff := got - c.want; diff > 0.01 || diff < -0.01 {
			t.Errorf("contrastRatio(%s, %s): want %.2f, got %.2f", c.a, c.b, c.want, got)
		}
	}
}

func TestContrastRatio_RejectsAMalformedColour(t *testing.T) {
	if _, err := contrastRatio("not-a-colour", "#ffffff"); err == nil {
		t.Error("want an error for the first colour")
	}
	if _, err := contrastRatio("#ffffff", "not-a-colour"); err == nil {
		t.Error("want an error for the second colour")
	}
}

func TestParseHex_RejectsAMalformedChannel(t *testing.T) {
	cases := []string{"#ff", "#gggggg", "#ffgggg", "#ffffgg"}
	for _, c := range cases {
		if _, _, _, err := parseHex(c); err == nil {
			t.Errorf("parseHex(%q): want an error", c)
		}
	}
}

func TestPersonaContrastOK(t *testing.T) {
	cases := []struct {
		name string
		t    Theme
		want bool
	}{
		{
			name: "every colour passes directly",
			t:    Theme{SurfacePage: "#000000", SurfaceRaised: "#000000", PersonaOutline: "transparent"},
			want: true,
		},
		{
			name: "a colour fails and there is no outline",
			t:    Theme{SurfacePage: "#fbbf24", SurfaceRaised: "#fbbf24", PersonaOutline: "transparent"},
			want: false, // amber against amber: 1:1, and no outline to rescue it
		},
		{
			name: "a colour fails but a readable outline rescues it",
			t:    Theme{SurfacePage: "#fbbf24", SurfaceRaised: "#fbbf24", PersonaOutline: "#000000"},
			want: true,
		},
		{
			name: "a colour fails and the outline itself doesn't pass",
			t:    Theme{SurfacePage: "#fbbf24", SurfaceRaised: "#fbbf24", PersonaOutline: "#fde68a"},
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := personaContrastOK(c.t)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Errorf("want %v, got %v", c.want, got)
			}
		})
	}
}

func TestPersonaContrastOK_RejectsAMalformedSurface(t *testing.T) {
	if _, err := personaContrastOK(Theme{SurfacePage: "not-a-colour", SurfaceRaised: "not-a-colour"}); err == nil {
		t.Error("want an error for a malformed surface")
	}
}

func TestPersonaContrastOK_RejectsAMalformedOutline(t *testing.T) {
	// A low-contrast surface forces the outline check, so a malformed
	// outline's error surfaces too.
	_, err := personaContrastOK(Theme{SurfacePage: "#fbbf24", SurfaceRaised: "#fbbf24", PersonaOutline: "not-a-colour"})
	if err == nil {
		t.Error("want an error for a malformed outline")
	}
}

// TestPersonaContrastOK_EveryThemeKeepsThePersonaPaletteReadable covers
// ticket 02's readability decision: every Theme keeps at least a 3:1
// contrast ratio between each Persona colour and its surfaces, either
// directly or via a Persona outline that itself passes (ticket 03).
func TestPersonaContrastOK_EveryThemeKeepsThePersonaPaletteReadable(t *testing.T) {
	for _, theme := range Themes {
		t.Run(theme.Name, func(t *testing.T) {
			ok, err := personaContrastOK(theme)
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				t.Errorf("%s does not keep every Persona colour readable (3:1) on its surfaces", theme.Name)
			}
		})
	}
}
