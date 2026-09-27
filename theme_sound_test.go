package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestThemeSoundsJSON_RoundTrips(t *testing.T) {
	var byTheme map[string]map[string]ToneRecipe
	if err := json.Unmarshal([]byte(themeSoundsJSON()), &byTheme); err != nil {
		t.Fatal(err)
	}
	if len(byTheme["classic"]) != 0 {
		t.Errorf("Classic should have no sound cues, got %+v", byTheme["classic"])
	}
	for _, key := range []string{"neon", "paper"} {
		cues := byTheme[key]
		for _, cue := range []string{
			"answer_submitted", "interview_done", "your_match", "bigscreen_match", "bigscreen_reveal",
			"round_opens", "round_closes", "countdown_last_3", "matchmaking_drumroll", "pair_revealed",
		} {
			recipe, ok := cues[cue]
			if !ok {
				t.Errorf("%s: missing the %q cue", key, cue)
				continue
			}
			if recipe.Waveform == "" || recipe.Hz <= 0 || recipe.Seconds <= 0 {
				t.Errorf("%s %q: incomplete recipe %+v", key, cue, recipe)
			}
		}
	}
}

// TestGameShowMoments_HaveAnOptionalSoundCue covers #85: each of the Game
// Show's five moments plays its own cue — silently, wherever the current
// Theme leaves it out of its Sounds map (already covered generically by the
// shared sound module, #44).
func TestGameShowMoments_HaveAnOptionalSoundCue(t *testing.T) {
	fragments := readFile(t, "templates/fragments.html")
	if !strings.Contains(fragments, "agenticBlindDateSound.play('round_opens')") {
		t.Error("a Round opening should have an optional sound cue")
	}
	if !strings.Contains(fragments, "agenticBlindDateSound.play('countdown_last_3')") {
		t.Error("the countdown's last 3 seconds should have an optional sound cue")
	}

	screen := readFile(t, "templates/screen.html")
	if !strings.Contains(screen, "playBigScreenCue('round_closes')") {
		t.Error("a Round closing (the callout) should have an optional sound cue")
	}
	if !strings.Contains(screen, "playBigScreenCue('matchmaking_drumroll')") {
		t.Error("the matchmaking drumroll should have an optional sound cue")
	}
	if !strings.Contains(screen, "playBigScreenCue('pair_revealed')") {
		t.Error("a pair revealed should have an optional sound cue")
	}
}

// TestEveryPage_HasTheSoundModule covers #44: every Participant page (and
// admin/data, "like any phone") gets the phone's opt-in sound module and its
// toggle; the Big Screen gets its own "enable sound" control instead, never
// the Participant's.
func TestEveryPage_HasTheSoundModule(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")

	for _, path := range []string{"/user", "/user/wait/a", "/admin", "/data"} {
		body := readBody(t, get(t, srv, path))
		if !strings.Contains(body, "agenticBlindDateSound") {
			t.Errorf("%s: missing the sound module", path)
		}
		if !strings.Contains(body, `id="abd-sound-toggle"`) {
			t.Errorf("%s: missing the sound toggle", path)
		}
	}

	body := readBody(t, get(t, srv, "/bigscreen"))
	if strings.Contains(body, "agenticBlindDateSound") {
		t.Error("the Big Screen should not run the Participant's sound module")
	}
	if !strings.Contains(body, "enableBigScreenSound") || !strings.Contains(body, "Enable sound") {
		t.Error("the Big Screen should show its own enable-sound control")
	}
}

// TestPhonePages_RespectTheSilentSwitchUnlessPlayThroughIsChosen covers #44:
// sound defaults to iOS's "ambient" audio session, which respects the
// silent switch, and a Participant can explicitly opt into playing through
// it instead (navigator.audioSession, feature-detected for browsers that
// don't implement it).
func TestPhonePages_RespectTheSilentSwitchUnlessPlayThroughIsChosen(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")

	body := readBody(t, get(t, srv, "/user/wait/a"))
	for _, want := range []string{
		"audioSession",
		"'playback'",
		"'ambient'",
		"abd-sound-playthrough-toggle",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q: the silent-switch play-through option isn't wired up", want)
		}
	}
}
