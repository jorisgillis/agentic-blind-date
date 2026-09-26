package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// contrastRatio is the WCAG 2.0 contrast ratio between two colours, each a
// "#rrggbb" hex string. Used to check every Theme keeps the Persona palette
// readable (ticket 02, ticket 03's amendment for outlines).
func contrastRatio(hexA, hexB string) (float64, error) {
	la, err := relativeLuminance(hexA)
	if err != nil {
		return 0, err
	}
	lb, err := relativeLuminance(hexB)
	if err != nil {
		return 0, err
	}
	lighter, darker := la, lb
	if lighter < darker {
		lighter, darker = darker, lighter
	}
	return (lighter + 0.05) / (darker + 0.05), nil
}

func relativeLuminance(hex string) (float64, error) {
	r, g, b, err := parseHex(hex)
	if err != nil {
		return 0, err
	}
	lin := func(c float64) float64 {
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b), nil
}

func parseHex(hex string) (r, g, b float64, err error) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 0, 0, 0, fmt.Errorf("not a #rrggbb colour: %q", hex)
	}
	channel := func(s string) (float64, error) {
		n, err := strconv.ParseUint(s, 16, 8)
		if err != nil {
			return 0, err
		}
		return float64(n) / 255, nil
	}
	if r, err = channel(hex[0:2]); err != nil {
		return 0, 0, 0, err
	}
	if g, err = channel(hex[2:4]); err != nil {
		return 0, 0, 0, err
	}
	if b, err = channel(hex[4:6]); err != nil {
		return 0, 0, 0, err
	}
	return r, g, b, nil
}

// personaContrastOK reports whether every Persona colour reads at at least a
// 3:1 contrast ratio against t's surfaces — directly if the colour itself
// passes, or via a non-transparent Persona outline that itself passes
// (ticket 03: light Themes draw the outline instead of relying on the
// Persona colour, which stays unchanged).
func personaContrastOK(t Theme) (bool, error) {
	for _, p := range personaPalette {
		for _, surface := range []string{t.SurfacePage, t.SurfaceRaised} {
			ratio, err := contrastRatio(p.Hex, surface)
			if err != nil {
				return false, err
			}
			if ratio >= 3.0 {
				continue
			}
			if t.PersonaOutline == "transparent" {
				return false, nil
			}
			outlineRatio, err := contrastRatio(t.PersonaOutline, surface)
			if err != nil {
				return false, err
			}
			if outlineRatio < 3.0 {
				return false, nil
			}
		}
	}
	return true, nil
}
