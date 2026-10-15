package main

import (
	"strings"
	"testing"
)

// wantCards is the registry's expected composition (#154): one entry per
// personal-page card, each naming its placement — the region it occupies on
// each page shell, or "" when the page does not carry it — and the data map
// key its data lands under ("" for the cards their container renders from
// its own data). An entry's placement decides which page shells and regions
// carry it: the wait and match page data maps and the phase-content region
// derive from exactly this table.
var wantCards = map[string]struct {
	wait, match cardRegion
	key         string
}{
	"phase-header": {regionBody, regionBody, "PhaseHeader"},
	"persona-card": {regionBody, "", "Participant"},
	"slides":       {regionBody, regionBody, "Slides"},
	"wait-status":  {regionBody, "", ""},
	"mission-card": {regionBody, regionPhase, "MissionCard"},
	"meet-status":  {regionBody, regionPhase, "MeetStatus"},
	"showtell":     {regionBody, regionPhase, "ShowTell"},
	"board":        {regionBody, regionBody, "Board"},
	"dossier":      {"", regionDrawer, "Dossier"},
	"settings":     {regionDrawer, regionDrawer, "Settings"},
	"recap-link":   {regionDrawer, regionDrawer, "RecapLink"},
	"match-reveal": {"", regionPhase, ""},
}

// pushedCards is which cards the pages' streams push: those name the
// signature source the streams watch. The others never refresh on their
// own: the phase-content region swaps whole on the page-level 'phase'
// event, the settings save re-renders itself, and the match reveal rides
// the region.
var pushedCards = map[string]bool{
	"phase-header": true, "persona-card": true, "wait-status": true,
	"slides": true, "mission-card": true, "meet-status": true,
	"showtell": true, "board": true, "dossier": true, "recap-link": true,
}

// TestCardRegistry_Composition asserts the registry's composition: one
// entry per personal-page card, each naming its region, its fragment, its
// data function and its signature source (#154's acceptance).
func TestCardRegistry_Composition(t *testing.T) {
	byName := make(map[string]pageCard, len(pageCards))
	for _, c := range pageCards {
		byName[c.Name] = c
	}
	if len(byName) != len(pageCards) {
		t.Errorf("the registry carries duplicate cards")
	}
	for name := range byName {
		if _, ok := wantCards[name]; !ok {
			t.Errorf("the registry carries %q, which the composition does not know", name)
		}
	}
	for name, want := range wantCards {
		c, ok := byName[name]
		if !ok {
			t.Errorf("the registry should carry %q", name)
			continue
		}
		if c.OnWait != want.wait || c.OnMatch != want.match {
			t.Errorf("%s: want placement wait=%q match=%q, got wait=%q match=%q",
				name, want.wait, want.match, c.OnWait, c.OnMatch)
		}
		if c.Key != want.key {
			t.Errorf("%s: want data key %q, got %q", name, want.key, c.Key)
		}
		if c.Fragment == "" {
			t.Errorf("%s: every card names its fragment", name)
		}
		if c.Data == nil {
			t.Errorf("%s: every card names its data function", name)
		}
		if pushedCards[name] != (c.Signature != nil) {
			t.Errorf("%s: pushed=%t but signature source is %v", name, pushedCards[name], c.Signature != nil)
		}
	}
}

// TestCardRegistry_PlacementDrivesThePageData asserts the derivation: the
// wait and match page data maps and the phase-content region carry exactly
// the cards the registry places on them — an entry's placement decides
// which page shells and regions carry it.
func TestCardRegistry_PlacementDrivesThePageData(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "me", "The Me", "ready")
	seed(t, deps.db, "mate", "The Mate", "ready")
	pair(t, deps.db, "me", "mate")
	deps.events.Reveal()
	me := reload(t, deps.db, "me")

	// The wait page's data map: every wait page card's key, plus the base
	// the status card's data spreads at the page's top level — and nothing
	// that only the match page carries.
	wait := deps.handler.waitPageData(me)
	for _, key := range []string{
		"Participant", "Count", "Revealed", "QueueAhead", // the status card's data
		"PhaseHeader", "Slides", "MissionCard", "MeetStatus", "ShowTell",
		"Board", "Settings", "RecapLink",
	} {
		if _, ok := wait[key]; !ok {
			t.Errorf("the wait page's data map should carry %q", key)
		}
	}
	for _, key := range []string{"PhaseContent", "Dossier", "Match", "Score"} {
		if _, ok := wait[key]; ok {
			t.Errorf("the wait page's data map should not carry %q", key)
		}
	}

	// The match page's data map: the match page's body and drawer cards'
	// keys — the phase-content region's cards land in the region's map,
	// not the page's.
	match := deps.handler.matchPageData(me)
	for _, key := range []string{
		"Me", "PhaseHeader", "Slides", "PhaseContent", "Board",
		"Dossier", "Settings", "RecapLink",
	} {
		if _, ok := match[key]; !ok {
			t.Errorf("the match page's data map should carry %q", key)
		}
	}
	for _, key := range []string{"MissionCard", "MeetStatus", "ShowTell"} {
		if _, ok := match[key]; ok {
			t.Errorf("the match page's data map should not carry %q", key)
		}
	}

	// The phase-content region's map: the region's cards' keys, with the
	// match reveal — the one card the region renders from its own data —
	// merged at the top level, and no evergreen card in sight.
	region := deps.handler.phaseContentData(me, deps.handler.missionView(me))
	for _, key := range []string{
		"Me", "Phase", "Match", "Score", // the reveal's data
		"MissionCard", "MeetStatus", "ShowTell",
	} {
		if _, ok := region[key]; !ok {
			t.Errorf("the phase-content region's data map should carry %q", key)
		}
	}
	for _, key := range []string{"Dossier", "Settings", "RecapLink", "PhaseHeader"} {
		if _, ok := region[key]; ok {
			t.Errorf("the phase-content region's data map should not carry %q", key)
		}
	}
}

// TestMatchStream_ANewRegistryEntryPushesItsCard proves #155's claim: the
// stream composes whatever signature sources the registry names, so a card
// added by a registry entry alone — the stream code is untouched — gets its
// push when its source changes. The test's entry watches the persona's
// name, which no card on the match page watches: the refresh below can
// only arrive through the new entry.
func TestMatchStream_ANewRegistryEntryPushesItsCard(t *testing.T) {
	pageCards = append(pageCards, pageCard{
		Name:     "test-card",
		OnMatch:  regionBody,
		Key:      "TestCard",
		Fragment: "fragment-persona-card.html",
		Data:     func(h *Handler, p *Participant, _ *MissionView) any { return p },
		Signature: func(h *Handler, p *Participant) string {
			return "persona:" + p.PersonaName + ";"
		},
	})
	t.Cleanup(func() { pageCards = pageCards[:len(pageCards)-1] })

	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	pair(t, deps.db, "a", "b")
	deps.events.Reveal()

	s := open(t, srv, "/user/match-stream/a")
	waitFor(t, s, "event: refresh") // the sync on connect
	before := s.String()
	sentNothingNew(t, s, 1, before, "nothing changed")

	setPersona(t, deps.db, "a", "The Renamed", "Ships things")
	waitForCount(t, s, "event: refresh", strings.Count(before, "event: refresh")+1)
}
