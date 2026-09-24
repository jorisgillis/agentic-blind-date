package main

import (
	"fmt"
	"sync"
)

// Matchmaking runs matching operations against the Pool one at a time: the
// admin's Rematch, and Continuous Matching for a Participant who just became
// ready. The Matcher decides who fits; the Relationship module records it.
type Matchmaking struct {
	db        *DB
	matcher   *Matcher
	relations *Relationships
	mu        sync.Mutex // one matching operation at a time
}

// NewMatchmaking creates the Matchmaking module.
func NewMatchmaking(db *DB, matcher *Matcher, relations *Relationships) *Matchmaking {
	return &Matchmaking{db: db, matcher: matcher, relations: relations}
}

// Rematch breaks every Match and pairs all ready Participants again, as one
// matching operation: Continuous Matching cannot interleave with it.
func (mm *Matchmaking) Rematch() error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	if err := mm.relations.UnpairAll(); err != nil {
		return err
	}
	mm.db.LogActivity("🔮 The matchmaker agents are at work...")

	participants, err := mm.db.GetAllByStep(StepReady)
	if err != nil {
		return err
	}
	if len(participants) < 2 {
		return fmt.Errorf("need at least 2 ready participants, got %d", len(participants))
	}

	for _, m := range mm.matcher.MatchPool(participants) {
		if _, err := mm.store(m); err != nil {
			return err
		}
	}

	mm.db.LogActivity("💞 Rematch complete")
	return nil
}

// MatchNewcomer is Continuous Matching for a Participant who just became ready:
// they are matched against the other ready Participants, taking over a weaker
// Match when needed. A partner displaced by a take-over is matched straight
// away in the same way. Participants already paired in this chain are not
// taken over again, so the chain ends.
func (mm *Matchmaking) MatchNewcomer(newcomer *Participant) error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	inChain := map[string]bool{}
	queue := []string{newcomer.ID}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		displaced, err := mm.matchOne(id, inChain)
		if err != nil {
			return err
		}
		queue = append(queue, displaced...)
	}
	return nil
}

// matchOne finds a partner for one Participant, skipping those paired earlier
// in the chain, and returns whoever the new Match displaced.
func (mm *Matchmaking) matchOne(id string, inChain map[string]bool) ([]string, error) {
	all, err := mm.db.GetAllParticipants()
	if err != nil {
		return nil, fmt.Errorf("GetAllParticipants: %w", err)
	}
	var newcomer *Participant
	var others []*Participant
	for _, p := range all {
		switch {
		case p.ID == id:
			newcomer = p
		case inChain[p.ID]:
		case p.PipelineStep == StepReady:
			others = append(others, p)
		}
	}
	if newcomer == nil {
		return nil, nil
	}
	mm.db.LogActivity(fmt.Sprintf("🔮 Matching %s against existing pool...", newcomer.PersonaName))

	m := mm.matcher.MatchNewcomer(newcomer, others)
	if m == nil {
		mm.db.LogActivity(fmt.Sprintf("⏳ %s is ready but no match available yet", newcomer.PersonaName))
		return nil, nil
	}
	displaced, err := mm.store(*m)
	if err != nil {
		return nil, err
	}
	inChain[m.A.ID], inChain[m.B.ID] = true, true
	if len(displaced) > 0 {
		mm.db.LogActivity(fmt.Sprintf("🔄 %s took over %s's previous match", newcomer.PersonaName, m.B.PersonaName))
	}
	return displaced, nil
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
