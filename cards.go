package main

import (
	"fmt"
	"strings"
)

// The personal pages' card registry (ADR 0019): one entry per card, naming
// its placement — which page shells carry it and in which region — the
// fragment that renders it, its data function, and the signature source the
// streams watch to push it. The wait and match page data maps and the
// phase-content region derive from this table (#154); the streams' diff key
// composes the signature sources (#155) — so adding a card is one entry
// here plus one fragment.

// cardRegion is where a card lives on a personal page: the page's main
// column, the match page's phase-content region, or the "More" drawer.
type cardRegion string

const (
	regionBody   cardRegion = "body"   // the page's main column, above the drawer
	regionPhase  cardRegion = "phase"  // the match page's phase-content region
	regionDrawer cardRegion = "drawer" // the "More" drawer (ADR 0019, step 3)
)

// pageCard is one card of the personal pages. OnWait and OnMatch name the
// card's placement: the region it occupies on each page shell, or "" when
// that page does not carry it at all. Key is the data map key the card's
// data lands under; the cards their container renders from its own data —
// the wait page's status card, the phase-content region's match reveal —
// have no key: their data merges into the container's map. Signature names
// what the streams watch for the card's push; the cards that never refresh
// on their own (the settings, the region, the reveal) have none.
type pageCard struct {
	Name      string
	OnWait    cardRegion
	OnMatch   cardRegion
	Key       string
	Fragment  string
	Data      func(h *Handler, p *Participant, mission *MissionView) any
	Signature func(h *Handler, p *Participant) string
}

// pageCards is the registry, in the order the page shells render the cards
// — each shell and region picks its own subset by placement.
var pageCards = []pageCard{
	{
		Name: "phase-header", OnWait: regionBody, OnMatch: regionBody,
		Key: "PhaseHeader", Fragment: "fragment-phase-header.html",
		Data: func(h *Handler, p *Participant, mission *MissionView) any {
			return h.phaseHeaderData(p, mission)
		},
		Signature: func(h *Handler, p *Participant) string {
			return "phase:" + string(h.events.State()) + ";"
		},
	},
	{
		Name: "persona-card", OnWait: regionBody,
		Key: "Participant", Fragment: "fragment-persona-card.html",
		Data: func(h *Handler, p *Participant, _ *MissionView) any { return p },
		Signature: func(h *Handler, p *Participant) string {
			return "persona:" + p.PersonaName + ";"
		},
	},
	{
		Name: "slides", OnWait: regionBody, OnMatch: regionBody,
		Key: "Slides", Fragment: "fragment-slides.html",
		Data: func(h *Handler, p *Participant, _ *MissionView) any { return h.slidesData(p) },
		Signature: func(h *Handler, p *Participant) string {
			return "slides:" + h.slides.Signature() + ";"
		},
	},
	{
		Name: "wait-status", OnWait: regionBody,
		Fragment: "fragment-wait-status.html",
		Data:     func(h *Handler, p *Participant, _ *MissionView) any { return h.waitStatusData(p) },
		Signature: func(h *Handler, p *Participant) string {
			return fmt.Sprintf("status:ready=%d;matched=%t;", h.db.ReadyCount(), p.IsMatched())
		},
	},
	{
		Name: "mission-card", OnWait: regionBody, OnMatch: regionPhase,
		Key: "MissionCard", Fragment: "fragment-mission-card.html",
		Data: func(h *Handler, p *Participant, mission *MissionView) any {
			return h.missionCardSection(p, mission)
		},
		Signature: func(h *Handler, p *Participant) string {
			return fmt.Sprintf("mission:card=%t;", h.missions.Has(p.ID))
		},
	},
	{
		Name: "meet-status", OnWait: regionBody, OnMatch: regionPhase,
		Key: "MeetStatus", Fragment: "fragment-meet-status.html",
		Data: func(h *Handler, p *Participant, _ *MissionView) any { return h.meetStatusData(p) },
		Signature: func(h *Handler, p *Participant) string {
			ms, _ := h.meetings.ForParticipant(p.ID)
			completed := 0
			for _, m := range ms {
				if m.Completed() {
					completed++
				}
			}
			return fmt.Sprintf("meet:%d/%d;", len(ms), completed)
		},
	},
	{
		Name: "showtell", OnWait: regionBody, OnMatch: regionPhase,
		Key: "ShowTell", Fragment: "fragment-showtell.html",
		Data: func(h *Handler, p *Participant, _ *MissionView) any { return h.showtellData(p) },
		Signature: func(h *Handler, p *Participant) string {
			return "showtell:" + h.showtell.Signature(p.ID) + ";"
		},
	},
	{
		Name: "board", OnWait: regionBody, OnMatch: regionBody,
		Key: "Board", Fragment: "fragment-board.html",
		Data: func(h *Handler, p *Participant, _ *MissionView) any { return h.boardData(p) },
		Signature: func(h *Handler, p *Participant) string {
			return "board:" + h.board.Signature(p.ID) + ";"
		},
	},
	{
		Name: "dossier", OnMatch: regionDrawer,
		Key: "Dossier", Fragment: "fragment-dossier.html",
		Data: func(h *Handler, p *Participant, _ *MissionView) any {
			dossier, _ := h.dossierData(p)
			return dossier
		},
		Signature: func(h *Handler, p *Participant) string {
			return fmt.Sprintf("dossier:unlocked=%t;", h.meetings.DossierUnlocked(p.ID, p.MatchedWith))
		},
	},
	{
		Name: "settings", OnWait: regionDrawer, OnMatch: regionDrawer,
		Key: "Settings", Fragment: "fragment-settings.html",
		Data: func(h *Handler, p *Participant, _ *MissionView) any { return h.settingsData(p) },
	},
	{
		Name: "recap-link", OnWait: regionDrawer, OnMatch: regionDrawer,
		Key: "RecapLink", Fragment: "fragment-recap-link.html",
		Data: func(h *Handler, p *Participant, _ *MissionView) any { return h.recapLinkData(p) },
		Signature: func(h *Handler, p *Participant) string {
			return fmt.Sprintf("recap:closed=%t;", h.events.State() == PhaseClosed)
		},
	},
	{
		Name: "match-reveal", OnMatch: regionPhase,
		Fragment: "fragment-match-reveal.html",
		Data:     func(h *Handler, p *Participant, _ *MissionView) any { return h.revealData(p) },
	},
}

// waitPageData is the wait page's data map, derived from the card registry:
// every card placed on the wait page contributes its data — under its key,
// or merged at the top level for the status card, the one card the page
// renders from its own data (its data is the page map's base). The drawer's
// Q&A recap and GitHub summary are the shell's own markup, not cards: the
// Wait handler adds them.
func (h *Handler) waitPageData(p *Participant) map[string]any {
	data := map[string]any{}
	mission := h.missionView(p)
	for _, c := range pageCards {
		if c.OnWait == "" {
			continue
		}
		landCardData(data, c, c.Data(h, p, mission))
	}
	return data
}

// matchPageData is the match page's data map, derived from the card
// registry: every card placed on the match page outside the phase-content
// region — the body's cards and the drawer's evergreen cards — contributes
// its data under its key. The region's cards land in the region's own map,
// and the region rides in its page-level slot (ADR 0019, step 2) beside the
// page's Me — the owner's own page names them.
func (h *Handler) matchPageData(p *Participant) map[string]any {
	data := map[string]any{"Me": p}
	mission := h.missionView(p)
	for _, c := range pageCards {
		if c.OnMatch == "" || c.OnMatch == regionPhase {
			continue
		}
		landCardData(data, c, c.Data(h, p, mission))
	}
	data["PhaseContent"] = h.phaseContentData(p, mission)
	return data
}

// landCardData lands one card's data in its container's data map: under the
// card's key, or — for a card with no key, the one its container renders
// from its own data — merged at the top level.
func landCardData(data map[string]any, c pageCard, d any) {
	if c.Key == "" {
		for k, v := range d.(map[string]any) {
			data[k] = v
		}
		return
	}
	data[c.Key] = d
}

// onWaitPage reports whether the registry places the card on the wait page.
func onWaitPage(c pageCard) bool { return c.OnWait != "" }

// onMatchPage reports whether the registry places the card on the match page.
func onMatchPage(c pageCard) bool { return c.OnMatch != "" }

// pageSignature is what a personal page's stream watches (#155): the
// signature sources of every pushed card the registry places on the page,
// composed in registry order. The stream code no longer changes per card:
// a new card's push arrives because its registry entry names what to watch.
func (h *Handler) pageSignature(p *Participant, on func(pageCard) bool) string {
	var b strings.Builder
	for _, c := range pageCards {
		if c.Signature == nil || !on(c) {
			continue
		}
		b.WriteString(c.Signature(h, p))
	}
	return b.String()
}
