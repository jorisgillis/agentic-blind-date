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
