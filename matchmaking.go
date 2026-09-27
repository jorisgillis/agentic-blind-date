package main

import (
	"fmt"
	"sync"
)

// defaultMaxChain bounds a chain of take-overs. Take-overs usually end
// quickly because they're cached and each one raises the taken Participant's
// score, but a Reset clears the cache and LLM scores aren't perfectly
// consistent between calls, so a chain could otherwise loop forever while
// holding the matching lock. Restored by #48.
const defaultMaxChain = 100

// Matchmaking runs matching operations against the Pool one at a time: the
// admin's Rematch, and Continuous Matching for a Participant who just became
// ready. The Matcher decides who fits; the Relationship module records it.
type Matchmaking struct {
	participants *ParticipantStore
	activity     *Activity
	matcher      *Matcher
	relations    *Relationships
	maxChain     int        // a field, not a const, so a test can shrink it
	mu           sync.Mutex // one matching operation at a time
}

// NewMatchmaking creates the Matchmaking module.
func NewMatchmaking(db *DB, matcher *Matcher, relations *Relationships) *Matchmaking {
	return &Matchmaking{
		participants: NewParticipantStore(db), activity: NewActivity(db),
		matcher: matcher, relations: relations, maxChain: defaultMaxChain,
	}
}

// WithLock runs fn as one matching operation: Continuous Matching cannot
// interleave with it, the same guarantee Rematch gives itself. Event
// administration uses this for Reset (#57).
func (mm *Matchmaking) WithLock(fn func() error) error {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	return fn()
}

// Rematch breaks every Match and pairs all ready Participants again, as one
// matching operation: Continuous Matching cannot interleave with it.
func (mm *Matchmaking) Rematch() error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	participants, err := mm.participants.AllByStep(StepReady)
	if err != nil {
		return err
	}
	if len(participants) < 2 {
		return fmt.Errorf("need at least 2 ready participants, got %d", len(participants))
	}
	if err := mm.relations.UnpairAll(); err != nil {
		return err
	}
	mm.activity.MatchmakingStarted()

	for _, m := range mm.matcher.MatchPool(participants) {
		if _, err := mm.store(m); err != nil {
			return err
		}
	}

	mm.activity.RematchComplete()
	return nil
}

// RunGameShowMatchmaking pairs every ready Participant at once, through the
// Matcher's cache-only Pool matching: no new LLM calls, since background
// assessment (#81) already populated the cache for anyone assessed so far.
// An odd Participant out becomes the wildcard instead of being paired (#82).
func (mm *Matchmaking) RunGameShowMatchmaking() error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	participants, err := mm.participants.AllByStep(StepReady)
	if err != nil {
		return err
	}
	mm.activity.MatchmakingStarted()

	taken := map[string]bool{}
	for _, m := range mm.matcher.MatchPoolFromCache(participants) {
		if _, err := mm.store(m); err != nil {
			return err
		}
		taken[m.A.ID], taken[m.B.ID] = true, true
	}

	for _, p := range participants {
		if taken[p.ID] {
			continue
		}
		if err := mm.participants.Change(ParticipantChange{ID: p.ID, Wildcard: boolPtr(true)}); err != nil {
			return err
		}
		mm.activity.Wildcard(p.PersonaName)
	}

	mm.activity.MatchmakingComplete()
	return nil
}

// AssessCandidates triggers background, throttled LLM assessment of a
// newly-ready Participant's candidate Pairs against the other ready
// Participants, populating the Matcher's cache. Unlike Continuous Matching,
// this doesn't pair anyone and doesn't need the matching lock (#81).
func (mm *Matchmaking) AssessCandidates(p *Participant) error {
	ready, err := mm.participants.AllByStep(StepReady)
	if err != nil {
		return err
	}
	mm.matcher.AssessCandidates(p, ready)
	return nil
}

// MatchNewcomer is Continuous Matching for a Participant who just became ready:
// they are matched against the other ready Participants, taking over a weaker
// Match when needed. A partner displaced by a take-over is matched straight
// away in the same way, but never with the pair that just displaced them.
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
func (mm *Matchmaking) matchOne(id string, exclude map[string]bool) (*Match, []string, error) {
	all, err := mm.participants.All()
	if err != nil {
		return nil, nil, fmt.Errorf("GetAllParticipants: %w", err)
	}
	var newcomer *Participant
	var others []*Participant
	for _, p := range all {
		switch {
		case p.ID == id:
			newcomer = p
		case exclude[p.ID]:
		case p.PipelineStep == StepReady:
			others = append(others, p)
		}
	}
	if newcomer == nil {
		return nil, nil, nil
	}
	mm.activity.MatchingAgainstPool(newcomer.PersonaName)

	m := mm.matcher.MatchNewcomer(newcomer, others)
	if m == nil {
		mm.activity.NoMatchYet(newcomer.PersonaName)
		return nil, nil, nil
	}
	displaced, err := mm.store(*m)
	if err != nil {
		return nil, nil, err
	}
	if len(displaced) > 0 {
		mm.activity.TookOver(newcomer.PersonaName, m.B.PersonaName)
	}
	return m, displaced, nil
}

// store records a Match through the Relationship module and returns the
// Participants it displaced from earlier Matches.
func (mm *Matchmaking) store(m Match) ([]string, error) {
	displaced, err := mm.relations.Pair(m)
	if err != nil {
		return nil, fmt.Errorf("storing match %s ↔ %s: %w", m.A.ID, m.B.ID, err)
	}
	mm.activity.Matched(m.A.PersonaName, m.B.PersonaName, m.Result.Score, m.Genuine)
	return displaced, nil
}
