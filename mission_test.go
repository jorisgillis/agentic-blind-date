package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// missionLLM scripts the Mission Agent to answer with one numbered reason per
// tile in the prompt, so tests can assert the reasons land in grid order.
func missionLLM() *fakeLLM {
	return newFakeLLM().onFunc("Mission Agent", func(user string) (string, error) {
		return reasonsReply(strings.Count(user, ". ["))
	})
}

// reasonsReply builds a Mission Agent reply with n numbered reasons.
func reasonsReply(n int) (string, error) {
	reasons := make([]string, n)
	for i := range reasons {
		reasons[i] = fmt.Sprintf("Reason %d", i+1)
	}
	encoded, _ := json.Marshal(map[string][]string{"reasons": reasons})
	return string(encoded), nil
}

// seedReady seeds n ready Participants with ids like p00, p01, ...
func seedReady(t *testing.T, db *DB, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		seed(t, db, fmt.Sprintf("p%02d", i), fmt.Sprintf("The Persona %02d", i), "ready")
	}
}

// cardCount counts the stored Mission cards.
func cardCount(t *testing.T, db *DB) int {
	t.Helper()
	var n int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM mission_cards`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// centerTile returns the card's center tile (position 4), or nil.
func centerTile(card *MissionCard) *MissionTile {
	for i := range card.Tiles {
		if card.Tiles[i].Position == centerPosition {
			return &card.Tiles[i]
		}
	}
	return nil
}

// tileAt returns the card's tile at the given position, or nil.
func tileAt(card *MissionCard, position int) *MissionTile {
	for i := range card.Tiles {
		if card.Tiles[i].Position == position {
			return &card.Tiles[i]
		}
	}
	return nil
}

// personIDs lists the tiles' Person ids.
func personIDs(tiles []MissionTile) []string {
	ids := make([]string, 0, len(tiles))
	for _, t := range tiles {
		ids = append(ids, t.PersonID)
	}
	return ids
}

// ── tile selection: the pure logic ─────────────────────────────────────────

// stubScores scores every candidate by a fixed per-id value.
func stubScores(scores map[string]int) func(a, b *Participant) int {
	return func(_, b *Participant) int { return scores[b.ID] }
}

func TestSelectTiles_TheMatchIsTheCenterAndExcludedFromTheSurrounding(t *testing.T) {
	me := testParticipant("me", "The Me", []string{"Go"})
	me.MatchedWith = "m"
	match := testParticipant("m", "The Match", []string{"Go"})
	others := []*Participant{match, me}
	scores := map[string]int{}
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("p%02d", i)
		scores[id] = 100 - i
		others = append(others, testParticipant(id, "The "+id, []string{"Go"}))
	}

	tiles := selectTiles(me, others, nil, stubScores(scores))

	if len(tiles) != 9 {
		t.Fatalf("want 9 tiles (center + 8 surrounding), got %d", len(tiles))
	}
	center := centerTile(&MissionCard{Tiles: tiles})
	if center == nil || center.PersonID != "m" || center.Kind != TileMatch {
		t.Fatalf("the center should be the match, got %+v", center)
	}
	if center.Reason != "Your match — find them in the room!" {
		t.Errorf("center reason: got %q", center.Reason)
	}

	// The surrounding tiles: positions 0-3 and 5-8, 4 affinity picks then 4
	// wildcards, no duplicates, neither the match nor me among them.
	wantPositions := []int{0, 1, 2, 3, 5, 6, 7, 8}
	seen := map[string]bool{}
	for i, position := range wantPositions {
		tile := tileAt(&MissionCard{Tiles: tiles}, position)
		if tile == nil {
			t.Fatalf("no tile at position %d", position)
		}
		wantKind := TileAffinity
		if i >= 4 {
			wantKind = TileWildcard
		}
		if tile.Kind != wantKind {
			t.Errorf("position %d: want kind %s, got %s", position, wantKind, tile.Kind)
		}
		if seen[tile.PersonID] {
			t.Errorf("person %s appears twice on the card", tile.PersonID)
		}
		seen[tile.PersonID] = true
		if tile.PersonID == "m" || tile.PersonID == "me" {
			t.Errorf("position %d: %s must not be on the card", position, tile.PersonID)
		}
	}
}

func TestSelectTiles_AffinityPicksTheHighestScoresAndWildcardsTheMostDifferent(t *testing.T) {
	me := testParticipant("me", "The Me", []string{"Go"}, participantOpts{
		answers: map[string]string{"q1": "tabs", "q2": "yes"},
	})
	me.MatchedWith = "m"
	others := []*Participant{testParticipant("m", "The Match", []string{"Go"})}

	// Eight candidates. The stub score makes c1-c4 the affinity picks; the
	// difference score (languages + opposing answers) makes c5, c7, c8, c6
	// the wildcard picks, in that order.
	candidates := []struct {
		id    string
		langs []string
		ans   map[string]string
	}{
		{"c1", []string{"Go"}, map[string]string{"q1": "tabs", "q2": "yes"}},
		{"c2", []string{"Go"}, map[string]string{"q1": "tabs", "q2": "yes"}},
		{"c3", []string{"Go"}, map[string]string{"q1": "tabs", "q2": "yes"}},
		{"c4", []string{"Go"}, map[string]string{"q1": "tabs", "q2": "yes"}},
		{"c5", []string{"Rust", "Python"}, map[string]string{"q1": "spaces", "q2": "no"}}, // 3 langs + 2 answers = 5
		{"c6", []string{"Go"}, map[string]string{"q1": "tabs", "q2": "no"}},               // 0 langs + 1 answer = 1
		{"c7", []string{"Rust"}, map[string]string{"q1": "tabs"}},                         // 2 langs + 0 answers = 2
		{"c8", []string{"Go", "Rust"}, map[string]string{"q1": "spaces"}},                 // 1 lang + 1 answer = 2
	}
	scores := map[string]int{}
	for i, c := range candidates {
		scores[c.id] = 100 - i*10 // c1 > c2 > ... > c8
		others = append(others, testParticipant(c.id, "The "+c.id, c.langs, participantOpts{answers: c.ans}))
	}

	tiles := selectTiles(me, others, nil, stubScores(scores))

	wantAffinity := []string{"c1", "c2", "c3", "c4"}
	for i, id := range wantAffinity {
		if tile := tileAt(&MissionCard{Tiles: tiles}, i); tile == nil || tile.PersonID != id {
			t.Errorf("affinity pick %d: want %s, got %+v", i, id, tile)
		}
	}
	wantWildcards := []string{"c5", "c7", "c8", "c6"} // c7 and c8 tie at 2, ordered by id
	for i, id := range wantWildcards {
		if tile := tileAt(&MissionCard{Tiles: tiles}, 5+i); tile == nil || tile.PersonID != id {
			t.Errorf("wildcard pick %d: want %s, got %+v", i, id, tile)
		}
	}
}

func TestSelectTiles_UnmatchedFallsBackToTheTopAffinityPick(t *testing.T) {
	me := testParticipant("me", "The Me", []string{"Go"})
	others := []*Participant{
		testParticipant("zz", "The Zz", []string{"Go"}),
		testParticipant("aa", "The Aa", []string{"Go"}),
	}
	scores := map[string]int{"zz": 1, "aa": 99}

	tiles := selectTiles(me, others, nil, stubScores(scores))

	center := centerTile(&MissionCard{Tiles: tiles})
	if center == nil || center.PersonID != "aa" || center.Kind != TileAffinity {
		t.Fatalf("the center should fall back to the top-affinity pick, got %+v", center)
	}
	if center.Reason != "Your best fit — start here" {
		t.Errorf("center reason: got %q", center.Reason)
	}
	// The center person is not repeated among the surrounding tiles.
	if tile := tileAt(&MissionCard{Tiles: tiles}, 0); tile == nil || tile.PersonID != "zz" {
		t.Errorf("the remaining candidate should be the only surrounding tile, got %+v", tile)
	}
	if len(tiles) != 2 {
		t.Errorf("want 2 tiles, got %d", len(tiles))
	}
}

func TestSelectTiles_TheAlreadyMetAndOneselfAreExcluded(t *testing.T) {
	me := testParticipant("me", "The Me", []string{"Go"})
	others := []*Participant{
		me,
		testParticipant("met", "The Met", []string{"Go"}),
		testParticipant("c1", "The C1", []string{"Go"}),
		testParticipant("c2", "The C2", []string{"Go"}),
	}
	scores := map[string]int{"met": 1000, "c1": 3, "c2": 2, "me": 1000}

	tiles := selectTiles(me, others, map[string]bool{"met": true}, stubScores(scores))

	for _, id := range personIDs(tiles) {
		if id == "me" || id == "met" {
			t.Errorf("%s must not be on the card, tiles: %v", id, personIDs(tiles))
		}
	}
}

func TestSelectTiles_TheCardIsDeterministicWhateverThePoolOrder(t *testing.T) {
	me := testParticipant("me", "The Me", []string{"Go"})
	var others []*Participant
	scores := map[string]int{}
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("p%02d", i)
		scores[id] = i % 3 // plenty of ties, broken by id
		others = append(others, testParticipant(id, "The "+id, []string{"Go"}))
	}
	reversed := make([]*Participant, len(others))
	for i, p := range others {
		reversed[len(others)-1-i] = p
	}

	first, _ := json.Marshal(selectTiles(me, others, nil, stubScores(scores)))
	second, _ := json.Marshal(selectTiles(me, reversed, nil, stubScores(scores)))
	if string(first) != string(second) {
		t.Errorf("the card must not depend on the pool's order:\n%s\n%s", first, second)
	}

	// No person appears twice.
	seen := map[string]bool{}
	for _, id := range personIDs(selectTiles(me, others, nil, stubScores(scores))) {
		if seen[id] {
			t.Errorf("person %s appears twice", id)
		}
		seen[id] = true
	}
}

func TestSelectTiles_FewerCandidatesThanTiles(t *testing.T) {
	me := testParticipant("me", "The Me", []string{"Go"})
	others := []*Participant{
		testParticipant("c1", "The C1", []string{"Go"}),
		testParticipant("c2", "The C2", []string{"Go"}),
	}
	scores := map[string]int{"c1": 2, "c2": 1}

	tiles := selectTiles(me, others, nil, stubScores(scores))

	if len(tiles) != 2 { // the center (c1) plus one affinity pick (c2)
		t.Fatalf("want 2 tiles, got %d", len(tiles))
	}
	if tile := tileAt(&MissionCard{Tiles: tiles}, 4); tile == nil || tile.PersonID != "c1" {
		t.Errorf("center: want c1, got %+v", tile)
	}
	if tile := tileAt(&MissionCard{Tiles: tiles}, 0); tile == nil || tile.PersonID != "c2" {
		t.Errorf("position 0: want c2, got %+v", tile)
	}
}

func TestSelectTiles_NoCandidatesGivesNoTiles(t *testing.T) {
	me := testParticipant("me", "The Me", []string{"Go"})

	if tiles := selectTiles(me, []*Participant{me}, nil, stubScores(nil)); len(tiles) != 0 {
		t.Errorf("want no tiles, got %v", personIDs(tiles))
	}
}

func TestSelectTiles_AMatchMissingFromThePoolFallsBackToAffinity(t *testing.T) {
	me := testParticipant("me", "The Me", []string{"Go"})
	me.MatchedWith = "gone" // the partner left; the card falls back
	others := []*Participant{
		testParticipant("c1", "The C1", []string{"Go"}),
		testParticipant("c2", "The C2", []string{"Go"}),
	}
	scores := map[string]int{"c1": 5, "c2": 4}

	tiles := selectTiles(me, others, nil, stubScores(scores))

	center := centerTile(&MissionCard{Tiles: tiles})
	if center == nil || center.PersonID != "c1" || center.Kind != TileAffinity {
		t.Errorf("the center should fall back to the top-affinity pick, got %+v", center)
	}
}

func TestDifferenceScore(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b *Participant
		want int
	}{
		{
			"identical profiles and answers",
			testParticipant("a", "A", []string{"Go"}, participantOpts{answers: map[string]string{"q1": "x"}}),
			testParticipant("b", "B", []string{"Go"}, participantOpts{answers: map[string]string{"q1": "x"}}),
			0,
		},
		{
			"languages on both sides",
			testParticipant("a", "A", []string{"Go", "Rust"}),
			testParticipant("b", "B", []string{"Rust", "Python"}),
			2, // Go only on a's side, Python only on b's side
		},
		{
			"opposing answers",
			testParticipant("a", "A", nil, participantOpts{answers: map[string]string{"q1": "x", "q2": "y"}}),
			testParticipant("b", "B", nil, participantOpts{answers: map[string]string{"q1": "z", "q2": "y", "q3": "w"}}),
			1, // only q1 differs; q3 is unanswered on a's side
		},
		{
			"free-text and custom answers are not compared",
			testParticipant("a", "A", nil, participantOpts{answers: map[string]string{"fixed_1": "Go", "custom_0": "x", "fixed_0": "Tabs"}}),
			testParticipant("b", "B", nil, participantOpts{answers: map[string]string{"fixed_1": "Rust", "custom_0": "y", "fixed_0": "Spaces"}}),
			1, // only the single-choice fixed_0 differs
		},
		{
			"free-text and custom answers are not compared",
			testParticipant("a", "A", nil, participantOpts{answers: map[string]string{"fixed_1": "Go", "custom_0": "x", "fixed_0": "Tabs"}}),
			testParticipant("b", "B", nil, participantOpts{answers: map[string]string{"fixed_1": "Rust", "custom_0": "y", "fixed_0": "Spaces"}}),
			1, // only the single-choice fixed_0 differs
		},
		{
			"no profiles at all",
			&Participant{ID: "a"},
			&Participant{ID: "b"},
			0,
		},
		{
			"no answers at all",
			testParticipant("a", "A", []string{"Go"}),
			testParticipant("b", "B", []string{"Go"}),
			0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := differenceScore(tc.a, tc.b); got != tc.want {
				t.Errorf("differenceScore: want %d, got %d", tc.want, got)
			}
		})
	}
}

// ── the Mission Agent: one batched call, generic fallback ──────────────────

func TestMissionAgent_OneBatchedCallGeneratesEveryReason(t *testing.T) {
	llm := missionLLM()
	agent := NewMissionAgent(llm)
	me := testParticipant("me", "The Me", []string{"Go"})
	me.PersonaSymbol = "🦊"
	others := []*Participant{testParticipant("c1", "The C1", []string{"Go"}), testParticipant("c2", "The C2", []string{"Rust"})}
	others[0].PersonaSymbol, others[1].PersonaSymbol = "🦁", "🐯"
	tiles := []MissionTile{
		newTile(0, TileAffinity, others[0], ""),
		newTile(1, TileWildcard, others[1], ""),
	}

	reasons := agent.Reasons(me, tiles, others)

	if len(reasons) != 2 || reasons[0] != "Reason 1" || reasons[1] != "Reason 2" {
		t.Errorf("reasons: got %v", reasons)
	}
	if n := llm.callsMatching("Mission Agent"); n != 1 {
		t.Errorf("want one batched call for all reasons, got %d", n)
	}
	call, _ := llm.lastCallMatching("Mission Agent")
	for _, want := range []string{"The Me", "The C1", "The C2", "[affinity]", "[wildcard]"} {
		if !strings.Contains(call.User, want) {
			t.Errorf("the prompt should contain %q, got:\n%s", want, call.User)
		}
	}
	if strings.Contains(call.User, "@me") || strings.Contains(call.User, "@c1") {
		t.Errorf("the prompt must not carry handles:\n%s", call.User)
	}
}

func TestMissionAgent_AnLLMFailureYieldsTheGenericReason(t *testing.T) {
	agent := NewMissionAgent(newFakeLLM().onErr("Mission Agent", errUnscripted))
	me := testParticipant("me", "The Me", nil)
	tiles := []MissionTile{newTile(0, TileAffinity, testParticipant("c1", "The C1", nil), "")}

	reasons := agent.Reasons(me, tiles, nil)

	if len(reasons) != 1 || reasons[0] != genericMissionReason {
		t.Errorf("want the generic reason, got %v", reasons)
	}
}

func TestMissionAgent_AReplyWithoutOneReasonPerTileYieldsTheGenericReason(t *testing.T) {
	for _, reply := range []string{
		`{"reasons": ["only one"]}`,    // too few
		`{"reasons": ["a", "b", "c"]}`, // too many
		`{"reasons": ["ok", ""]}`,      // an empty reason
		`{"reasons": ["ok", "   "]}`,   // a blank reason
		`{"reasons": null}`,            // no reasons at all
		`I cannot help with that`,      // not JSON
	} {
		agent := NewMissionAgent(newFakeLLM().on("Mission Agent", reply))
		tiles := []MissionTile{
			newTile(0, TileAffinity, testParticipant("c1", "The C1", nil), ""),
			newTile(1, TileWildcard, testParticipant("c2", "The C2", nil), ""),
		}
		reasons := agent.Reasons(testParticipant("me", "The Me", nil), tiles, nil)
		if len(reasons) != 2 || reasons[0] != genericMissionReason || reasons[1] != genericMissionReason {
			t.Errorf("reply %s: want the generic reason for every tile, got %v", reply, reasons)
		}
	}
}

func TestMissionAgent_TheReplyIsTrimmedAndToleratesMarkdownFences(t *testing.T) {
	reply := "```json\n" + `{"reasons": ["  first  ", "second"]}` + "\n```"
	agent := NewMissionAgent(newFakeLLM().on("Mission Agent", reply))
	tiles := []MissionTile{
		newTile(0, TileAffinity, testParticipant("c1", "The C1", nil), ""),
		newTile(1, TileWildcard, testParticipant("c2", "The C2", nil), ""),
	}

	reasons := agent.Reasons(testParticipant("me", "The Me", nil), tiles, nil)

	if len(reasons) != 2 || reasons[0] != "first" || reasons[1] != "second" {
		t.Errorf("want the trimmed reasons, got %v", reasons)
	}
}

func TestMissionPrompt_ATileWhosePersonIsGoneStillRenders(t *testing.T) {
	// The pool is read at generation, so every tile's person is in it; a tile
	// naming someone else (the pool changed between selection and prompt) is
	// rendered without their profile context rather than dropped.
	me := testParticipant("me", "The Me", nil)
	ghost := newTile(0, TileWildcard, testParticipant("ghost", "The Ghost", nil), "")

	prompt := missionPrompt(me, []MissionTile{ghost}, nil)

	if !strings.Contains(prompt, "The Ghost") || !strings.Contains(prompt, "[wildcard]") {
		t.Errorf("the tile should still be described, got:\n%s", prompt)
	}
}

// ── generation: timing, idempotence, failures ──────────────────────────────

func TestMissions_NoCardWhenTheEventDoesNotAllowThem(t *testing.T) {
	srv, deps := newTestServer(t, missionLLM(), nil)
	seedReady(t, deps.db, 3)

	// Onboarding: no card.
	if card, err := deps.missions.GenerateFor("p00"); card != nil || err != nil {
		t.Errorf("during onboarding: want no card, got %+v (err %v)", card, err)
	}
	if deps.missions.Has("p00") {
		t.Error("during onboarding: no card should be stored")
	}

	// Closed: no card, even jumping straight from onboarding.
	if err := deps.events.Advance(PhaseClosed); err != nil {
		t.Fatal(err)
	}
	if card, err := deps.missions.GenerateFor("p00"); card != nil || err != nil {
		t.Errorf("once closed: want no card, got %+v (err %v)", card, err)
	}
	if deps.missions.Has("p00") {
		t.Error("once closed: no card should be stored")
	}
	_ = srv
}

func TestMissions_GenerateForGeneratesTheCard(t *testing.T) {
	_, deps := newTestServer(t, missionLLM(), nil)
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	seed(t, deps.db, "me", "The Me", "ready")
	seedReady(t, deps.db, 9)

	card, err := deps.missions.GenerateFor("me")
	if err != nil {
		t.Fatal(err)
	}
	if card == nil {
		t.Fatal("want a card")
	}
	if len(card.Tiles) != 9 {
		t.Fatalf("want 9 tiles, got %d", len(card.Tiles))
	}
	if center := centerTile(card); center == nil || center.Kind != TileAffinity || center.PersonID != "p00" {
		t.Errorf("unmatched: the center should be the top-affinity pick, got %+v", center)
	}
	// The 8 surrounding tiles carry the Mission Agent's reasons in grid order.
	for i, position := range []int{0, 1, 2, 3, 5, 6, 7, 8} {
		tile := tileAt(card, position)
		if tile == nil {
			t.Fatalf("no tile at position %d", position)
		}
		if want := fmt.Sprintf("Reason %d", i+1); tile.Reason != want {
			t.Errorf("position %d: want reason %q, got %q", position, want, tile.Reason)
		}
	}
	// The card is stored, and the activity ticker announces it.
	stored, err := deps.missions.Get("me")
	if err != nil || stored == nil {
		t.Fatalf("the card should be stored: %+v (err %v)", stored, err)
	}
	if stored.CreatedAt.IsZero() {
		t.Error("the stored card should carry its creation time")
	}
	msgs, _ := deps.db.GetRecentActivity(5)
	if !strings.Contains(strings.Join(msgs, "\n"), "The Me got a mission card") {
		t.Errorf("the ticker should announce the card, got %v", msgs)
	}
}

func TestMissions_GenerateForIsIdempotent(t *testing.T) {
	llm := missionLLM()
	_, deps := newTestServer(t, llm, nil)
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	seed(t, deps.db, "me", "The Me", "ready")
	seedReady(t, deps.db, 9)

	first, err := deps.missions.GenerateFor("me")
	if err != nil {
		t.Fatal(err)
	}
	second, err := deps.missions.GenerateFor("me")
	if err != nil {
		t.Fatal(err)
	}

	stored, err := deps.missions.Get("me")
	if err != nil {
		t.Fatal(err)
	}
	if second.CreatedAt != stored.CreatedAt {
		t.Error("the second call should return the stored card, not a new one")
	}
	encodedFirst, _ := json.Marshal(first.Tiles)
	encodedSecond, _ := json.Marshal(second.Tiles)
	if string(encodedFirst) != string(encodedSecond) {
		t.Errorf("the card is fixed at generation:\n%s\n%s", encodedFirst, encodedSecond)
	}
	if n := llm.callsMatching("Mission Agent"); n != 1 {
		t.Errorf("the Mission Agent should be called once, got %d", n)
	}
}

func TestMissions_GenerateForReportsFailures(t *testing.T) {
	setup := func(t *testing.T) *testDeps {
		t.Helper()
		_, deps := newTestServer(t, missionLLM(), nil)
		if err := deps.events.Advance(PhaseSocial); err != nil {
			t.Fatal(err)
		}
		seed(t, deps.db, "me", "The Me", "ready")
		seedReady(t, deps.db, 2)
		return deps
	}

	t.Run("reading the card", func(t *testing.T) {
		deps := setup(t)
		// The read fault only fires when the card's row is scanned.
		if err := deps.missions.save(&MissionCard{ParticipantID: "me"}); err != nil {
			t.Fatal(err)
		}
		breakOnFault(t, deps.db, "mission_read")
		if _, err := deps.missions.GenerateFor("me"); err == nil {
			t.Error("want a failure when the card cannot be read")
		}
	})
	t.Run("reading the ready pool", func(t *testing.T) {
		deps := setup(t)
		breakOnFault(t, deps.db, "persona_lookup")
		if _, err := deps.missions.GenerateFor("me"); err == nil {
			t.Error("want a failure when the ready pool cannot be read")
		}
	})
	t.Run("an unknown participant", func(t *testing.T) {
		deps := setup(t)
		if _, err := deps.missions.GenerateFor("nobody"); err == nil {
			t.Error("want a failure for an unknown participant")
		}
	})
	t.Run("reading the completed meetings", func(t *testing.T) {
		deps := setup(t)
		// The read fault only fires when a meeting row is scanned.
		ms := NewMeetings(deps.db, deps.events)
		if _, _, err := ms.Confirm(reload(t, deps.db, "me"), reload(t, deps.db, "p00"), OriginMission); err != nil {
			t.Fatal(err)
		}
		breakOnFault(t, deps.db, "meet_read")
		if _, err := deps.missions.GenerateFor("me"); err == nil {
			t.Error("want a failure when the completed meetings cannot be read")
		}
	})
	t.Run("saving the card", func(t *testing.T) {
		deps := setup(t)
		breakOnFault(t, deps.db, "mission_insert")
		if _, err := deps.missions.GenerateFor("me"); err == nil {
			t.Error("want a failure when the card cannot be saved")
		}
	})
}

func TestMissions_Get(t *testing.T) {
	db := newTestDB(t)
	missions := NewMissions(db, NewMatcher(db, newFakeGitHub(), newFakeLLM()), newFakeLLM())

	if card, err := missions.Get("nobody"); card != nil || err != nil {
		t.Errorf("no card: want nil, got %+v (err %v)", card, err)
	}

	tile := newTile(centerPosition, TileMatch, testParticipant("m", "The M", nil), "why")
	if err := missions.save(&MissionCard{ParticipantID: "me", Tiles: []MissionTile{tile}}); err != nil {
		t.Fatal(err)
	}
	card, err := missions.Get("me")
	if err != nil {
		t.Fatal(err)
	}
	if len(card.Tiles) != 1 || card.Tiles[0] != tile || card.CreatedAt.IsZero() {
		t.Errorf("the card should round-trip, got %+v", card)
	}

	// Corruption setup: the store only writes well-formed tiles JSON, so an
	// undecodable row is written directly.
	db.db.Exec(`UPDATE mission_cards SET tiles = '{broken'`)
	if _, err := missions.Get("me"); err == nil {
		t.Error("want a failure for an undecodable card")
	}

	breakOnFault(t, db, "mission_read")
	if _, err := missions.Get("me"); err == nil {
		t.Error("want a failure when the card cannot be read")
	}
}

func TestMissions_Has(t *testing.T) {
	db := newTestDB(t)
	missions := NewMissions(db, NewMatcher(db, newFakeGitHub(), newFakeLLM()), newFakeLLM())

	if missions.Has("me") {
		t.Error("no card yet")
	}
	if err := missions.save(&MissionCard{ParticipantID: "me"}); err != nil {
		t.Fatal(err)
	}
	if !missions.Has("me") {
		t.Error("the card should be reported")
	}
}

func TestMissions_StampedDerivesFromCompletedMeetings(t *testing.T) {
	db, events, ms, a, b := meetingSetup(t)
	missions := NewMissions(db, NewMatcher(db, newFakeGitHub(), newFakeLLM()), newFakeLLM())

	stamped, err := missions.Stamped("a")
	if err != nil || len(stamped) != 0 {
		t.Fatalf("no meetings yet: got %v (err %v)", stamped, err)
	}

	if _, _, err := ms.Confirm(a, b, OriginMission); err != nil {
		t.Fatal(err)
	}
	stamped, err = missions.Stamped("a")
	if err != nil || len(stamped) != 0 {
		t.Errorf("a pending meeting is not a stamp: got %v (err %v)", stamped, err)
	}

	if _, _, err := ms.Confirm(b, a, OriginMission); err != nil {
		t.Fatal(err)
	}
	stamped, err = missions.Stamped("a")
	if err != nil || !stamped["b"] {
		t.Errorf("a completed meeting stamps the tile: got %v (err %v)", stamped, err)
	}

	breakOnFault(t, db, "meet_read")
	if _, err := missions.Stamped("a"); err == nil {
		t.Error("want a failure when the meetings cannot be read")
	}
	_ = events
}

func TestMissions_RemoveParticipant(t *testing.T) {
	db := newTestDB(t)
	missions := NewMissions(db, NewMatcher(db, newFakeGitHub(), newFakeLLM()), newFakeLLM())
	if err := missions.save(&MissionCard{ParticipantID: "me"}); err != nil {
		t.Fatal(err)
	}

	if err := missions.RemoveParticipant("me"); err != nil {
		t.Fatal(err)
	}
	if missions.Has("me") {
		t.Error("the card should be gone")
	}

	// The delete fault only fires when a row is actually deleted.
	if err := missions.save(&MissionCard{ParticipantID: "me"}); err != nil {
		t.Fatal(err)
	}
	breakOnFault(t, db, "mission_delete")
	if err := missions.RemoveParticipant("me"); err == nil {
		t.Error("want a failure when the card cannot be deleted")
	}
}

func TestMissions_GenerateAllDealsACardToEveryReadyParticipant(t *testing.T) {
	_, deps := newTestServer(t, missionLLM(), nil)
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	seedReady(t, deps.db, 12)

	deps.missions.GenerateAll()

	eventually(t, "a card for every ready participant", func() bool {
		return cardCount(t, deps.db) == 12
	})
	card, err := deps.missions.Get("p00")
	if err != nil || card == nil {
		t.Fatalf("p00's card: %+v (err %v)", card, err)
	}
	if len(card.Tiles) != 9 {
		t.Errorf("want 9 tiles, got %d", len(card.Tiles))
	}
	msgs, _ := deps.db.GetRecentActivity(20)
	if !strings.Contains(strings.Join(msgs, "\n"), "The Mission Agent dealt 12 mission cards") {
		t.Errorf("the ticker should announce the batch, got %v", msgs)
	}
}

func TestMissions_GenerateAllRunsALimitedWorkerPool(t *testing.T) {
	var mu sync.Mutex
	current, busiest := 0, 0
	llm := newFakeLLM().onFunc("Mission Agent", func(user string) (string, error) {
		mu.Lock()
		current++
		if current > busiest {
			busiest = current
		}
		mu.Unlock()
		time.Sleep(30 * time.Millisecond)
		mu.Lock()
		current--
		mu.Unlock()
		return reasonsReply(strings.Count(user, ". ["))
	})
	_, deps := newTestServer(t, llm, nil)
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	seedReady(t, deps.db, 10)

	deps.missions.GenerateAll()

	eventually(t, "a card for every ready participant", func() bool {
		return cardCount(t, deps.db) == 10
	})
	if busiest < 2 || busiest > missionWorkers {
		t.Errorf("the pool should run %d workers at a time, observed %d", missionWorkers, busiest)
	}
}

func TestMissions_GenerateAllReportsAPoolReadFailure(t *testing.T) {
	_, deps := newTestServer(t, missionLLM(), nil)
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	seedReady(t, deps.db, 2)
	breakOnFault(t, deps.db, "persona_lookup")

	deps.missions.generateAll()

	if n := cardCount(t, deps.db); n != 0 {
		t.Errorf("no cards should be dealt, got %d", n)
	}
}

func TestMissions_GenerateAllReportsACardFailureWithoutStoppingThePool(t *testing.T) {
	_, deps := newTestServer(t, missionLLM(), nil)
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	seedReady(t, deps.db, 2)
	breakOnFault(t, deps.db, "mission_insert")

	deps.missions.generateAll()

	if n := cardCount(t, deps.db); n != 0 {
		t.Errorf("the failing saves should leave no cards, got %d", n)
	}
}

func TestMissions_GenerateAllWithoutReadyParticipantsDealsNothing(t *testing.T) {
	_, deps := newTestServer(t, missionLLM(), nil)
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}

	deps.missions.generateAll()

	if msgs, _ := deps.db.GetRecentActivity(5); len(msgs) != 0 {
		t.Errorf("dealing no cards should log nothing, got %v", msgs)
	}
}

// ── generation timing: the social transition and latecomers ────────────────

func TestSocialTransition_GeneratesTheCardsAndReturnsImmediately(t *testing.T) {
	llm := missionLLM()
	llm.delay = 250 * time.Millisecond // one slow Mission Agent call
	srv, deps := newTestServer(t, llm, nil)
	seedReady(t, deps.db, 6)

	start := time.Now()
	resp := post(t, srv, "/admin/phase", url.Values{"to": {"social"}})
	elapsed := time.Since(start)

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("advance to social: want 303, got %d", resp.StatusCode)
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("the transition should return immediately, took %v", elapsed)
	}
	if deps.missions.Has("p00") {
		t.Error("the transition should return before the first card is generated")
	}

	eventually(t, "a card for every ready participant", func() bool {
		return cardCount(t, deps.db) == 6
	})
}

func TestSocialTransition_AtSixtyParticipantsThePoolDealsEveryCard(t *testing.T) {
	srv, deps := newTestServer(t, missionLLM(), nil)
	seedReady(t, deps.db, 60)

	resp := post(t, srv, "/admin/phase", url.Values{"to": {"social"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("advance to social: want 303, got %d", resp.StatusCode)
	}
	if deps.missions.Has("p00") {
		t.Error("the transition should return before the first card is generated")
	}

	start := time.Now()
	eventually(t, "a card for every ready participant", func() bool {
		return cardCount(t, deps.db) == 60
	})
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("60 cards should be dealt well within 5s per participant, took %v", elapsed)
	}
}

func TestClosedEvent_GeneratesNoCards(t *testing.T) {
	srv, deps := newTestServer(t, missionLLM(), nil)
	seedReady(t, deps.db, 3)

	resp := post(t, srv, "/admin/phase", url.Values{"to": {"closed"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("advance to closed: want 303, got %d", resp.StatusCode)
	}

	if deps.missions.Has("p00") {
		t.Error("no card should be generated once the event is closed")
	}
	if msgs, _ := deps.db.GetRecentActivity(5); strings.Contains(strings.Join(msgs, "\n"), "mission card") {
		t.Errorf("no card activity should be logged, got %v", msgs)
	}
}

func TestShowTellJump_GeneratesTheCards(t *testing.T) {
	// A forward jump straight to show & tell keeps everything in the social:
	// mission cards go live.
	srv, deps := newTestServer(t, missionLLM(), nil)
	seedReady(t, deps.db, 3)

	post(t, srv, "/admin/phase", url.Values{"to": {"showtell"}})

	eventually(t, "cards after jumping to show & tell", func() bool {
		return cardCount(t, deps.db) == 3
	})
}

// joinAndOnboard registers a GitHub participant and answers their whole
// Interview, returning them once they are ready.
func joinAndOnboard(t *testing.T, srv *testSrv, deps *testDeps, handle string) *Participant {
	t.Helper()
	post(t, srv, "/user/join", url.Values{"name": {handle}, "github": {handle}})
	var p *Participant
	eventually(t, handle+" interviewing", func() bool {
		p, _ = deps.db.GetParticipantByHandle(handle)
		return p != nil && p.PipelineStep == StepInterviewing
	})
	answerAll(t, srv, p)
	eventually(t, handle+" ready", func() bool {
		return reload(t, deps.db, p.ID).PipelineStep == StepReady
	})
	return reload(t, deps.db, p.ID)
}

func TestLatecomer_GetsACardWhenTheyReachReadyDuringTheSocial(t *testing.T) {
	gh := newFakeGitHub().withProfile(&GitHubProfile{Login: "late", Languages: []string{"Rust"}})
	llm := missionLLM().
		on("interviewer", `{"questions": ["Why Rust?"]}`).
		on("personality generator", `{"name": "The Borrow Checker", "tagline": "Ships"}`).
		onFunc("matchmaker", func(user string) (string, error) {
			// The seeded Gopher is the latecomer's best candidate.
			score := 55
			if strings.Contains(user, "The Gopher") {
				score = 90
			}
			return fmt.Sprintf(`{"score": %d, "reason": "Both like tabs", "red_flags": [], "green_flags": [], "icebreakers": []}`, score), nil
		})
	srv, deps := newTestServer(t, llm, gh)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seedReady(t, deps.db, 8) // p00..p07
	post(t, srv, "/admin/phase", url.Values{"to": {"social"}})
	eventually(t, "a's card at the social", func() bool { return deps.missions.Has("a") })
	// a was unmatched when their card was generated: the center is the
	// top-affinity fallback, and the card stays fixed now that a is matched.
	aCard, _ := deps.missions.Get("a")
	if center := centerTile(aCard); center == nil || center.Kind != TileAffinity {
		t.Fatalf("a's card is fixed at generation: center %+v", center)
	}

	late := joinAndOnboard(t, srv, deps, "late")

	eventually(t, "the latecomer matched with the Gopher", func() bool {
		return reload(t, deps.db, late.ID).MatchedWith == "a"
	})
	eventually(t, "the latecomer's card", func() bool { return deps.missions.Has(late.ID) })

	card, err := deps.missions.Get(late.ID)
	if err != nil || card == nil {
		t.Fatalf("the latecomer's card: %+v (err %v)", card, err)
	}
	if len(card.Tiles) != 9 {
		t.Fatalf("want 9 tiles, got %d", len(card.Tiles))
	}
	if center := centerTile(card); center == nil || center.PersonID != "a" || center.Kind != TileMatch {
		t.Errorf("the center should be the latecomer's match, got %+v", center)
	}
	// The match is excluded from the surrounding tiles; the 8 others fill them.
	seen := map[string]bool{}
	for _, position := range []int{0, 1, 2, 3, 5, 6, 7, 8} {
		tile := tileAt(card, position)
		if tile == nil {
			t.Fatalf("no tile at position %d", position)
		}
		if tile.PersonID == "a" {
			t.Errorf("the match must not be a surrounding tile (position %d)", position)
		}
		seen[tile.PersonID] = true
	}
	if len(seen) != 8 {
		t.Errorf("want 8 distinct surrounding persons, got %d", len(seen))
	}
}

func TestLatecomer_ACardFailureDoesNotBlockBecomingReady(t *testing.T) {
	gh := newFakeGitHub().withProfile(&GitHubProfile{Login: "late", Languages: []string{"Go"}})
	llm := missionLLM().
		on("interviewer", `{"questions": ["Why Go?"]}`).
		on("personality generator", `{"name": "The Late One", "tagline": "Ships"}`).
		on("matchmaker", `{"score": 80, "reason": "Both like Go", "red_flags": [], "green_flags": [], "icebreakers": []}`)
	srv, deps := newTestServer(t, llm, gh)
	seedReady(t, deps.db, 9)
	post(t, srv, "/admin/phase", url.Values{"to": {"social"}})

	breakOnFault(t, deps.db, "mission_insert")
	late := joinAndOnboard(t, srv, deps, "late")

	if late.PipelineStep != StepReady {
		t.Errorf("a card failure must not block becoming ready, got %v", late.PipelineStep)
	}
	if deps.missions.Has(late.ID) {
		t.Error("no card when the card cannot be saved")
	}
}

func TestLatecomer_ReadyDuringOnboardingGetsTheCardAtTheSocial(t *testing.T) {
	gh := newFakeGitHub().withProfile(&GitHubProfile{Login: "early", Languages: []string{"Go"}})
	llm := missionLLM().
		on("interviewer", `{"questions": ["Why Go?"]}`).
		on("personality generator", `{"name": "The Early Bird", "tagline": "Ships"}`).
		on("matchmaker", `{"score": 80, "reason": "Both like tabs", "red_flags": [], "green_flags": [], "icebreakers": []}`)
	srv, deps := newTestServer(t, llm, gh)
	seedReady(t, deps.db, 9)

	early := joinAndOnboard(t, srv, deps, "early")
	if deps.missions.Has(early.ID) {
		t.Error("no card while the event is still onboarding")
	}

	post(t, srv, "/admin/phase", url.Values{"to": {"social"}})

	eventually(t, "the card at the social transition", func() bool {
		return deps.missions.Has(early.ID)
	})
}

// ── the mission card section and its debug endpoint ────────────────────────

func TestMissionCardSection_RendersTheGridReasonsAndProgress(t *testing.T) {
	srv, deps := newTestServer(t, missionLLM(), nil)
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	seed(t, deps.db, "me", "The Me", "ready")
	seedReady(t, deps.db, 9)
	// Two completed meetings before the card is generated: me leads the
	// connection ranking, and the already-met are excluded from the tiles.
	ms := NewMeetings(deps.db, deps.events)
	me := reload(t, deps.db, "me")
	for _, other := range []string{"p00", "p01"} {
		them := reload(t, deps.db, other)
		if _, _, err := ms.Confirm(me, them, OriginMission); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ms.Confirm(them, me, OriginMission); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := deps.missions.GenerateFor("me"); err != nil {
		t.Fatal(err)
	}

	body := readBody(t, get(t, srv, "/user/mission/me"))

	for _, want := range []string{
		"Your mission", "The Persona 02", "Reason 1", // a tile with its reason
		"❤️ 2 · #1", "Top connections", // live progress
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the section should contain %q", want)
		}
	}
	if n := strings.Count(body, `rounded-full px-2 py-0.5`); n != 5 {
		t.Errorf("the top 5 should show 5 entries, got %d", n)
	}
	// The already-met are not on the card (their names still show in the
	// Top connections chips, so check the stored card).
	card, err := deps.missions.Get("me")
	if err != nil || card == nil {
		t.Fatalf("me's card: %+v (err %v)", card, err)
	}
	for _, tile := range card.Tiles {
		if tile.PersonID == "p00" || tile.PersonID == "p01" {
			t.Errorf("the already-met should not be tiles: %+v", tile)
		}
	}
}

func TestMissionCardSection_IsOnThePhaseAwarePersonalPage(t *testing.T) {
	srv, deps := newTestServer(t, missionLLM(), nil)
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	seed(t, deps.db, "zz", "The Zz", "ready")
	seedReady(t, deps.db, 9)
	if _, err := deps.missions.GenerateFor("zz"); err != nil {
		t.Fatal(err)
	}

	// The wait page (unmatched) carries the section.
	body := readBody(t, get(t, srv, "/user/wait/zz"))
	if !strings.Contains(body, `id="mission-card"`) || !strings.Contains(body, `hx-get="/user/mission/zz"`) {
		t.Error("the wait page should carry the mission card section")
	}

	// The match page (matched from the social on) carries it too.
	pair(t, deps.db, "zz", "p00")
	body = readBody(t, get(t, srv, "/user/match/zz"))
	if !strings.Contains(body, `id="mission-card"`) || !strings.Contains(body, `hx-get="/user/mission/zz"`) {
		t.Error("the match page should carry the mission card section")
	}
}

func TestMissionCardSection_ShowsThePreparingPlaceholder(t *testing.T) {
	srv, deps := newTestServer(t, missionLLM(), nil)
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	seed(t, deps.db, "me", "The Me", "ready")
	seedReady(t, deps.db, 2)

	body := readBody(t, get(t, srv, "/user/mission/me"))

	if !strings.Contains(body, "being prepared") {
		t.Errorf("the section should show the placeholder while the card is generated, got %q", body)
	}
	if strings.Contains(body, "meet 8 personas") {
		t.Error("no card yet: the grid should not render")
	}
}

func TestMissionCardSection_IsEmptyWhenTheEventDoesNotAllowCards(t *testing.T) {
	srv, deps := newTestServer(t, missionLLM(), nil)
	seed(t, deps.db, "me", "The Me", "ready")
	seedReady(t, deps.db, 2)

	body := readBody(t, get(t, srv, "/user/mission/me"))

	if strings.Contains(body, "being prepared") || strings.Contains(body, "Your mission") {
		t.Errorf("during onboarding the section should be empty, got %q", body)
	}
}

func TestMissionCardSection_AnUnknownParticipantIs404(t *testing.T) {
	srv, _ := newTestServer(t, missionLLM(), nil)

	if resp := get(t, srv, "/user/mission/nobody"); resp.StatusCode != 404 {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

func TestMissionCard_StampsDeriveFromCompletedMeetings(t *testing.T) {
	srv, deps := newTestServer(t, missionLLM(), nil)
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	seed(t, deps.db, "me", "The Me", "ready")
	seedReady(t, deps.db, 9)
	if _, err := deps.missions.GenerateFor("me"); err != nil {
		t.Fatal(err)
	}
	card, _ := deps.missions.Get("me")
	target := tileAt(card, 0)
	if target == nil {
		t.Fatal("the card should have a tile at position 0")
	}
	ms := NewMeetings(deps.db, deps.events)
	me := reload(t, deps.db, "me")
	them := reload(t, deps.db, target.PersonID)

	// A one-sided confirmation is pending: no stamp yet.
	if _, _, err := ms.Confirm(me, them, OriginMission); err != nil {
		t.Fatal(err)
	}
	body := readBody(t, get(t, srv, "/user/mission/me"))
	if strings.Contains(body, "stamp-in") {
		t.Error("a pending meeting must not stamp the tile")
	}

	// Both sides confirmed: the tile stamps, derived from the meeting.
	if _, _, err := ms.Confirm(them, me, OriginMission); err != nil {
		t.Fatal(err)
	}
	body = readBody(t, get(t, srv, "/user/mission/me"))
	if !strings.Contains(body, "stamp-in") {
		t.Error("a completed meeting should stamp the tile")
	}

	// The debug endpoint shows the same stamp state.
	body = readBody(t, get(t, srv, "/data/missioncard/me"))
	var payload struct {
		Tiles []struct {
			Position int    `json:"position"`
			PersonID string `json:"person_id"`
			Reason   string `json:"reason"`
			Stamped  bool   `json:"stamped"`
		} `json:"tiles"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tile := range payload.Tiles {
		if tile.Position == 0 {
			found = true
			if !tile.Stamped {
				t.Error("position 0 should be stamped")
			}
			if tile.Reason != "Reason 1" {
				t.Errorf("position 0: want the tile's reason, got %q", tile.Reason)
			}
		}
	}
	if !found {
		t.Error("the debug endpoint should show the tile at position 0")
	}
}

func TestDataMissionCard(t *testing.T) {
	srv, deps := newTestServer(t, missionLLM(), nil)
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	seed(t, deps.db, "me", "The Me", "ready")
	seedReady(t, deps.db, 9)
	if _, err := deps.missions.GenerateFor("me"); err != nil {
		t.Fatal(err)
	}

	resp := get(t, srv, "/data/missioncard/me")
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	for _, want := range []string{`"participant_id": "me"`, `"The Me"`, `"position": 4`, `"kind": "affinity"`, `"reason": "Reason 1"`, `"stamped": false`} {
		if !strings.Contains(body, want) {
			t.Errorf("the debug endpoint should contain %s", want)
		}
	}

	if resp := get(t, srv, "/data/missioncard/nobody"); resp.StatusCode != 404 {
		t.Errorf("an unknown participant: want 404, got %d", resp.StatusCode)
	}
	if resp := get(t, srv, "/data/missioncard/p00"); resp.StatusCode != 404 {
		t.Errorf("a participant without a card: want 404, got %d", resp.StatusCode)
	}

	breakOnFault(t, deps.db, "mission_read")
	if resp := get(t, srv, "/data/missioncard/me"); resp.StatusCode != 500 {
		t.Errorf("a card that cannot be read: want 500, got %d", resp.StatusCode)
	}
}

// ── the section's data: rank vs the top 5, grid, read failures ────────────

func TestMissionCardData_RanksAgainstTheTop5(t *testing.T) {
	_, deps := newTestServer(t, missionLLM(), nil)
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"p1", "p2", "p3", "p4", "p5", "p6", "p7", "p8"} {
		seed(t, deps.db, id, "The "+id, "ready")
	}
	// Completed meetings: p1 and p2 have 3 connections each, p3-p6 one, p7-p8 none.
	ms := NewMeetings(deps.db, deps.events)
	for _, pair := range [][2]string{{"p1", "p2"}, {"p1", "p3"}, {"p1", "p4"}, {"p2", "p5"}, {"p2", "p6"}} {
		a, b := reload(t, deps.db, pair[0]), reload(t, deps.db, pair[1])
		if _, _, err := ms.Confirm(a, b, OriginOther); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ms.Confirm(b, a, OriginOther); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"p1", "p8"} {
		if _, err := deps.missions.GenerateFor(id); err != nil {
			t.Fatal(err)
		}
	}
	h := NewHandler(deps.db, nil, nil, nil, nil, nil, deps.missions, deps.showtell, NewSlideDecks(deps.db, t.TempDir()), nil)

	data := h.missionCardData(reload(t, deps.db, "p1"))
	if data["Connections"].(int) != 3 {
		t.Errorf("p1's connections: want 3, got %v", data["Connections"])
	}
	if data["Rank"].(int) != 1 {
		t.Errorf("p1's rank: want 1, got %v", data["Rank"])
	}
	top := data["Top"].([]ConnectionStanding)
	if len(top) != 5 {
		t.Fatalf("the top 5: want 5 entries, got %d", len(top))
	}
	wantOrder := []string{"p1", "p2", "p3", "p4", "p5"} // ties broken by persona name
	for i, want := range wantOrder {
		if top[i].ID != want {
			t.Errorf("top[%d]: want %s, got %s", i, want, top[i].ID)
		}
	}

	data = h.missionCardData(reload(t, deps.db, "p8"))
	if data["Rank"].(int) != 8 {
		t.Errorf("p8's rank: want 8, got %v", data["Rank"])
	}
	if len(data["Top"].([]ConnectionStanding)) != 5 {
		t.Error("the top 5 is capped at 5")
	}
}

func TestMissionCardData_WithFewParticipantsTheTopIsTheWholeBoard(t *testing.T) {
	_, deps := newTestServer(t, missionLLM(), nil)
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	seedReady(t, deps.db, 3)
	if _, err := deps.missions.GenerateFor("p00"); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(deps.db, nil, nil, nil, nil, nil, deps.missions, deps.showtell, NewSlideDecks(deps.db, t.TempDir()), nil)

	data := h.missionCardData(reload(t, deps.db, "p00"))

	if top := data["Top"].([]ConnectionStanding); len(top) != 3 {
		t.Errorf("with 3 participants the top is the whole board, got %d", len(top))
	}
}

func TestMissionCardData_ReportsReadFailures(t *testing.T) {
	setup := func(t *testing.T) (*testDeps, *Handler) {
		t.Helper()
		_, deps := newTestServer(t, missionLLM(), nil)
		if err := deps.events.Advance(PhaseSocial); err != nil {
			t.Fatal(err)
		}
		seed(t, deps.db, "me", "The Me", "ready")
		seedReady(t, deps.db, 9)
		if _, err := deps.missions.GenerateFor("me"); err != nil {
			t.Fatal(err)
		}
		return deps, NewHandler(deps.db, nil, nil, nil, nil, nil, deps.missions, deps.showtell, NewSlideDecks(deps.db, t.TempDir()), nil)
	}

	t.Run("the card cannot be read", func(t *testing.T) {
		deps, h := setup(t)
		breakOnFault(t, deps.db, "mission_read")
		data := h.missionCardData(reload(t, deps.db, "me"))
		if _, ok := data["Tiles"]; ok {
			t.Error("without a card there are no tiles")
		}
		if !data["Preparing"].(bool) {
			t.Error("the placeholder should show while the event allows cards")
		}
	})
	t.Run("the meetings cannot be read", func(t *testing.T) {
		deps, h := setup(t)
		// The read fault only fires when a meeting row is scanned.
		ms := NewMeetings(deps.db, deps.events)
		me, them := reload(t, deps.db, "me"), reload(t, deps.db, "p00")
		if _, _, err := ms.Confirm(me, them, OriginMission); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ms.Confirm(them, me, OriginMission); err != nil {
			t.Fatal(err)
		}
		breakOnFault(t, deps.db, "meet_read")
		data := h.missionCardData(me)
		tiles := data["Tiles"].([]missionTileView)
		if len(tiles) != 9 {
			t.Fatalf("the tiles should still render, got %d", len(tiles))
		}
		for _, tile := range tiles {
			if tile.Stamped {
				t.Error("without readable meetings no tile is stamped")
			}
		}
	})
	t.Run("the leaderboard cannot be read", func(t *testing.T) {
		deps, h := setup(t)
		p := reload(t, deps.db, "me")
		breakOnFault(t, deps.db, "persona_lookup")
		data := h.missionCardData(p)
		if data["Rank"].(int) != 0 {
			t.Errorf("without a leaderboard there is no rank, got %v", data["Rank"])
		}
		if top, ok := data["Top"].([]ConnectionStanding); !ok || len(top) != 0 {
			t.Errorf("without a leaderboard there is no top, got %v", data["Top"])
		}
	})
}

func TestTileGrid_KeepsTheGridShape(t *testing.T) {
	tiles := []missionTileView{
		{MissionTile: MissionTile{Position: 4, PersonID: "center"}},
		{MissionTile: MissionTile{Position: 0, PersonID: "top-left"}},
		{MissionTile: MissionTile{Position: 8, PersonID: "bottom-right"}},
	}

	grid := tileGrid(tiles)

	if grid[4] == nil || grid[4].PersonID != "center" {
		t.Errorf("position 4 should hold the center, got %+v", grid[4])
	}
	if grid[0] == nil || grid[0].PersonID != "top-left" {
		t.Errorf("position 0 should hold the first tile, got %+v", grid[0])
	}
	if grid[8] == nil || grid[8].PersonID != "bottom-right" {
		t.Errorf("position 8 should hold the last tile, got %+v", grid[8])
	}
	for _, position := range []int{1, 2, 3, 5, 6, 7} {
		if grid[position] != nil {
			t.Errorf("position %d should stay empty", position)
		}
	}

	// Deliberately invalid state: the store only writes positions 0-8, so an
	// out-of-range position is written directly; the tile is dropped, the grid
	// keeps its shape.
	grid = tileGrid([]missionTileView{{MissionTile: MissionTile{Position: 99}}, {MissionTile: MissionTile{Position: -1}}})
	for i, tile := range grid {
		if tile != nil {
			t.Errorf("grid[%d] should be empty", i)
		}
	}
}

func TestTileGrid_AnOutOfRangeTileStillRenders(t *testing.T) {
	srv, deps := newTestServer(t, missionLLM(), nil)
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	seed(t, deps.db, "me", "The Me", "ready")
	// Deliberately invalid state (as in TestTileGrid): a position the store
	// never writes, stored directly.
	deps.db.db.Exec(`INSERT INTO mission_cards (participant_id, tiles) VALUES ('me', '[{"position": 99, "kind": "wildcard", "person_id": "x", "persona_name": "The X", "persona_symbol": "❓", "persona_color": "bg-gray-400", "reason": "r"}]')`)

	body := readBody(t, get(t, srv, "/user/mission/me"))

	if !strings.Contains(body, "Your mission") {
		t.Error("the card should render with its grid")
	}
	if strings.Contains(body, "The X") {
		t.Error("the out-of-range tile should be dropped")
	}
}

func TestRankOf(t *testing.T) {
	board := []ConnectionStanding{{ID: "a", Connections: 3}, {ID: "b", Connections: 1}}

	if got := rankOf(board, "b"); got != 2 {
		t.Errorf("rank of b: want 2, got %d", got)
	}
	if got := rankOf(board, "nobody"); got != 0 {
		t.Errorf("rank of nobody: want 0, got %d", got)
	}
}

// ── push: the card and the stamps arrive via the streams ───────────────────

func TestWaitStream_TheCardAppearsViaPush(t *testing.T) {
	srv, deps := newTestServer(t, missionLLM(), nil)
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	seed(t, deps.db, "zz", "The Zz", "ready")
	seedReady(t, deps.db, 9)

	s := open(t, srv, "/user/wait-stream/zz")
	waitFor(t, s, "event: refresh") // the sync on connect
	before := s.String()
	sentNothingNew(t, s, 1, before, "before the card exists")

	if _, err := deps.missions.GenerateFor("zz"); err != nil {
		t.Fatal(err)
	}
	waitForCount(t, s, "event: refresh", strings.Count(before, "event: refresh")+1)

	body := readBody(t, get(t, srv, "/user/mission/zz"))
	if !strings.Contains(body, "Your mission") {
		t.Error("after the push the section should show the card")
	}
}

func TestMatchStream_TheStampsArriveViaPush(t *testing.T) {
	srv, deps := newTestServer(t, missionLLM(), nil)
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	seed(t, deps.db, "zz", "The Zz", "ready")
	seedReady(t, deps.db, 9)
	if _, err := deps.missions.GenerateFor("zz"); err != nil {
		t.Fatal(err)
	}
	pair(t, deps.db, "zz", "p00")

	s := open(t, srv, "/user/match-stream/zz")
	waitFor(t, s, "event: refresh") // the sync on connect
	before := s.String()
	sentNothingNew(t, s, 1, before, "nothing changed")

	// Completing a meeting with a tile person stamps the tile — the push
	// re-renders the section.
	card, _ := deps.missions.Get("zz")
	target := tileAt(card, 0)
	ms := NewMeetings(deps.db, deps.events)
	me := reload(t, deps.db, "zz")
	them := reload(t, deps.db, target.PersonID)
	if _, _, err := ms.Confirm(me, them, OriginMission); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ms.Confirm(them, me, OriginMission); err != nil {
		t.Fatal(err)
	}
	waitForCount(t, s, "event: refresh", strings.Count(before, "event: refresh")+1)

	body := readBody(t, get(t, srv, "/user/mission/zz"))
	if !strings.Contains(body, "stamp-in") {
		t.Error("after the push the tile should be stamped")
	}
}

// ── phase predicate, leaderboard, reset, delete ────────────────────────────

func TestPhase_MissionCardsLive(t *testing.T) {
	for _, tc := range []struct {
		phase Phase
		want  bool
	}{
		{PhaseOnboarding, false},
		{PhaseSocial, true},
		{PhaseShowTell, true},
		{PhaseClosed, false},
		{Phase("banquet"), false}, // an unknown phase never counts as reached
	} {
		if got := tc.phase.MissionCardsLive(); got != tc.want {
			t.Errorf("%s.MissionCardsLive(): want %v, got %v", tc.phase, tc.want, got)
		}
	}
}

func TestMeetings_Leaderboard(t *testing.T) {
	db, _, ms, a, b := meetingSetup(t)
	seed(t, db, "c", "The C", "ready")

	board, err := ms.Leaderboard()
	if err != nil {
		t.Fatal(err)
	}
	if len(board) != 3 {
		t.Fatalf("want 3 participants, got %d", len(board))
	}
	for _, standing := range board {
		if standing.Connections != 0 {
			t.Errorf("%s: a pending-less board should be all zeros, got %+v", standing.ID, standing)
		}
	}

	// One side only: still no connections.
	if _, _, err := ms.Confirm(a, b, OriginOther); err != nil {
		t.Fatal(err)
	}
	board, _ = ms.Leaderboard()
	for _, standing := range board {
		if standing.Connections != 0 {
			t.Errorf("a pending meeting is not a connection: %+v", standing)
		}
	}

	// Both sides: one connection each; ties broken by persona name, so the
	// Crab ranks above the Gopher.
	if _, _, err := ms.Confirm(b, a, OriginOther); err != nil {
		t.Fatal(err)
	}
	board, err = ms.Leaderboard()
	if err != nil {
		t.Fatal(err)
	}
	want := []ConnectionStanding{
		{ID: "b", Name: "The Crab", Symbol: board[0].Symbol, Color: board[0].Color, Connections: 1},
		{ID: "a", Name: "The Gopher", Symbol: board[1].Symbol, Color: board[1].Color, Connections: 1},
		{ID: "c", Name: "The C", Symbol: board[2].Symbol, Color: board[2].Color, Connections: 0},
	}
	for i, w := range want {
		if board[i] != w {
			t.Errorf("board[%d]: want %+v, got %+v", i, w, board[i])
		}
	}
}

func TestMeetings_LeaderboardReportsReadFailures(t *testing.T) {
	t.Run("the query fails", func(t *testing.T) {
		db, _, ms, _, _ := meetingSetup(t)
		breakOnFault(t, db, "persona_lookup")
		if _, err := ms.Leaderboard(); err == nil {
			t.Error("want a failure when the participants cannot be read")
		}
	})
	t.Run("a row fails to scan", func(t *testing.T) {
		db, _, ms, _, _ := meetingSetup(t)
		breakOnFault(t, db, "connection_read")
		if _, err := ms.Leaderboard(); err == nil {
			t.Error("want a failure when a row cannot be scanned")
		}
	})
}

func TestReset_ClearsTheMissionCards(t *testing.T) {
	db := newTestDB(t)
	events := NewEventStore(db)
	seed(t, db, "a", "The A", "ready")
	if err := events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	missions := NewMissions(db, NewMatcher(db, newFakeGitHub(), newFakeLLM()), missionLLM())
	if _, err := missions.GenerateFor("a"); err != nil {
		t.Fatal(err)
	}

	if err := events.Reset(); err != nil {
		t.Fatal(err)
	}
	if card, err := missions.Get("a"); card != nil || err != nil {
		t.Errorf("a Reset clears the cards: got %+v (err %v)", card, err)
	}

	// The clear fault only fires when a row is actually deleted.
	seed(t, db, "a", "The A", "ready")
	if err := missions.save(&MissionCard{ParticipantID: "a"}); err != nil {
		t.Fatal(err)
	}
	breakOnFault(t, db, "reset_mission_cards")
	if err := events.Reset(); err == nil {
		t.Error("want a failure when the cards cannot be cleared")
	}
}

func TestDeleteParticipant_RemovesTheMissionCard(t *testing.T) {
	srv, deps := newTestServer(t, missionLLM(), nil)
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	seed(t, deps.db, "gone", "The Gone", "ready")
	seedReady(t, deps.db, 2)
	if _, err := deps.missions.GenerateFor("gone"); err != nil {
		t.Fatal(err)
	}
	// The delete fault only fires when a row is actually deleted.
	if _, err := deps.missions.GenerateFor("p00"); err != nil {
		t.Fatal(err)
	}

	if resp := del(t, srv, "/data/participant/gone"); resp.StatusCode != 200 {
		t.Fatalf("delete: want 200, got %d", resp.StatusCode)
	}
	if deps.missions.Has("gone") {
		t.Error("deleting the participant should delete their card")
	}

	breakOnFault(t, deps.db, "mission_delete")
	if resp := del(t, srv, "/data/participant/p00"); resp.StatusCode != 500 {
		t.Errorf("a card that cannot be deleted: want 500, got %d", resp.StatusCode)
	}
}
