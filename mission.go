package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
	card, err := m.Get(id)
	if err != nil {
		return nil, err
	}
	if card != nil {
		return card, nil
	}
	return m.createCard(id)
}

// createCard generates, saves and announces a new card for the Participant.
func (m *Missions) createCard(id string) (*MissionCard, error) {
	me, others, err := m.cardPool(id)
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
	logger("mission").Debug("mission card generated", "participant", id, "tiles", len(card.Tiles))
	m.db.LogActivity(fmt.Sprintf("🃏 %s got a mission card", me.PersonaName))
	return card, nil
}

// cardPool fetches the Participant and the ready pool their card is built
// from.
func (m *Missions) cardPool(id string) (*Participant, []*Participant, error) {
	others, err := m.store.AllByStep(StepReady)
	if err != nil {
		return nil, nil, fmt.Errorf("mission card for %s: %w", id, err)
	}
	me, err := m.store.Get(id)
	if err != nil {
		return nil, nil, err
	}
	return me, others, nil
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
		logger("mission").Error("generating mission cards", "err", err)
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
				logger("mission").Warn("mission card", "participant", p.ID, "err", err)
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
	applyReasons(tiles, m.agent.Reasons(me, tilesAroundCenter(tiles), others))
	return &MissionCard{ParticipantID: me.ID, Tiles: tiles}, nil
}

// tilesAroundCenter lists the tiles around the center, in grid order.
func tilesAroundCenter(tiles []MissionTile) []MissionTile {
	var surrounding []MissionTile
	for _, t := range tiles {
		if t.Position != centerPosition {
			surrounding = append(surrounding, t)
		}
	}
	return surrounding
}

// applyReasons fills the surrounding tiles' reasons in grid order; the center
// keeps its static reason.
func applyReasons(tiles []MissionTile, reasons []string) {
	next := 0
	for i := range tiles {
		if tiles[i].Position == centerPosition {
			continue
		}
		tiles[i].Reason = reasons[next]
		next++
	}
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
	// COUNT(*) always yields one integer row: a failed read counts as zero.
	_ = m.db.db.QueryRow(`SELECT COUNT(*) FROM mission_cards WHERE participant_id = ?`, id).Scan(&n)
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

// MissionView is the mission card's read model: the card, its stamps and the
// Participant's connection count, derived together in one pass over their
// meetings — the count is the length of the stamp set, one connection per
// completed meeting.
type MissionView struct {
	Card        *MissionCard
	Stamped     map[string]bool
	Connections int
}

// View returns the Participant's mission card with its stamps and their
// connection count in a single pass over their meetings, so the personal
// page renders the phase header and the mission card from one result — no
// separate connection COUNT. Nil when they have no card. When the meetings
// cannot be read the view degrades to unstamped tiles with a zero count —
// the card still renders — and the failure is logged here.
func (m *Missions) View(id string) (*MissionView, error) {
	card, err := m.Get(id)
	if err != nil {
		return nil, err
	}
	if card == nil {
		return nil, nil
	}
	stamped, err := m.Stamped(id)
	if err != nil {
		logger("mission").Warn("mission view stamps", "participant", id, "err", err)
		stamped = map[string]bool{}
	}
	return &MissionView{Card: card, Stamped: stamped, Connections: len(stamped)}, nil
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

// tilePick is one surrounding tile's Person and why it is on the card.
type tilePick struct {
	p    *Participant
	kind MissionTileKind
}

// selectTiles picks me's 3×3 grid from the other ready Participants: the
// center (position 4) is their Match, or their top-affinity pick when
// unmatched; the 8 surrounding tiles are 4 affinity picks (the highest
// heuristic PairScore) and 4 wildcards (deliberately different — opposites
// attract). Me, my Match and everyone I already met (a completed meeting)
// are excluded from the surrounding tiles, and no person appears twice. The
// order is deterministic: score descending, then id.
func selectTiles(me *Participant, others []*Participant, met map[string]bool, score func(a, b *Participant) int) []MissionTile {
	candidates := tileCandidates(me, others, met)
	affinity := ranked(candidates, affinityScore(me, score))
	center, kind := pickCenter(me, others, affinity)
	return assembleTiles(center, kind, surroundingPicks(me, affinity, center))
}

// affinityScore adapts the pair heuristic into ranked's per-candidate score.
func affinityScore(me *Participant, score func(a, b *Participant) int) func(*Participant) int {
	return func(o *Participant) int { return score(me, o) }
}

// differenceScoreOf adapts the difference heuristic into ranked's
// per-candidate score.
func differenceScoreOf(me *Participant) func(*Participant) int {
	return func(o *Participant) int { return differenceScore(me, o) }
}

// tileCandidates lists the Participants who can be a surrounding tile: not
// me, not already met, not my Match (the center, not a surrounding tile).
func tileCandidates(me *Participant, others []*Participant, met map[string]bool) []*Participant {
	var candidates []*Participant
	for _, o := range others {
		if eligibleTileCandidate(me, o, met) {
			candidates = append(candidates, o)
		}
	}
	return candidates
}

func eligibleTileCandidate(me, o *Participant, met map[string]bool) bool {
	if o.ID == me.ID || met[o.ID] {
		return false
	}
	return !me.IsMatched() || o.ID != me.MatchedWith
}

// pickCenter picks the center: their Match when they have one in the pool,
// otherwise the top-affinity pick. Unmatched centers are TileAffinity.
func pickCenter(me *Participant, others, affinity []*Participant) (*Participant, MissionTileKind) {
	if match, ok := matchedCenter(me, others); ok {
		return match, TileMatch
	}
	if len(affinity) > 0 {
		return affinity[0], TileAffinity
	}
	return nil, TileAffinity
}

func matchedCenter(me *Participant, others []*Participant) (*Participant, bool) {
	if !me.IsMatched() {
		return nil, false
	}
	return participantByID(me.MatchedWith, others)
}

func participantByID(id string, all []*Participant) (*Participant, bool) {
	for _, o := range all {
		if o.ID == id {
			return o, true
		}
	}
	return nil, false
}

// surroundingPicks picks the 8 tiles around the center: 4 affinity picks,
// then 4 wildcards (deliberately different — opposites attract).
func surroundingPicks(me *Participant, affinity []*Participant, center *Participant) []tilePick {
	rest := excludingCenter(affinity, center)
	picks := take(rest, 4)
	wildcards := take(ranked(rest[len(picks):], differenceScoreOf(me)), 4)
	var out []tilePick
	for _, p := range picks {
		out = append(out, tilePick{p: p, kind: TileAffinity})
	}
	for _, p := range wildcards {
		out = append(out, tilePick{p: p, kind: TileWildcard})
	}
	return out
}

// excludingCenter drops the center from the affinity ranking.
func excludingCenter(affinity []*Participant, center *Participant) []*Participant {
	var rest []*Participant
	for _, o := range affinity {
		if center == nil || o.ID != center.ID {
			rest = append(rest, o)
		}
	}
	return rest
}

// assembleTiles lays the tiles out on the grid: the center at position 4,
// the picks around it in order, skipping the center square.
func assembleTiles(center *Participant, kind MissionTileKind, picks []tilePick) []MissionTile {
	var tiles []MissionTile
	if center != nil {
		tiles = append(tiles, newTile(centerPosition, kind, center, centerReason(kind)))
	}
	position := 0
	for _, pick := range picks {
		position = skipCenter(position)
		tiles = append(tiles, newTile(position, pick.kind, pick.p, ""))
		position++
	}
	return tiles
}

// skipCenter advances a surrounding position past the center square.
func skipCenter(position int) int {
	for position == centerPosition {
		position++
	}
	return position
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
	return languageDifference(a, b) + answerDifference(a, b)
}

// languageDifference counts the languages on exactly one side.
func languageDifference(a, b *Participant) int {
	aLangs, bLangs := languageSet(a), languageSet(b)
	n := 0
	for l := range aLangs {
		if !bLangs[l] {
			n++
		}
	}
	for l := range bLangs {
		if !aLangs[l] {
			n++
		}
	}
	return n
}

// answerDifference counts the interview questions the pair answered
// differently.
func answerDifference(a, b *Participant) int {
	bAnswers := answersOf(b)
	n := 0
	for k, av := range answersOf(a) {
		if bv, ok := bAnswers[k]; ok && comparableAnswer(k) && av != bv {
			n++
		}
	}
	return n
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
		logger("mission").Warn("mission agent reasons, using the generic reason", "participant", me.ID, "err", err)
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
		return validateMissionReasons(r.Reasons, len(tiles))
	})
	if err != nil {
		return nil, err
	}
	return trimmedReasons(reply.Reasons), nil
}

// validateMissionReasons rejects a reply that does not hold exactly one
// non-empty reason per tile.
func validateMissionReasons(reasons []string, want int) error {
	if len(reasons) != want {
		return fmt.Errorf("mission reply has %d reasons, want %d", len(reasons), want)
	}
	for _, reason := range reasons {
		if strings.TrimSpace(reason) == "" {
			return errors.New("mission reply has an empty reason")
		}
	}
	return nil
}

// trimmedReasons trims every reason, in place order.
func trimmedReasons(reasons []string) []string {
	trimmed := make([]string, len(reasons))
	for i, r := range reasons {
		trimmed[i] = strings.TrimSpace(r)
	}
	return trimmed
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
	fmt.Fprintf(&b, "MISSION FOR %s %s\nInterests: %s\nInterview answers:\n%s\n",
		me.PersonaSymbol, me.PersonaName, fmtInterests(me.Interests), interviewText(me))
	b.WriteString("\nTILES (one reason each, in this order):\n")
	for i, t := range tiles {
		fmt.Fprintf(&b, "%d. [%s] %s %s\n", i+1, t.Kind, t.Symbol, t.Name)
		if p, ok := byID[t.PersonID]; ok {
			fmt.Fprintf(&b, "   Interests: %s\n   Interview answers:\n%s\n", fmtInterests(p.Interests), indent(interviewText(p), "   "))
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
