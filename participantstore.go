package main

import (
	"context"
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
// failure. Introduced as the "expand" step of an expand-migrate-contract
// refactor (#49); the Relationship, Onboarding and Interview modules now
// migrate onto it in turn (#50, #51), while DB's old per-column methods
// keep working until the final "contract" step removes them (#53).
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

// AllByStep reads every Participant at the given Pipeline Step, in the order
// they registered.
func (s *ParticipantStore) AllByStep(step Step) ([]*Participant, error) {
	p, err := s.db.GetAllByStep(step)
	if err != nil {
		return nil, fmt.Errorf("participant store: %w", err)
	}
	return p, nil
}

// GetByHandle reads one Participant by their GitHub handle.
func (s *ParticipantStore) GetByHandle(handle string) (*Participant, error) {
	p, err := s.db.GetParticipantByHandle(handle)
	if err != nil {
		return nil, notFoundOr(err, handle)
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

// ParticipantChange changes one Participant: a nil (or false, for Delete)
// field leaves that part alone. Delete removes the Participant outright;
// every other field is ignored when it is set.
type ParticipantChange struct {
	ID           string
	Delete       bool
	Interests    *Interests
	Match        *matchResult // the Participant's own side of their current Match
	MatchedWith  *string      // who they're matched with now; "" clears it
	Persona      *Persona
	Profile      *GitHubProfile
	Answers      map[string]string
	PipelineStep *Step // guarded exactly like DB.AdvanceStep: only from the step right before it
}

// tag names what kind of change this is, for the test-only fault hook
// (faults_test.go): each ParticipantChange in production sets exactly one
// concern, so one tag identifies the whole change.
func (c ParticipantChange) tag() string {
	switch {
	case c.Delete:
		return "delete"
	case c.MatchedWith != nil:
		if *c.MatchedWith == "" {
			return "unpair"
		}
		return "pair"
	case c.Persona != nil:
		return "persona"
	case c.Profile != nil:
		return "profile"
	case c.Interests != nil:
		return "interests"
	case c.Answers != nil:
		return "answers"
	case c.PipelineStep != nil:
		return "pipeline_step"
	default:
		return ""
	}
}

// Change applies one or more Participant changes in a single transaction,
// announcing the change once it commits. Changing an unknown Participant
// fails the whole transaction with ErrParticipantNotFound; any other
// database failure fails it too, leaving every change unapplied.
func (s *ParticipantStore) Change(changes ...ParticipantChange) error {
	return s.db.inTx(func(tx *sql.Tx) error {
		for _, c := range changes {
			if err := s.db.checkFault(c.tag()); err != nil {
				return err
			}
			if c.Delete {
				if err := execFound(s.db.execCtx(), tx, c.ID, `DELETE FROM participants WHERE id = ?`, c.ID); err != nil {
					return err
				}
				continue
			}
			if c.Interests != nil {
				encoded, _ := json.Marshal(c.Interests) // plain data: cannot fail
				if err := execFound(s.db.execCtx(), tx, c.ID, `UPDATE participants SET interests = ? WHERE id = ?`, string(encoded), c.ID); err != nil {
					return err
				}
			}
			if c.Match != nil {
				red, green, ice := encodeMatchResult(c.Match)
				if err := execFound(s.db.execCtx(), tx, c.ID, `
					UPDATE participants SET compat_score = ?, compat_reason = ?,
					    red_flags = ?, green_flags = ?, icebreakers = ?
					WHERE id = ?`, c.Match.Score, c.Match.Reason, red, green, ice, c.ID); err != nil {
					return err
				}
			}
			if c.MatchedWith != nil {
				if err := execFound(s.db.execCtx(), tx, c.ID, `UPDATE participants SET matched_with = ? WHERE id = ?`, *c.MatchedWith, c.ID); err != nil {
					return err
				}
			}
			if c.Persona != nil {
				if err := execFound(s.db.execCtx(), tx, c.ID, `UPDATE participants SET persona_name = ?, persona_tagline = ? WHERE id = ?`,
					c.Persona.Name, c.Persona.Tagline, c.ID); err != nil {
					return err
				}
			}
			if c.Profile != nil {
				encoded, _ := json.Marshal(c.Profile) // plain data: cannot fail
				if err := execFound(s.db.execCtx(), tx, c.ID, `UPDATE participants SET profile_json = ? WHERE id = ?`, string(encoded), c.ID); err != nil {
					return err
				}
			}
			if c.Answers != nil {
				encoded, _ := json.Marshal(c.Answers) // plain data: cannot fail
				if err := execFound(s.db.execCtx(), tx, c.ID, `UPDATE participants SET answers_json = ? WHERE id = ?`, string(encoded), c.ID); err != nil {
					return err
				}
			}
			if c.PipelineStep != nil {
				if err := advanceStep(s.db.execCtx(), tx, c.ID, *c.PipelineStep); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// advanceStep moves a Participant to the next Pipeline Step, guarded like
// DB.AdvanceStep: ErrIllegalTransition unless they are at the step directly
// before it, so a step is entered at most once.
func advanceStep(ctx context.Context, tx *sql.Tx, id string, to Step) error {
	from, ok := previousStep[to]
	if !ok {
		return fmt.Errorf("%w: nothing leads to %s", ErrIllegalTransition, to)
	}
	res, err := tx.ExecContext(ctx, `UPDATE participants SET pipeline_step = ? WHERE id = ? AND pipeline_step = ?`, to, id, from)
	if err != nil {
		return err
	}
	if rowsAffected(res) != 1 {
		return fmt.Errorf("%w: %s is not at %s, cannot enter %s", ErrIllegalTransition, id, from, to)
	}
	return nil
}

// StartInterview stores the profile and question set and opens the
// Interview, all in one transaction, guarded like DB.StartInterview: it
// fails with ErrIllegalTransition, writing nothing, unless the Participant
// is still being prepared (so a second, concurrent preparation cannot swap
// the questions of a running Interview).
func (s *ParticipantStore) StartInterview(id string, profile *GitHubProfile, questions []Question) error {
	if err := s.db.checkFault("start_interview"); err != nil {
		return err
	}
	profileJSON, _ := json.Marshal(profile)     // plain data: cannot fail
	questionsJSON, _ := json.Marshal(questions) // plain data: cannot fail
	return s.db.inTx(func(tx *sql.Tx) error {
		res, err := tx.ExecContext(s.db.execCtx(), `UPDATE participants SET pipeline_step = ?, profile_json = ?, questions = ?
			WHERE id = ? AND pipeline_step = ?`, StepInterviewing, string(profileJSON), string(questionsJSON), id, StepFetchingGitHub)
		if err != nil {
			return err
		}
		if rowsAffected(res) != 1 {
			return fmt.Errorf("%w: %s is no longer being prepared", ErrIllegalTransition, id)
		}
		return nil
	})
}

// execFound runs a statement expected to touch the Participant id and
// reports ErrParticipantNotFound when it touches no row instead,
// distinguishing an unknown Participant from a failure.
func execFound(ctx context.Context, tx *sql.Tx, id, query string, args ...any) error {
	res, err := tx.ExecContext(ctx, query, args...)
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
