package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Interests describes what a Participant works with: languages, tools and
// domains, computed from GitHub data or filled in from ExtraAnswers.
type Interests struct {
	Languages []string `json:"languages"`
	Tools     []string `json:"tools"`
	Domains   []string `json:"domains"`
}

// ParticipantStore reads and changes Participants through a small, typed
// interface, instead of DB's roughly two dozen one-line per-column methods.
// It announces every change on DB's change feed and distinguishes a
// Participant that does not exist (ErrParticipantNotFound) from a database
// failure. This is the "expand" step of an expand-migrate-contract refactor
// (#49): nothing else uses it yet, and DB's existing methods keep working.
type ParticipantStore struct {
	db *DB
}

// NewParticipantStore creates the Participant store.
func NewParticipantStore(db *DB) *ParticipantStore {
	return &ParticipantStore{db: db}
}

// ErrParticipantNotFound is returned when a read or change names a
// Participant who does not exist.
var ErrParticipantNotFound = errors.New("participant not found")

// Get reads one Participant by ID.
func (s *ParticipantStore) Get(id string) (*Participant, error) {
	p, err := s.db.GetParticipant(id)
	if err != nil {
		return nil, notFoundOr(err, id)
	}
	return p, nil
}

// All reads every Participant, in the order they registered.
func (s *ParticipantStore) All() ([]*Participant, error) {
	p, err := s.db.GetAllParticipants()
	if err != nil {
		return nil, fmt.Errorf("participant store: %w", err)
	}
	return p, nil
}

// Create registers a new Participant, picking a Persona colour and symbol
// not yet overused.
func (s *ParticipantStore) Create(id, handle, name string, hasGitHub bool) error {
	if err := s.db.CreateParticipant(id, handle, name, hasGitHub); err != nil {
		return fmt.Errorf("participant store: %w", err)
	}
	return nil
}

// ParticipantChange changes one Participant: nil (or false, for Delete)
// leaves that part alone. Delete removes the Participant outright; every
// other field is ignored when it is set.
type ParticipantChange struct {
	ID          string
	Delete      bool
	Interests   *Interests
	Match       *matchResult // the Participant's own side of their current Match
	MatchedWith *string      // who they're matched with now; "" clears it
}

// Change applies one or more Participant changes in a single transaction,
// announcing the change once it commits. Changing an unknown Participant
// fails the whole transaction with ErrParticipantNotFound; any other
// database failure fails it too, leaving every change unapplied.
func (s *ParticipantStore) Change(changes ...ParticipantChange) error {
	return s.db.inTx(func(tx *sql.Tx) error {
		for _, c := range changes {
			if c.Delete {
				if err := execFound(tx, c.ID, `DELETE FROM participants WHERE id = ?`, c.ID); err != nil {
					return err
				}
				continue
			}
			if c.Interests != nil {
				encoded, _ := json.Marshal(c.Interests) // plain data: cannot fail
				if err := execFound(tx, c.ID, `UPDATE participants SET interests = ? WHERE id = ?`, string(encoded), c.ID); err != nil {
					return err
				}
			}
			if c.Match != nil {
				red, green, ice := encodeMatchResult(c.Match)
				if err := execFound(tx, c.ID, `
					UPDATE participants SET compat_score = ?, compat_reason = ?,
					    red_flags = ?, green_flags = ?, icebreakers = ?
					WHERE id = ?`, c.Match.Score, c.Match.Reason, red, green, ice, c.ID); err != nil {
					return err
				}
			}
			if c.MatchedWith != nil {
				if err := execFound(tx, c.ID, `UPDATE participants SET matched_with = ? WHERE id = ?`, *c.MatchedWith, c.ID); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// execFound runs a statement expected to touch the Participant id and
// reports ErrParticipantNotFound when it touches no row instead,
// distinguishing an unknown Participant from a failure.
func execFound(tx *sql.Tx, id, query string, args ...any) error {
	res, err := tx.Exec(query, args...)
	if err != nil {
		return err
	}
	if rowsAffected(res) == 0 {
		return fmt.Errorf("%w: %s", ErrParticipantNotFound, id)
	}
	return nil
}

// notFoundOr distinguishes an unknown Participant from a database failure.
func notFoundOr(err error, id string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrParticipantNotFound, id)
	}
	return fmt.Errorf("participant store: %w", err)
}

// decodeMatchResult decodes a Match assessment's score, reason and three
// flag/icebreaker lists. If any list is undecodable, the whole assessment is
// invalid, so a legacy or corrupt row is rejected outright rather than
// half-served — the rule the LLM cache uses, since a bad cache entry simply
// means the pair gets re-scored. A Participant's own current Match is read
// more leniently (decodeList, in relationships.go): once a Match is made,
// one corrupted flag list shouldn't make the Match itself unreadable.
func decodeMatchResult(score int, reason, red, green, ice string) (*matchResult, error) {
	r := &matchResult{Score: score, Reason: reason}
	for _, f := range []struct {
		raw string
		dst *[]string
	}{{red, &r.RedFlags}, {green, &r.GreenFlags}, {ice, &r.Icebreakers}} {
		if err := json.Unmarshal([]byte(f.raw), f.dst); err != nil {
			return nil, err
		}
		if *f.dst == nil {
			*f.dst = []string{}
		}
	}
	return r, nil
}

// encodeMatchResult encodes a Match assessment's three lists for storage.
// Shared by the Participant table (Change) and the LLM cache (SetLLMCache).
func encodeMatchResult(r *matchResult) (red, green, ice string) {
	encode := func(list []string) string {
		b, _ := json.Marshal(nonNil(list)) // a []string always marshals
		return string(b)
	}
	return encode(r.RedFlags), encode(r.GreenFlags), encode(r.Icebreakers)
}
