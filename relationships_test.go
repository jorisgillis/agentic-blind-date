package main

import (
	"sort"
	"testing"
)

func readyParticipants(t *testing.T, db *DB, ids ...string) map[string]*Participant {
	t.Helper()
	out := map[string]*Participant{}
	for _, id := range ids {
		out[id] = seed(t, db, id, "The "+id, "ready")
	}
	return out
}

func assessment(score int) *matchResult {
	return &matchResult{Score: score, Reason: "why not", RedFlags: []string{"tabs, sadly"}, GreenFlags: []string{}, Icebreakers: []string{"Why?"}}
}

// assertInvariant checks the Key Invariant: every Match is symmetric, so nobody has two partners.
func assertInvariant(t *testing.T, db *DB) {
	t.Helper()
	all, _ := db.GetAllParticipants()
	byID := map[string]*Participant{}
	for _, p := range all {
		byID[p.ID] = p
	}
	for _, p := range all {
		if p.MatchedWith == "" {
			continue
		}
		partner, ok := byID[p.MatchedWith]
		if !ok || partner.MatchedWith != p.ID {
			t.Errorf("Key Invariant broken: %s → %s, but not the other way round", p.ID, p.MatchedWith)
		}
	}
}

func TestRelationshipsPair_RecordsTheMatchOnBothSides(t *testing.T) {
	db := newTestDB(t)
	ps := readyParticipants(t, db, "A", "B")
	rel := NewRelationships(db)

	displaced, err := rel.Pair(Match{A: ps["A"], B: ps["B"], Result: assessment(80)})

	if err != nil || len(displaced) != 0 {
		t.Fatalf("Pair: displaced %v, err %v", displaced, err)
	}
	for _, id := range []string{"A", "B"} {
		p := reload(t, db, id)
		if p.MatchedWith == "" || p.CompatScore != 80 || p.CompatReason != "why not" || p.RedFlags != `["tabs, sadly"]` {
			t.Errorf("%s: %+v", id, p)
		}
	}
	assertInvariant(t, db)
}

func TestRelationshipsPair_TakingOverAMatchReturnsTheDisplacedPartnerToThePool(t *testing.T) {
	db := newTestDB(t)
	ps := readyParticipants(t, db, "A", "B", "N")
	rel := NewRelationships(db)
	rel.Pair(Match{A: ps["A"], B: ps["B"], Result: assessment(40)})

	displaced, err := rel.Pair(Match{A: ps["N"], B: reload(t, db, "A"), Result: assessment(70)})

	if err != nil {
		t.Fatal(err)
	}
	if len(displaced) != 1 || displaced[0] != "B" {
		t.Errorf("displaced: want [B], got %v", displaced)
	}
	if b := reload(t, db, "B"); b.MatchedWith != "" || b.PipelineStep != "ready" || b.CompatScore != 0 || b.RedFlags != "[]" {
		t.Errorf("B should be back in the Pool with no assessment, got %+v", b)
	}
	if a := reload(t, db, "A"); a.MatchedWith != "N" || a.CompatScore != 70 {
		t.Errorf("A should be matched with N at 70, got %q %d", a.MatchedWith, a.CompatScore)
	}
	assertInvariant(t, db)
}

func TestRelationshipsPair_PairingTwoMatchedParticipantsDisplacesBothPartners(t *testing.T) {
	db := newTestDB(t)
	ps := readyParticipants(t, db, "A", "B", "C", "D")
	rel := NewRelationships(db)
	rel.Pair(Match{A: ps["A"], B: ps["B"], Result: assessment(40)})
	rel.Pair(Match{A: ps["C"], B: ps["D"], Result: assessment(40)})

	displaced, _ := rel.Pair(Match{A: reload(t, db, "A"), B: reload(t, db, "C"), Result: assessment(90)})

	sort.Strings(displaced)
	if len(displaced) != 2 || displaced[0] != "B" || displaced[1] != "D" {
		t.Errorf("displaced: want [B D], got %v", displaced)
	}
	assertInvariant(t, db)
}

func TestRelationshipsPair_AFailedPairLeavesNothingHalfWritten(t *testing.T) {
	db := newTestDB(t)
	ps := readyParticipants(t, db, "A", "B")
	rel := NewRelationships(db)
	rel.Pair(Match{A: ps["A"], B: ps["B"], Result: assessment(40)})

	_, err := rel.Pair(Match{A: reload(t, db, "A"), B: &Participant{ID: "ghost"}, Result: assessment(99)})

	if err == nil {
		t.Fatal("pairing with an unknown Participant should fail")
	}
	if a := reload(t, db, "A"); a.MatchedWith != "B" || a.CompatScore != 40 {
		t.Errorf("A's original Match should be untouched, got %q %d", a.MatchedWith, a.CompatScore)
	}
	assertInvariant(t, db)
}

func TestRelationshipsRemove_FreesThePartner(t *testing.T) {
	db := newTestDB(t)
	ps := readyParticipants(t, db, "A", "B")
	rel := NewRelationships(db)
	rel.Pair(Match{A: ps["A"], B: ps["B"], Result: assessment(80)})

	if err := rel.Remove("A"); err != nil {
		t.Fatal(err)
	}

	if _, err := db.GetParticipant("A"); err == nil {
		t.Error("A should be gone")
	}
	if b := reload(t, db, "B"); b.MatchedWith != "" || b.PipelineStep != "ready" {
		t.Errorf("B should be back in the Pool, got step %s matched with %q", b.PipelineStep, b.MatchedWith)
	}
	assertInvariant(t, db)
}

func TestRelationshipsUnpairAll_ReturnsEveryoneToThePool(t *testing.T) {
	db := newTestDB(t)
	ps := readyParticipants(t, db, "A", "B", "C", "D", "E")
	rel := NewRelationships(db)
	rel.Pair(Match{A: ps["A"], B: ps["B"], Result: assessment(80)})
	rel.Pair(Match{A: ps["C"], B: ps["D"], Result: assessment(60)})

	if err := rel.UnpairAll(); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"A", "B", "C", "D", "E"} {
		if p := reload(t, db, id); p.MatchedWith != "" || p.PipelineStep != "ready" || p.CompatScore != 0 || p.RedFlags != "[]" {
			t.Errorf("%s should be unmatched and ready with no assessment, got %+v", id, p)
		}
	}
}

func TestRematch_RunningAlongsideANewcomersMatchingKeepsTheKeyInvariant(t *testing.T) {
	llm := newFakeLLM().on("matchmaker", `{"score": 70, "reason": "fine"}`)
	_, deps := newTestServer(t, llm, nil)
	for _, id := range []string{"A", "B", "C", "D", "N"} {
		seed(t, deps.db, id, id, "ready")
	}

	done := make(chan error, 2)
	go func() { done <- deps.agents.Rematch() }()
	go func() { done <- deps.agents.RunContinuousMatching(reload(t, deps.db, "N")) }()
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}

	assertInvariant(t, deps.db)
}

func TestRelationshipsPartnerOf_ReturnsThePartnerAndTheAssessment(t *testing.T) {
	db := newTestDB(t)
	ps := readyParticipants(t, db, "A", "B", "C")
	rel := NewRelationships(db)
	rel.Pair(Match{A: ps["A"], B: ps["B"], Result: assessment(80)})

	partner, result, err := rel.PartnerOf("A")

	if err != nil || partner == nil || partner.ID != "B" {
		t.Fatalf("partner: got %v (err %v)", partner, err)
	}
	if result.Score != 80 || result.Reason != "why not" || len(result.RedFlags) != 1 || result.RedFlags[0] != "tabs, sadly" || len(result.Icebreakers) != 1 {
		t.Errorf("assessment: got %+v", result)
	}
	if partner, result, err := rel.PartnerOf("C"); partner != nil || result != nil || err != nil {
		t.Errorf("unmatched: want nothing, got %v %v %v", partner, result, err)
	}
}

func TestRelationshipsPartnerOf_AnUndecodableAssessmentIsEmptyNotBroken(t *testing.T) {
	db := newTestDB(t)
	ps := readyParticipants(t, db, "A", "B")
	rel := NewRelationships(db)
	rel.Pair(Match{A: ps["A"], B: ps["B"], Result: assessment(80)})
	db.db.Exec(`UPDATE participants SET red_flags = 'not json', icebreakers = '' WHERE id = 'A'`)

	_, result, err := rel.PartnerOf("A")

	if err != nil || result.RedFlags == nil || len(result.RedFlags) != 0 || result.Icebreakers == nil {
		t.Errorf("want empty lists, got %+v (err %v)", result, err)
	}
}

func TestRelationshipState_IsSeparateFromThePipelineStep(t *testing.T) {
	db := newTestDB(t)
	ps := readyParticipants(t, db, "A", "B", "N")
	rel := NewRelationships(db)

	rel.Pair(Match{A: ps["A"], B: ps["B"], Result: assessment(40)})
	if a := reload(t, db, "A"); a.PipelineStep != "ready" || !a.IsMatched() {
		t.Errorf("a matched Participant stays at Pipeline Step ready, got %s (matched %v)", a.PipelineStep, a.IsMatched())
	}
	if db.ReadyCount() != 3 {
		t.Errorf("ReadyCount counts matched Participants too: want 3, got %d", db.ReadyCount())
	}
}

func TestNewDB_MigratesTheOldMatchedPipelineStep(t *testing.T) {
	path := t.TempDir() + "/old.db"
	old, err := NewDB(path)
	if err != nil {
		t.Fatal(err)
	}
	old.CreateParticipant("A", "a", "A")
	old.CreateParticipant("B", "b", "B")
	old.db.Exec(`UPDATE participants SET pipeline_step = 'matched', matched_with = 'B' WHERE id = 'A'`)
	old.db.Exec(`UPDATE participants SET pipeline_step = 'matched', matched_with = 'A' WHERE id = 'B'`)
	old.Close()

	db, err := NewDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if a := reload(t, db, "A"); a.PipelineStep != "ready" || a.MatchedWith != "B" {
		t.Errorf("want step ready and partner kept, got %s / %q", a.PipelineStep, a.MatchedWith)
	}
}
