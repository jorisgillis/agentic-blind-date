package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// Relationships owns Relationship State: who is matched with whom, and the
// assessment of each Match. It enforces the Key Invariant: a Participant has
// at most one partner. Every change is a single transaction.
type Relationships struct {
	db *DB
}

// NewRelationships creates the Relationship module.
func NewRelationships(db *DB) *Relationships {
	return &Relationships{db: db}
}

// Pair records a Match between m.A and m.B, with its assessment on both sides.
// Any existing Match of either Participant is broken first; the partners left
// behind are returned to the Pool and reported as displaced.
func (r *Relationships) Pair(m Match) (displaced []string, err error) {
	tx, err := r.db.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	for _, side := range [][2]string{{m.A.ID, m.B.ID}, {m.B.ID, m.A.ID}} {
		former, err := partnerOf(tx, side[0])
		if err != nil {
			return nil, err
		}
		if former != "" && former != side[1] {
			if err := unpair(tx, former); err != nil {
				return nil, err
			}
			displaced = append(displaced, former)
		}
	}

	red, green, ice := encodeAssessment(m.Result)
	for _, side := range [][2]string{{m.A.ID, m.B.ID}, {m.B.ID, m.A.ID}} {
		res, err := tx.Exec(`
			UPDATE participants SET matched_with = ?, compat_score = ?, compat_reason = ?,
			    red_flags = ?, green_flags = ?, icebreakers = ?, pipeline_step = 'matched'
			WHERE id = ?`, side[1], m.Result.Score, m.Result.Reason, red, green, ice, side[0])
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return nil, fmt.Errorf("pairing %s ↔ %s: participant %s not found", m.A.ID, m.B.ID, side[0])
		}
	}
	return displaced, tx.Commit()
}

// UnpairAll breaks every Match at once, returning everyone to the Pool.
func (r *Relationships) UnpairAll() error {
	_, err := r.db.db.Exec(`
		UPDATE participants SET matched_with = '', compat_score = 0, compat_reason = '',
		    red_flags = '[]', green_flags = '[]', icebreakers = '[]', pipeline_step = 'ready'
		WHERE pipeline_step = 'matched' OR COALESCE(matched_with, '') != ''`)
	return err
}

// Remove deletes a Participant. Their partner, if any, is returned to the Pool.
func (r *Relationships) Remove(id string) error {
	tx, err := r.db.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	partner, err := partnerOf(tx, id)
	if err != nil {
		return err
	}
	if partner != "" {
		if err := unpair(tx, partner); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM participants WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func partnerOf(tx *sql.Tx, id string) (string, error) {
	var partner string
	err := tx.QueryRow(`SELECT COALESCE(matched_with, '') FROM participants WHERE id = ?`, id).Scan(&partner)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("participant %s not found", id)
	}
	return partner, err
}

// unpair returns a Participant to the Pool, clearing their Match and its assessment.
func unpair(tx *sql.Tx, id string) error {
	_, err := tx.Exec(`
		UPDATE participants SET matched_with = '', compat_score = 0, compat_reason = '',
		    red_flags = '[]', green_flags = '[]', icebreakers = '[]',
		    pipeline_step = CASE WHEN pipeline_step = 'matched' THEN 'ready' ELSE pipeline_step END
		WHERE id = ?`, id)
	return err
}

func encodeAssessment(r *matchResult) (red, green, ice string) {
	redJSON, _ := json.Marshal(nonNil(r.RedFlags))
	greenJSON, _ := json.Marshal(nonNil(r.GreenFlags))
	iceJSON, _ := json.Marshal(nonNil(r.Icebreakers))
	return string(redJSON), string(greenJSON), string(iceJSON)
}
