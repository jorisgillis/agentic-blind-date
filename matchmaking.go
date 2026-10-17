package main

import (
	"errors"
	"fmt"
	"sync"
)

// defaultMaxChain bounds a chain of take-overs. Take-overs usually end
// quickly because they're cached and each one raises the taken Participant's
// score, but a Reset clears the cache and LLM scores aren't perfectly
// consistent between calls, so a chain could otherwise loop forever while
// holding the matching lock. Restored by #48.
const defaultMaxChain = 100

// ErrMatchingFrozen is returned when the admin's Rematch is attempted from the
// social phase onward: matching freezes at the social, pairs are final.
var ErrMatchingFrozen = errors.New("matching is frozen")

// Matchmaking runs matching operations against the Pool one at a time: the
// admin's Rematch, and Continuous Matching for a Participant who just became
// ready. The Matcher decides who fits; the Relationship module records it.
// Matching freezes at the social: Rematch is rejected with ErrMatchingFrozen
// and Continuous Matching never takes over an existing Match from then on.
type Matchmaking struct {
	db           *DB
	events       *EventStore
	participants *ParticipantStore
	matcher      *Matcher
	relations    *Relationships
	maxChain     int
	mu           sync.Mutex // one matching operation at a time
}

// NewMatchmaking creates the Matchmaking module.
func NewMatchmaking(db *DB, matcher *Matcher, relations *Relationships, maxChain int) *Matchmaking {
	return &Matchmaking{
		db: db, events: NewEventStore(db), participants: NewParticipantStore(db),
		matcher: matcher, relations: relations, maxChain: maxChain,
	}
}

// Rematch breaks every Match and pairs all ready Participants again, as one
// matching operation: Continuous Matching cannot interleave with it. From the
// social onward matching is frozen and Rematch is rejected: pairs are final.
func (mm *Matchmaking) Rematch() error {
	if phase := mm.events.State(); phase.MatchingFrozen() {
		return fmt.Errorf("%w: the event is in the %s phase, pairs are final", ErrMatchingFrozen, phase)
	}

	mm.mu.Lock()
	defer mm.mu.Unlock()

	participants, err := mm.readyPool()
	if err != nil {
		return err
	}
	if err := mm.relations.UnpairAll(); err != nil {
		return err
	}
	mm.db.LogActivity("🔮 The matchmaker agents are at work...")

	if err := mm.storeAll(mm.matcher.MatchPool(participants)); err != nil {
		return err
	}

	mm.db.LogActivity("💞 Rematch complete")
	return nil
}

// readyPool fetches the ready Participants, requiring a pairable pool.
func (mm *Matchmaking) readyPool() ([]*Participant, error) {
	participants, err := mm.participants.AllByStep(StepReady)
	if err != nil {
		return nil, err
	}
	if len(participants) < 2 {
		return nil, fmt.Errorf("need at least 2 ready participants, got %d", len(participants))
	}
	return participants, nil
}

// storeAll records every Match through the Relationship module.
func (mm *Matchmaking) storeAll(matches []Match) error {
	for _, m := range matches {
		if _, err := mm.store(m); err != nil {
			return err
		}
	}
	return nil
}

// MatchNewcomer is Continuous Matching for a Participant who just became ready:
// they are matched against the other ready Participants, taking over a weaker
// Match when needed. A partner displaced by a take-over is matched straight
// away in the same way, but never with the pair that just displaced them.
// From the social onward matching is frozen: the newcomer is paired only with
// other unmatched ready Participants and never displaces a Match.
func (mm *Matchmaking) MatchNewcomer(newcomer *Participant) error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	type pending struct {
		id      string
		exclude map[string]bool // the pair that displaced them
	}
	queue := []pending{{id: newcomer.ID}}
	for steps := 0; len(queue) > 0; steps++ {
		if steps == mm.maxChain {
			return fmt.Errorf("matching %s: chain of take-overs longer than %d", newcomer.ID, mm.maxChain)
		}
		next := queue[0]
		queue = queue[1:]
		m, displaced, err := mm.matchOne(next.id, next.exclude)
		if err != nil {
			return err
		}
		for _, id := range displaced {
			queue = append(queue, pending{id: id, exclude: map[string]bool{m.A.ID: true, m.B.ID: true}})
		}
	}
	return nil
}

// matchOne finds a partner for one Participant among the ready Participants
// not excluded, stores the Match, and returns it with whoever it displaced.
// Once matching is frozen (from the social onward) matched Participants are
// not candidates: a latecomer pairs only with unmatched ready Participants.
func (mm *Matchmaking) matchOne(id string, exclude map[string]bool) (*Match, []string, error) {
	all, err := mm.participants.All()
	if err != nil {
		return nil, nil, fmt.Errorf("ParticipantStore.All: %w", err)
	}
	newcomer, others := splitPool(all, id, exclude, mm.events.State().MatchingFrozen())
	if newcomer == nil {
		return nil, nil, nil
	}
	mm.db.LogActivity(fmt.Sprintf("🔮 Matching %s against existing pool...", newcomer.PersonaName))

	m := mm.matcher.MatchNewcomer(newcomer, others)
	if m == nil {
		mm.db.LogActivity(fmt.Sprintf("⏳ %s is ready but no match available yet", newcomer.PersonaName))
		return nil, nil, nil
	}
	displaced, err := mm.store(*m)
	if err != nil {
		return nil, nil, err
	}
	mm.announceTakeOver(newcomer, m, displaced)
	return m, displaced, nil
}

// splitPool separates the Participant with the given id from their candidate
// pool: everyone else who is a candidate.
func splitPool(all []*Participant, id string, exclude map[string]bool, frozen bool) (newcomer *Participant, others []*Participant) {
	for _, p := range all {
		switch {
		case p.ID == id:
			newcomer = p
		case exclude[p.ID]:
		case isCandidate(p, frozen):
			others = append(others, p)
		}
	}
	return newcomer, others
}

// isCandidate reports whether p can be matched with: ready, and — once
// matching is frozen — not already matched.
func isCandidate(p *Participant, frozen bool) bool {
	return p.PipelineStep == StepReady && (!frozen || !p.IsMatched())
}

// announceTakeOver proclaims a take-over on the ticker.
func (mm *Matchmaking) announceTakeOver(newcomer *Participant, m *Match, displaced []string) {
	if len(displaced) > 0 {
		mm.db.LogActivity(fmt.Sprintf("🔄 %s took over %s's previous match", newcomer.PersonaName, m.B.PersonaName))
	}
}

// store records a Match through the Relationship module and returns the
// Participants it displaced from earlier Matches.
func (mm *Matchmaking) store(m Match) ([]string, error) {
	displaced, err := mm.relations.Pair(m)
	if err != nil {
		return nil, fmt.Errorf("storing match %s ↔ %s: %w", m.A.ID, m.B.ID, err)
	}
	mm.db.LogActivity(fmt.Sprintf("💘 %s ↔ %s (%d%%)", m.A.PersonaName, m.B.PersonaName, m.Result.Score))
	return displaced, nil
}
