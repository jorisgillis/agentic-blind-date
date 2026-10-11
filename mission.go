package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

// centerPosition is the middle square of the 3×3 grid.
const centerPosition = 4

// MissionTileKind says why a tile is on the card.
type MissionTileKind string

// The tile kinds.
const (
	// TileMatch is the center tile: the Participant's Match.
	TileMatch MissionTileKind = "match"
	// TileAffinity is a top heuristic pick: one of the highest PairScores.
	TileAffinity MissionTileKind = "affinity"
	// TileWildcard is a deliberately different pick: opposites attract.
	TileWildcard MissionTileKind = "wildcard"
)

// MissionTile is one square of a Mission card: the Persona to meet, where it
// sits on the 3×3 grid, and why (the Mission Agent's one-liner). The Persona
// is snapshotted at generation — the card is fixed then, so it renders even
// if that Participant later leaves the event.
type MissionTile struct {
	Position int             `json:"position"` // 0-8, row-major; 4 is the center
	Kind     MissionTileKind `json:"kind"`
	PersonID string          `json:"person_id"`
	Name     string          `json:"persona_name"`
	Symbol   string          `json:"persona_symbol"`
	Color    string          `json:"persona_color"`
	Reason   string          `json:"reason"`
}

// MissionCard is a Participant's 3×3 hunting ground for the evening: the
// Personas to meet, fixed at generation (no regeneration when pools change).
// Stamps are not stored: a tile is stamped when a completed meeting exists
// with that person.
type MissionCard struct {
	ParticipantID string        `json:"participant_id"`
	Tiles         []MissionTile `json:"tiles"`
	CreatedAt     time.Time     `json:"created_at"`
}

// missionWorkers is how many Mission cards are generated concurrently — a
// small number of LLM calls at a time, like the matcher's assessment
// batching.
var missionWorkers = 4

// Missions owns the mission_cards table: each ready Participant's 3×3 Mission
// card, generated at the social transition and for latecomers during the
// social and show & tell. Card generation never writes pairing data
// (matched_with); its single writer stays the Relationship module.
type Missions struct {
	db       *DB
	events   *EventStore
	store    *ParticipantStore
	matcher  *Matcher
	meetings *Meetings
	agent    *MissionAgent
}

// NewMissions creates the Missions module.
func NewMissions(db *DB, matcher *Matcher, llm LLM) *Missions {
	events := NewEventStore(db)
	return &Missions{
		db: db, events: events, store: NewParticipantStore(db),
		matcher: matcher, meetings: NewMeetings(db, events),
		agent: NewMissionAgent(llm),
	}
}

// GenerateFor generates the Participant's Mission card when the event allows
// it — during the social and show & tell, never during onboarding or once
// closed — and returns it, or nil when the event does not allow cards. It is
// idempotent: a Participant who already has a card keeps it (the card is
// fixed at generation).
func (m *Missions) GenerateFor(id string) (*MissionCard, error) {
	if !m.events.State().MissionCardsLive() {
		return nil, nil
	}
	existing, err := m.Get(id)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}
	others, err := m.store.AllByStep(StepReady)
	if err != nil {
		return nil, fmt.Errorf("mission card for %s: %w", id, err)
	}
	me, err := m.store.Get(id)
	if err != nil {
		return nil, err
	}
	card, err := m.generate(me, others)
	if err != nil {
		return nil, err
	}
	if err := m.save(card); err != nil {
		return nil, err
	}
	m.db.LogActivity(fmt.Sprintf("🃏 %s got a mission card", me.PersonaName))
	return card, nil
}

// GenerateAll generates a Mission card for every ready Participant, in a
// background worker pool — a small number of concurrent LLM calls, like the
// matcher's assessment batching — so the phase transition returns
// immediately. Cards appear on personal pages via push.
func (m *Missions) GenerateAll() {
	go m.generateAll()
}

// generateAll runs the worker pool. GenerateFor re-checks the phase and the
// existing card per Participant, so a transition racing this pool stays
// correct.
func (m *Missions) generateAll() {
	ready, err := m.store.AllByStep(StepReady)
	if err != nil {
		log.Printf("Mission cards: %v", err)
		return
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, missionWorkers)
	for _, p := range ready {
		wg.Add(1)
		go func(p *Participant) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if _, err := m.GenerateFor(p.ID); err != nil {
				log.Printf("Mission card for %s: %v", p.ID, err)
			}
		}(p)
	}
	wg.Wait()
	if len(ready) > 0 {
		m.db.LogActivity(fmt.Sprintf("🃏 The Mission Agent dealt %d mission cards", len(ready)))
	}
}

// generate builds one Participant's Mission card from the ready pool: the
// tiles, then the Mission Agent's one batched call for the 8 surrounding
// tiles' reasons.
func (m *Missions) generate(me *Participant, others []*Participant) (*MissionCard, error) {
	stamped, err := m.Stamped(me.ID)
	if err != nil {
		return nil, fmt.Errorf("mission card for %s: %w", me.ID, err)
	}
	tiles := selectTiles(me, others, stamped, m.matcher.PairScore)

	// One batched LLM call for the surrounding tiles' reasons, in grid
	// order; on failure the generic reason stands — card generation never
	// blocks on the LLM.
	var surrounding []MissionTile
	for _, t := range tiles {
		if t.Position != centerPosition {
			surrounding = append(surrounding, t)
		}
	}
	reasons := m.agent.Reasons(me, surrounding, others)
	next := 0
	for i := range tiles {
		if tiles[i].Position == centerPosition {
			continue
		}
		tiles[i].Reason = reasons[next]
		next++
	}

	return &MissionCard{ParticipantID: me.ID, Tiles: tiles}, nil
}

// save stores the card, announcing it on the change feed so personal pages
// pick it up via push. The insert is idempotent (the first card wins): two
// concurrent generations for the same Participant cannot both land.
func (m *Missions) save(card *MissionCard) error {
	encoded, _ := json.Marshal(card.Tiles) // plain data: cannot fail
	_, err := m.db.db.Exec(`INSERT OR IGNORE INTO mission_cards (participant_id, tiles) VALUES (?, ?)`, card.ParticipantID, string(encoded))
	if err == nil {
		m.db.changed()
	}
	return err
}

// Get returns the Participant's Mission card, or nil when they have none.
func (m *Missions) Get(id string) (*MissionCard, error) {
	card := &MissionCard{ParticipantID: id}
	var tilesJSON string
	err := m.db.db.QueryRow(`SELECT tiles, created_at FROM mission_cards WHERE participant_id = ?`, id).Scan(&tilesJSON, &card.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(tilesJSON), &card.Tiles); err != nil {
		return nil, fmt.Errorf("mission card for %s: %w", id, err)
	}
	return card, nil
}

// Has reports whether the Participant has a Mission card — the streams watch
// it, so a freshly generated card appears on the personal page via push.
func (m *Missions) Has(id string) bool {
	var n int
	m.db.db.QueryRow(`SELECT COUNT(*) FROM mission_cards WHERE participant_id = ?`, id).Scan(&n)
	return n > 0
}

// Stamped returns the ids of the Participants whose tiles on id's card are
// stamped: everyone they have a completed meeting with. Stamps derive from
// the meetings — there is no stamped_at column; the meeting is the stamp.
// Tiles are guidance, not gates: meeting anyone counts, not only tile people.
func (m *Missions) Stamped(id string) (map[string]bool, error) {
	meetings, err := m.meetings.ForParticipant(id)
	if err != nil {
		return nil, err
	}
	stamped := map[string]bool{}
	for _, meeting := range meetings {
		if meeting.Completed() {
			stamped[meeting.Other(id)] = true
		}
	}
	return stamped, nil
}

// RemoveParticipant deletes a Participant's Mission card when they leave the
// event.
func (m *Missions) RemoveParticipant(id string) error {
	_, err := m.db.db.Exec(`DELETE FROM mission_cards WHERE participant_id = ?`, id)
	if err == nil {
		m.db.changed()
	}
	return err
}

// selectTiles picks me's 3×3 grid from the other ready Participants: the
// center (position 4) is their Match, or their top-affinity pick when
// unmatched; the 8 surrounding tiles are 4 affinity picks (the highest
// heuristic PairScore) and 4 wildcards (deliberately different — opposites
// attract). Me, my Match and everyone I already met (a completed meeting)
// are excluded from the surrounding tiles, and no person appears twice. The
// order is deterministic: score descending, then id.
func selectTiles(me *Participant, others []*Participant, met map[string]bool, score func(a, b *Participant) int) []MissionTile {
	byID := make(map[string]*Participant, len(others))
	var candidates []*Participant
	for _, o := range others {
		byID[o.ID] = o
		switch {
		case o.ID == me.ID, met[o.ID], me.IsMatched() && o.ID == me.MatchedWith:
			// Me, the already-met and my Match (the center, not a
			// surrounding tile) are not candidates.
		default:
			candidates = append(candidates, o)
		}
	}

	// The center: their Match, or the top-affinity pick when unmatched.
	var center *Participant
	centerKind := TileAffinity
	if me.IsMatched() {
		if match, ok := byID[me.MatchedWith]; ok {
			center, centerKind = match, TileMatch
		}
	}
	affinity := ranked(candidates, func(o *Participant) int { return score(me, o) })
	if center == nil && len(affinity) > 0 {
		center = affinity[0]
	}

	// The 8 surrounding tiles: 4 affinity picks, then 4 wildcards.
	var rest []*Participant
	for _, o := range affinity {
		if center != nil && o.ID == center.ID {
			continue
		}
		rest = append(rest, o)
	}
	picks := take(rest, 4)
	wildcards := take(ranked(rest[len(picks):], func(o *Participant) int { return differenceScore(me, o) }), 4)

	var tiles []MissionTile
	if center != nil {
		tiles = append(tiles, newTile(centerPosition, centerKind, center, centerReason(centerKind)))
	}
	position := 0
	for _, group := range []struct {
		picks []*Participant
		kind  MissionTileKind
	}{{picks, TileAffinity}, {wildcards, TileWildcard}} {
		for _, o := range group.picks {
			for position == centerPosition {
				position++
			}
			tiles = append(tiles, newTile(position, group.kind, o, ""))
			position++
		}
	}
	return tiles
}

// ranked orders the Participants by score descending, ties broken by id, so
// the card is deterministic whatever order the pool arrives in.
func ranked(participants []*Participant, score func(*Participant) int) []*Participant {
	out := append([]*Participant(nil), participants...)
	sort.Slice(out, func(i, j int) bool {
		si, sj := score(out[i]), score(out[j])
		if si != sj {
			return si > sj
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// take returns the first n Participants.
func take(participants []*Participant, n int) []*Participant {
	if len(participants) > n {
		return participants[:n]
	}
	return participants
}

// newTile snapshots the Persona onto a tile.
func newTile(position int, kind MissionTileKind, p *Participant, reason string) MissionTile {
	return MissionTile{
		Position: position, Kind: kind, PersonID: p.ID,
		Name: p.PersonaName, Symbol: p.PersonaSymbol, Color: p.PersonaColor,
		Reason: reason,
	}
}

// centerReason is the center tile's static reason: the Mission Agent writes
// the 8 surrounding tiles' reasons; the center is their Match (or their best
// fit), and says so.
func centerReason(kind MissionTileKind) string {
	if kind == TileMatch {
		return "Your match — find them in the room!"
	}
	return "Your best fit — start here"
}

// differenceScore rates how unlike two Participants are: +1 per language on
// exactly one side, +1 per interview question the pair answered differently.
// Wildcard tiles are the deliberately different picks — opposites attract.
func differenceScore(a, b *Participant) int {
	score := 0
	aLangs, bLangs := languageSet(a), languageSet(b)
	for l := range aLangs {
		if !bLangs[l] {
			score++
		}
	}
	for l := range bLangs {
		if !aLangs[l] {
			score++
		}
	}
	aAnswers, bAnswers := a.Answers, b.Answers
	if aAnswers == nil {
		aAnswers = map[string]string{}
	}
	if bAnswers == nil {
		bAnswers = map[string]string{}
	}
	for k, av := range aAnswers {
		if bv, ok := bAnswers[k]; ok && av != bv {
			score++
		}
	}
	return score
}

func languageSet(p *Participant) map[string]bool {
	set := map[string]bool{}
	if p.Profile != nil {
		for _, l := range p.Profile.Languages {
			set[l] = true
		}
	}
	return set
}

// MissionAgent is the LLM behind a Mission card's reasons: one batched call
// per Participant generating a one-line reason per surrounding tile. It never
// blocks card generation: on failure the generic reason stands.
type MissionAgent struct {
	llm LLM
}

// NewMissionAgent creates the Mission Agent.
func NewMissionAgent(llm LLM) *MissionAgent {
	return &MissionAgent{llm: llm}
}

// genericMissionReason is the fallback when the Mission Agent cannot answer.
const genericMissionReason = "The Mission Agent is offline — trust your gut and say hi."

// missionReasonsReply is the Mission Agent's raw reply shape: one reason per
// surrounding tile, in tile order.
type missionReasonsReply struct {
	Reasons []string `json:"reasons"`
}

// Reasons writes one one-line reason per tile — the Mission Agent's single
// batched LLM call per Participant. On any failure, or a reply that does not
// hold one reason per tile, the generic reason stands for every tile.
func (a *MissionAgent) Reasons(me *Participant, tiles []MissionTile, others []*Participant) []string {
	reasons, err := a.ask(me, tiles, others)
	if err != nil {
		log.Printf("Mission Agent reasons for %s: %v", me.ID, err)
		return genericReasons(len(tiles))
	}
	return reasons
}

// ask makes the one batched call and validates the reply: exactly one
// non-empty reason per tile.
func (a *MissionAgent) ask(me *Participant, tiles []MissionTile, others []*Participant) ([]string, error) {
	system := `You are the Mission Agent at a tech meetup blind date event.
A Participant gets a 3×3 mission card of Personas to meet. For each tile, write one funny, tongue-in-cheek one-line reason (max 80 chars) why they should go meet that Persona.
Affinity tiles: play up what the pair has in common. Wildcard tiles: play up the contrast — opposites attract.
Respond with ONLY valid JSON — no markdown:
{"reasons": ["<one line per tile, in tile order>", ...]}
`
	reply, err := AskStructured(a.llm, system, missionPrompt(me, tiles, others), func(r missionReasonsReply) error {
		if len(r.Reasons) != len(tiles) {
			return fmt.Errorf("mission reply has %d reasons, want %d", len(r.Reasons), len(tiles))
		}
		for _, reason := range r.Reasons {
			if strings.TrimSpace(reason) == "" {
				return errors.New("mission reply has an empty reason")
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	reasons := make([]string, len(reply.Reasons))
	for i, r := range reply.Reasons {
		reasons[i] = strings.TrimSpace(r)
	}
	return reasons, nil
}

// missionPrompt describes the Participant and every tile's Persona for the
// Mission Agent: persona names, interests and answers — never handles, the
// card identifies people in the room.
func missionPrompt(me *Participant, tiles []MissionTile, others []*Participant) string {
	byID := make(map[string]*Participant, len(others))
	for _, o := range others {
		byID[o.ID] = o
	}
	var b strings.Builder
	fmt.Fprintf(&b, "MISSION FOR %s %s\nInterests: %s\nInterview answers: %v\n",
		me.PersonaSymbol, me.PersonaName, fmtInterests(me.Interests), me.Answers)
	b.WriteString("\nTILES (one reason each, in this order):\n")
	for i, t := range tiles {
		fmt.Fprintf(&b, "%d. [%s] %s %s\n", i+1, t.Kind, t.Symbol, t.Name)
		if p, ok := byID[t.PersonID]; ok {
			fmt.Fprintf(&b, "   Interests: %s\n   Interview answers: %v\n", fmtInterests(p.Interests), p.Answers)
		}
	}
	return b.String()
}

// genericReasons is the fallback when the Mission Agent cannot answer: one
// generic reason per tile.
func genericReasons(n int) []string {
	reasons := make([]string, n)
	for i := range reasons {
		reasons[i] = genericMissionReason
	}
	return reasons
}
