package main

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// PostStatus is where a Post stands: pending the Host's decision, approved
// (visible to everyone) or rejected (final; visible to its poster only).
type PostStatus string

// The post statuses.
const (
	// PostPending waits for the Host; only its poster sees it.
	PostPending PostStatus = "pending"
	// PostApproved is on the Board for everyone.
	PostApproved PostStatus = "approved"
	// PostRejected was refused, or taken down, by the Host. It is final.
	PostRejected PostStatus = "rejected"
)

// Post is one short message on the Board, with the poster's Persona joined
// live — Posts render through the poster's persona only.
type Post struct {
	ID            int64
	ParticipantID string
	Text          string
	Status        PostStatus
	CreatedAt     time.Time
	PersonaName   string
	PersonaSymbol string
	PersonaColor  string
}

// The Board's limits.
const (
	// maxPostLength caps a Post's text, in characters.
	maxPostLength = 280
	// maxPostsPerParticipant caps the Posts one Participant may hold, counting
	// the pending and approved ones: a rejected or deleted Post frees its place.
	maxPostsPerParticipant = 10
)

// The Board's guards.
var (
	// ErrInvalidPost is returned for a Post with no text or too much of it.
	ErrInvalidPost = errors.New("invalid post")
	// ErrBoardClosed is returned when posting after the event closed.
	ErrBoardClosed = errors.New("the board is closed")
	// ErrNotReady is returned when a Participant who is not ready posts: a
	// Post needs a Persona.
	ErrNotReady = errors.New("the participant is not ready yet")
	// ErrPostCap is returned when a Participant already holds the most Posts.
	ErrPostCap = errors.New("too many posts")
	// ErrPostNotFound is returned for an unknown Post id, or a Post that is not
	// the caller's to delete.
	ErrPostNotFound = errors.New("post not found")
	// ErrInvalidPostTransition is returned when the Host moves a Post to a
	// status it cannot reach: pending → approved or rejected, approved →
	// rejected; rejected is final.
	ErrInvalidPostTransition = errors.New("the post cannot move to that status")
)

// postTransitions lists the statuses a Post may move to from each status.
var postTransitions = map[PostStatus][]PostStatus{
	PostPending:  {PostApproved, PostRejected},
	PostApproved: {PostRejected},
	PostRejected: nil,
}

func canMovePost(from, to PostStatus) bool {
	for _, allowed := range postTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// Board owns the board_posts table: the Posts Participants share, the Host's
// approval, and what each surface may show. A Post's text reaches anybody but
// its poster only once the Host approved it.
type Board struct {
	db     *DB
	events *EventStore
	mu     sync.Mutex // serializes the Host's decisions: one read-then-write on a Post
}

// NewBoard creates the Board module.
func NewBoard(db *DB, events *EventStore) *Board {
	return &Board{db: db, events: events}
}

// selectPost reads Posts joined with their poster's Persona. The join is
// inner: the Posts of a Participant who left the event are not shown.
const selectPost = `
	SELECT b.id, b.participant_id, b.text, b.status, b.created_at,
	       p.persona_name, p.persona_symbol, p.persona_color
	FROM board_posts b
	JOIN participants p ON p.id = b.participant_id`

func scanPost(row interface{ Scan(...any) error }) (*Post, error) {
	b := &Post{}
	err := row.Scan(&b.ID, &b.ParticipantID, &b.Text, &b.Status, &b.CreatedAt,
		&b.PersonaName, &b.PersonaSymbol, &b.PersonaColor)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (b *Board) queryPosts(clause string, args ...any) ([]Post, error) {
	rows, err := b.db.db.Query(selectPost+" "+clause, args...)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)
	var out []Post
	for rows.Next() {
		post, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *post)
	}
	return out, rows.Err()
}

// Post records the Participant's Post, pending the Host's approval. The text
// needs 1–280 characters, the Participant must be ready, the event not closed,
// and the Participant may hold at most 10 pending or approved Posts.
func (b *Board) Post(p *Participant, text string) error {
	text, err := trimPostText(text)
	if err != nil {
		return err
	}
	if err := b.postAllowed(p); err != nil {
		return err
	}
	return b.insertPost(p, text)
}

// trimPostText normalizes a Post's text and enforces its 1–280 characters.
func trimPostText(text string) (string, error) {
	text = strings.TrimSpace(text)
	if n := utf8.RuneCountInString(text); n == 0 || n > maxPostLength {
		return "", fmt.Errorf("%w: a post needs 1–%d characters", ErrInvalidPost, maxPostLength)
	}
	return text, nil
}

// postAllowed checks the posting guards: the event not closed, the
// Participant ready — a Post needs a Persona.
func (b *Board) postAllowed(p *Participant) error {
	if b.events.State().IsClosed() {
		return ErrBoardClosed
	}
	if !p.PipelineStep.IsReady() {
		return ErrNotReady
	}
	return nil
}

// insertPost records the Post, pending the Host's approval, under the
// Participant's cap on the Posts they hold.
func (b *Board) insertPost(p *Participant, text string) error {
	return b.db.inTx(func(tx *sql.Tx) error {
		var held int
		err := tx.QueryRow(`SELECT COUNT(*) FROM board_posts WHERE participant_id = ? AND status IN (?, ?)`,
			p.ID, PostPending, PostApproved).Scan(&held)
		if err != nil {
			return err
		}
		if held >= maxPostsPerParticipant {
			return fmt.Errorf("%w: at most %d posts each", ErrPostCap, maxPostsPerParticipant)
		}
		_, err = tx.Exec(`INSERT INTO board_posts (participant_id, text) VALUES (?, ?)`, p.ID, text)
		return err
	})
}

// Visible returns what the Participant sees on their page, newest first: every
// approved Post, and their own Posts in any status.
func (b *Board) Visible(participantID string) ([]Post, error) {
	return b.queryPosts(`WHERE b.status = ? OR b.participant_id = ? ORDER BY b.id DESC`, PostApproved, participantID)
}

// Approved returns the approved Posts, newest first — the Big Screen, the
// recap and the export. A negative limit returns them all.
func (b *Board) Approved(limit int) ([]Post, error) {
	return b.queryPosts(`WHERE b.status = ? ORDER BY b.id DESC LIMIT ?`, PostApproved, limit)
}

// Moderation returns what the Host console lists: the pending Posts, oldest
// first, then the approved ones, newest first.
func (b *Board) Moderation() ([]Post, error) {
	return b.queryPosts(`WHERE b.status IN (?, ?)
		ORDER BY b.status = ?, CASE b.status WHEN ? THEN b.id ELSE -b.id END`,
		PostPending, PostApproved, PostApproved, PostPending)
}

// Moderate is the Host's decision on a Post: approve it, reject it, or take an
// approved one down (which rejects it). It works in every phase.
func (b *Board) Moderate(id int64, to PostStatus) (*Post, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	post, err := b.postByID(id)
	if err != nil {
		return nil, err
	}
	if !canMovePost(post.Status, to) {
		return nil, fmt.Errorf("%w: %s → %s", ErrInvalidPostTransition, post.Status, to)
	}
	if err := b.setPostStatus(id, to); err != nil {
		return nil, err
	}
	post.Status = to
	b.announcePost(post, to)
	return post, nil
}

// postByID fetches one Post by id, or ErrPostNotFound.
func (b *Board) postByID(id int64) (*Post, error) {
	posts, err := b.queryPosts(`WHERE b.id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(posts) == 0 {
		return nil, ErrPostNotFound
	}
	return &posts[0], nil
}

// setPostStatus moves a Post to its new status.
func (b *Board) setPostStatus(id int64, to PostStatus) error {
	return b.db.inTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE board_posts SET status = ? WHERE id = ?`, to, id)
		return err
	})
}

// announcePost logs the Post reaching the Board.
func (b *Board) announcePost(post *Post, to PostStatus) {
	if to == PostApproved {
		b.db.LogActivity(fmt.Sprintf("📣 %s %s posted on the board", post.PersonaSymbol, post.PersonaName))
	}
}

// Delete removes the Participant's own Post, in any status. A Post that is not
// theirs is not found.
func (b *Board) Delete(participantID string, id int64) error {
	return b.db.inTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`DELETE FROM board_posts WHERE id = ? AND participant_id = ?`, id, participantID)
		if err != nil {
			return err
		}
		if rowsAffected(res) == 0 {
			return ErrPostNotFound
		}
		return nil
	})
}

// RemoveParticipant deletes a Participant's Posts when they leave the event.
func (b *Board) RemoveParticipant(id string) error {
	return b.db.inTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`DELETE FROM board_posts WHERE participant_id = ?`, id)
		return err
	})
}

// Signature is what the personal pages' streams watch: the Posts the
// Participant sees, with their status. The section refreshes when a Post is
// approved, taken down or deleted, or changes hands.
func (b *Board) Signature(participantID string) string {
	posts, err := b.Visible(participantID)
	if err != nil {
		return ""
	}
	var s strings.Builder
	for _, post := range posts {
		fmt.Fprintf(&s, "%d:%s;", post.ID, post.Status)
	}
	return s.String()
}
