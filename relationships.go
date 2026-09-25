package main

import (
	"encoding/json"
	"fmt"
	"sync"
)

// Relationships owns Relationship State: who is matched with whom, and the
// assessment of each Match. It enforces the Key Invariant: a Participant has
// at most one partner. Pair, Remove and UnpairAll each read the current
// partner and then write the outcome as two separate store calls, so mu
// serializes them against each other — without it, a Remove interleaved
// between another goroutine's read and write could unpair or delete a
// Participant the other goroutine still thinks is a valid partner.
type Relationships struct {
	mu    sync.Mutex
	store *ParticipantStore
}

// NewRelationships creates the Relationship module.
func NewRelationships(db *DB) *Relationships {
	return &Relationships{store: NewParticipantStore(db)}
}

func strPtr(s string) *string { return &s }

// Pair records a Match between m.A and m.B, with its assessment on both
// sides, in one transaction. Any existing Match of either Participant is
// broken first; the partners left behind are returned to the Pool and
// reported as displaced.
func (r *Relationships) Pair(m Match) (displaced []string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sides := [][2]*Participant{{m.A, m.B}, {m.B, m.A}}
	var changes []ParticipantChange
	for _, side := range sides {
		p, err := r.store.Get(side[0].ID)
		if err != nil {
			return nil, err
		}
		if former := p.MatchedWith; former != "" && former != side[1].ID {
			changes = append(changes, ParticipantChange{ID: former, MatchedWith: strPtr(""), Match: &matchResult{}})
			displaced = append(displaced, former)
		}
	}
	for _, side := range sides {
		changes = append(changes, ParticipantChange{ID: side[0].ID, MatchedWith: strPtr(side[1].ID), Match: m.Result})
	}
	if err := r.store.Change(changes...); err != nil {
		return nil, err
	}
	return displaced, nil
}

// PartnerOf returns a Participant's partner and the assessment of their Match,
// or nils when they are unmatched. Lists that cannot be decoded come back empty.
func (r *Relationships) PartnerOf(id string) (*Participant, *matchResult, error) {
	p, err := r.store.Get(id)
	if err != nil {
		return nil, nil, err
	}
	if p.MatchedWith == "" {
		return nil, nil, nil
	}
	partner, err := r.store.Get(p.MatchedWith)
	if err != nil {
		return nil, nil, fmt.Errorf("partner of %s: %w", id, err)
	}
	return partner, &matchResult{
		Score:       p.CompatScore,
		Reason:      p.CompatReason,
		RedFlags:    decodeList(p.RedFlags),
		GreenFlags:  decodeList(p.GreenFlags),
		Icebreakers: decodeList(p.Icebreakers),
	}, nil
}

func decodeList(raw string) []string {
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil || list == nil {
		return []string{}
	}
	return list
}

// UnpairAll breaks every Match at once, returning everyone to the Pool.
func (r *Relationships) UnpairAll() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	all, err := r.store.All()
	if err != nil {
		return err
	}
	var changes []ParticipantChange
	for _, p := range all {
		if p.MatchedWith != "" {
			changes = append(changes, ParticipantChange{ID: p.ID, MatchedWith: strPtr(""), Match: &matchResult{}})
		}
	}
	if len(changes) == 0 {
		return nil
	}
	return r.store.Change(changes...)
}

// Remove deletes a Participant. Their partner, if any, is returned to the
// Pool, in the same transaction as the deletion.
func (r *Relationships) Remove(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, err := r.store.Get(id)
	if err != nil {
		return err
	}
	changes := []ParticipantChange{{ID: id, Delete: true}}
	if p.MatchedWith != "" {
		changes = append(changes, ParticipantChange{ID: p.MatchedWith, MatchedWith: strPtr(""), Match: &matchResult{}})
	}
	return r.store.Change(changes...)
}
