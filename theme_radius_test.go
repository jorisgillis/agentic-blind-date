package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestTemplates_EveryRoundedClassIsThemeMapped is a regression guard for a
// #40 review finding: templates use Tailwind's own rounded-{lg,xl,2xl,3xl,
// full} scale, not just the 4 names that happen to match a token 1:1, so
// every rounded-* class actually used must have a themeVarMappings entry —
// otherwise that corner stays Tailwind's fixed, unthemed default regardless
// of Theme.
func TestTemplates_EveryRoundedClassIsThemeMapped(t *testing.T) {
	mapped := map[string]bool{}
	for _, m := range themeVarMappings {
		if strings.HasPrefix(m.tailwind, "radius-") {
			mapped[strings.TrimPrefix(m.tailwind, "radius-")] = true
		}
	}

	re := regexp.MustCompile(`\brounded-(sm|md|lg|xl|2xl|3xl|full)\b`)
	files, err := filepath.Glob("templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			seen[m[1]] = true
		}
	}
	if len(seen) == 0 {
		t.Fatal("no rounded-* class found in templates: is the glob or regex wrong?")
	}
	for suffix := range seen {
		if !mapped[suffix] {
			t.Errorf("templates use rounded-%s, but no themeVarMappings entry themes it", suffix)
		}
	}
}
