package main

import (
	"database/sql"
	"errors"
	"sync"

	"github.com/google/uuid"
)

// Recaps owns the recaps table: each Participant's recap token — the
// unguessable UUID that gates their personal recap page. The token is created
// on first use and persists across server restarts (SQLite), so a recap link
// keeps working after a restart.
type Recaps struct {
	db    *DB
	store *ParticipantStore
	mu    sync.Mutex
}

// NewRecaps creates the Recaps module.
func NewRecaps(db *DB) *Recaps {
	return &Recaps{db: db, store: NewParticipantStore(db)}
}

// TokenFor returns the Participant's recap token, creating one on first use.
// mu serializes the create-on-first-use, so two concurrent renders of the
// recap link cannot mint two tokens for one Participant.
func (r *Recaps) TokenFor(id string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var token string
	err := r.db.db.QueryRow(`SELECT token FROM recaps WHERE participant_id = ?`, id).Scan(&token)
	if err == nil {
		return token, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	token = uuid.New().String()
	if _, err := r.db.db.Exec(`INSERT INTO recaps (participant_id, token) VALUES (?, ?)`, id, token); err != nil {
		return "", err
	}
	return token, nil
}

// ByToken resolves a recap token to its Participant, or nil when the token is
// unknown — or its Participant has left, which kills the token. The token is
// the recap page's only gate; it is never a value in any debug endpoint.
func (r *Recaps) ByToken(token string) (*Participant, error) {
	var id string
	err := r.db.db.QueryRow(`SELECT participant_id FROM recaps WHERE token = ?`, token).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p, err := r.store.Get(id)
	if errors.Is(err, ErrParticipantNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

// RemoveParticipant deletes a Participant's recap token when they leave the
// event.
func (r *Recaps) RemoveParticipant(id string) error {
	_, err := r.db.db.Exec(`DELETE FROM recaps WHERE participant_id = ?`, id)
	if err == nil {
		r.db.changed()
	}
	return err
}
