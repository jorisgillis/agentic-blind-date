package main

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// SignupStatus is where a show & tell sign-up stands: queued in the host's
// queue, on stage (the Big Screen's presenter mode), or presented.
type SignupStatus string

// The sign-up statuses.
const (
	// SignupQueued waits in the host's queue.
	SignupQueued SignupStatus = "queued"
	// SignupOnStage is the presenter the Big Screen shows.
	SignupOnStage SignupStatus = "on_stage"
	// SignupPresented is a talk that happened.
	SignupPresented SignupStatus = "presented"
)

// ShowTellSignup is one Participant's show & tell sign-up: their talk, its
// place in the host's queue, its status and the Hype Agent's intro line.
// IdentityRevealed is set only by the host's explicit reveal action; the
// identity itself stays on the Participant and leaves the server only in the
// Big Screen's presenter payload, once revealed.
type ShowTellSignup struct {
	ID               int64        `json:"id"`
	ParticipantID    string       `json:"participant_id"`
	Title            string       `json:"title"`
	Description      string       `json:"description"`
	RepoURL          string       `json:"repo_url"`
	Status           SignupStatus `json:"status"`
	IdentityRevealed bool         `json:"identity_revealed"`
	Hype             string       `json:"hype"`
	Position         int          `json:"position"`
	CreatedAt        time.Time    `json:"created_at"`
	// The presenter's Persona, joined live — personas are safe to show anywhere.
	PersonaName    string `json:"persona_name"`
	PersonaSymbol  string `json:"persona_symbol"`
	PersonaColor   string `json:"persona_color"`
	PersonaTagline string `json:"persona_tagline"`
}

// The show & tell guards.
var (
	// ErrShowTellClosed is returned when a show & tell action is attempted
	// outside the phases that allow it: sign-up opens with the social, and
	// presenter mode (staging, the identity reveal, done) with show & tell.
	ErrShowTellClosed = errors.New("show & tell is not open")
	// ErrInvalidSignup is returned for a sign-up that fails validation: no
	// title, or a repo link that is not an http(s) URL.
	ErrInvalidSignup = errors.New("invalid show & tell sign-up")
	// ErrSignupNotFound is returned for an unknown sign-up id.
	ErrSignupNotFound = errors.New("show & tell sign-up not found")
	// ErrSignupNotQueued is returned when staging a sign-up that is not
	// waiting in the queue (a talk that already happened).
	ErrSignupNotQueued = errors.New("the sign-up is not queued")
	// ErrAlreadyOnStage is returned when staging while another sign-up is on stage.
	ErrAlreadyOnStage = errors.New("someone is already on stage")
	// ErrNotOnStage is returned when revealing or finishing a sign-up that is
	// not on stage.
	ErrNotOnStage = errors.New("the sign-up is not on stage")
	// ErrInvalidDirection is returned when reordering the queue with a
	// direction other than up or down.
	ErrInvalidDirection = errors.New("the direction must be up or down")
)

// selectSignup reads one sign-up joined with its presenter's Persona. The
// persona columns are COALESCEd: a sign-up whose presenter left the event
// still lists (debug-only), with an empty persona.
const selectSignup = `
	SELECT s.id, s.participant_id, s.title, s.description, s.repo_url,
	       s.status, s.identity_revealed, s.hype, s.position, s.created_at,
	       COALESCE(p.persona_name, ''), COALESCE(p.persona_symbol, ''),
	       COALESCE(p.persona_color, ''), COALESCE(p.persona_tagline, '')
	FROM showtell_signups s
	LEFT JOIN participants p ON p.id = s.participant_id`

// scanSignup scans one sign-up row. identity_revealed is stored as 0/1.
func scanSignup(row interface{ Scan(...any) error }) (*ShowTellSignup, error) {
	s := &ShowTellSignup{}
	var revealed bool
	err := row.Scan(
		&s.ID, &s.ParticipantID, &s.Title, &s.Description, &s.RepoURL,
		&s.Status, &revealed, &s.Hype, &s.Position, &s.CreatedAt,
		&s.PersonaName, &s.PersonaSymbol, &s.PersonaColor, &s.PersonaTagline,
	)
	if err != nil {
		return nil, err
	}
	s.IdentityRevealed = revealed
	return s, nil
}

// signupRow reads one sign-up from row, mapping "no row" to nil.
func signupRow(row *sql.Row) (*ShowTellSignup, error) {
	signup, err := scanSignup(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return signup, nil
}

// ShowTell owns the showtell_signups and showtell_questions tables: the show
// & tell sign-ups, the host's queue, the Big Screen's presenter mode and the
// audience questions. Sign-up opens with the social; staging, the identity
// reveal, done and the question queue belong to show & tell.
type ShowTell struct {
	db     *DB
	events *EventStore
	agent  *HypeAgent
	mu     sync.Mutex // serializes staging and question transitions: one read-then-write on the queue
}

// NewShowTell creates the ShowTell module.
func NewShowTell(db *DB, llm LLM) *ShowTell {
	return &ShowTell{db: db, events: NewEventStore(db), agent: NewHypeAgent(llm)}
}

// Signup records the Participant's sign-up — a title, a one-line description
// and an optional repo link — or updates it when they already have one (the
// place in the queue and the status stay). Late sign-ups join the tail. The
// sign-up is open during the social and show & tell only.
func (s *ShowTell) Signup(p *Participant, title, description, repoURL string) error {
	title, description, repoURL, err := validateSignup(title, description, repoURL)
	if err != nil {
		return err
	}
	if phase := s.events.State(); !phase.AllowsShowTell() {
		return fmt.Errorf("%w: the event is in the %s phase", ErrShowTellClosed, phase)
	}
	err = s.db.inTx(func(tx *sql.Tx) error {
		// One sign-up per Participant; a new one takes the next free place,
		// so late sign-ups join the tail of the queue.
		_, err := tx.Exec(`
			INSERT INTO showtell_signups (participant_id, title, description, repo_url, position)
			VALUES (?, ?, ?, ?, (SELECT COALESCE(MAX(position), -1) + 1 FROM showtell_signups))
			ON CONFLICT(participant_id) DO UPDATE SET
				title = excluded.title,
				description = excluded.description,
				repo_url = excluded.repo_url`,
			p.ID, title, description, repoURL)
		return err
	})
	if err != nil {
		return err
	}
	s.db.LogActivity(fmt.Sprintf("🎤 %s signed up for show & tell: %q", p.PersonaName, title))
	return nil
}

// validateSignup trims the sign-up's fields and validates them: a title is
// required, and a repo link, when given, must be an http(s) URL.
func validateSignup(title, description, repoURL string) (string, string, string, error) {
	title = strings.TrimSpace(title)
	description = strings.TrimSpace(description)
	repoURL = strings.TrimSpace(repoURL)
	if title == "" {
		return "", "", "", fmt.Errorf("%w: a title is required", ErrInvalidSignup)
	}
	if !validRepoURL(repoURL) {
		return "", "", "", fmt.Errorf("%w: the repo link must start with http:// or https://", ErrInvalidSignup)
	}
	return title, description, repoURL, nil
}

// validRepoURL reports whether a repo link is acceptable: empty (no link) or
// an http(s) URL.
func validRepoURL(repoURL string) bool {
	if repoURL == "" {
		return true
	}
	return strings.HasPrefix(repoURL, "http://") || strings.HasPrefix(repoURL, "https://")
}

// Queue returns every sign-up, in queue order — the host console's view: the
// queued talks, the one on stage and the already-presented ones.
func (s *ShowTell) Queue() ([]ShowTellSignup, error) {
	return s.querySignups(`ORDER BY s.position`)
}

// querySignups selects the sign-ups matching the given clause.
func (s *ShowTell) querySignups(clause string, args ...any) ([]ShowTellSignup, error) {
	rows, err := s.db.db.Query(selectSignup+" "+clause, args...)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows)

	var out []ShowTellSignup
	for rows.Next() {
		signup, err := scanSignup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *signup)
	}
	return out, rows.Err()
}

// ByID returns the sign-up, or nil when the id is not in the queue.
func (s *ShowTell) ByID(id int64) (*ShowTellSignup, error) {
	return signupRow(s.db.db.QueryRow(selectSignup+` WHERE s.id = ?`, id))
}

// ForParticipant returns the Participant's sign-up, or nil when they have none.
func (s *ShowTell) ForParticipant(participantID string) (*ShowTellSignup, error) {
	return signupRow(s.db.db.QueryRow(selectSignup+` WHERE s.participant_id = ?`, participantID))
}

// OnStage returns the sign-up that is on stage — the Big Screen's presenter —
// or nil when the stage is empty.
func (s *ShowTell) OnStage() (*ShowTellSignup, error) {
	return signupRow(s.db.db.QueryRow(selectSignup+` WHERE s.status = ? LIMIT 1`, SignupOnStage))
}

// QueuePosition is the Participant's 1-based place in the queue — 0 when they
// are not queued. Derived from the queue, never stored.
func (s *ShowTell) QueuePosition(participantID string) int {
	var n int
	err := s.db.db.QueryRow(`
		SELECT (SELECT COUNT(*) FROM showtell_signups q
		        WHERE q.status = ? AND q.position < m.position) + 1
		FROM showtell_signups m
		WHERE m.participant_id = ? AND m.status = ?`,
		SignupQueued, participantID, SignupQueued).Scan(&n)
	if err != nil {
		return 0
	}
	return n
}

// Signature is what the personal page's stream watches: the Participant's
// sign-up state — queued (and where in the queue), on stage, presented,
// revealed — their questions' states, and who is on stage, because the
// section's question form depends on the presenter. The section refreshes
// when any of them changes.
func (s *ShowTell) Signature(participantID string) string {
	var b strings.Builder
	s.writeSignupSignature(&b, participantID)
	s.writeQuestionSignature(&b, participantID)
	s.writeStageSignature(&b)
	return b.String()
}

// writeSignupSignature appends the Participant's sign-up state — queued (and
// where in the queue), on stage, presented, revealed — when they have one.
func (s *ShowTell) writeSignupSignature(b *strings.Builder, participantID string) {
	if signup, err := s.ForParticipant(participantID); err == nil && signup != nil {
		fmt.Fprintf(b, "%d:%s:%d:%t;", signup.ID, signup.Status, signup.Position, signup.IdentityRevealed)
	}
}

// writeQuestionSignature appends the Participant's questions' states.
func (s *ShowTell) writeQuestionSignature(b *strings.Builder, participantID string) {
	if questions, err := s.QuestionsForParticipant(participantID); err == nil {
		for _, q := range questions {
			fmt.Fprintf(b, "%d:%s;", q.ID, q.Status)
		}
	}
}

// writeStageSignature appends who is on stage, because the section's
// question form depends on the presenter.
func (s *ShowTell) writeStageSignature(b *strings.Builder) {
	if onStage, err := s.OnStage(); err == nil && onStage != nil {
		fmt.Fprintf(b, "stage:%d;", onStage.ID)
	}
}

// Stage puts a queued sign-up on stage: the Big Screen's presenter mode shows
// the presenter's persona card with the Hype Agent's intro line (the generic
// line when the LLM fails). Only one sign-up is on stage at a time.
func (s *ShowTell) Stage(id int64) (*ShowTellSignup, error) {
	if phase := s.events.State(); !phase.IsShowTell() {
		return nil, fmt.Errorf("%w: the event is in the %s phase, presenter mode starts with show & tell", ErrShowTellClosed, phase)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	queue, err := s.Queue()
	if err != nil {
		return nil, err
	}
	signup, err := stageCandidate(queue, id)
	if err != nil {
		return nil, err
	}
	// The Hype Agent writes the intro line before the write, so staging never
	// holds a transaction across an LLM call.
	signup.Hype = s.agent.Hype(signup)
	err = s.db.inTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE showtell_signups SET status = ?, hype = ? WHERE id = ?`, SignupOnStage, signup.Hype, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	signup.Status = SignupOnStage
	s.db.LogActivity(fmt.Sprintf("🎤 %s %s is on stage: %q", signup.PersonaSymbol, signup.PersonaName, signup.Title))
	return signup, nil
}

// stageCandidate finds the sign-up to stage in the queue, rejecting staging
// while another sign-up is on stage, an unknown id, and a sign-up that is
// not waiting in the queue (a talk that already happened).
func stageCandidate(queue []ShowTellSignup, id int64) (*ShowTellSignup, error) {
	if onStage := onStageSignup(queue); onStage != nil {
		return nil, fmt.Errorf("%w: %s %s is on stage", ErrAlreadyOnStage, onStage.PersonaSymbol, onStage.PersonaName)
	}
	signup := queueByID(queue, id)
	if signup == nil {
		return nil, ErrSignupNotFound
	}
	if signup.Status != SignupQueued {
		return nil, fmt.Errorf("%w: already %s", ErrSignupNotQueued, signup.Status)
	}
	return signup, nil
}

// onStageSignup returns the sign-up on stage in the queue, nil when the
// stage is empty.
func onStageSignup(queue []ShowTellSignup) *ShowTellSignup {
	for i := range queue {
		if queue[i].Status == SignupOnStage {
			return &queue[i]
		}
	}
	return nil
}

// queueByID returns the sign-up with the given id in the queue, nil when the
// id is not in it.
func queueByID(queue []ShowTellSignup, id int64) *ShowTellSignup {
	for i := range queue {
		if queue[i].ID == id {
			return &queue[i]
		}
	}
	return nil
}

// RevealIdentity flips the on-stage presenter's card to their GitHub
// handle/name — the only place an identity leaves the server besides the
// owner's own recap. Idempotent: a second reveal is a no-op.
func (s *ShowTell) RevealIdentity(id int64) (*ShowTellSignup, error) {
	if phase := s.events.State(); !phase.IsShowTell() {
		return nil, fmt.Errorf("%w: the event is in the %s phase", ErrShowTellClosed, phase)
	}
	signup, err := s.onStageByID(id)
	if err != nil {
		return nil, err
	}
	if signup.IdentityRevealed {
		return signup, nil
	}
	err = s.db.inTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE showtell_signups SET identity_revealed = 1 WHERE id = ?`, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	signup.IdentityRevealed = true
	s.db.LogActivity(fmt.Sprintf("🎭 The host revealed who %s %s is", signup.PersonaSymbol, signup.PersonaName))
	return signup, nil
}

// Done marks the on-stage talk presented, taking the stage for the next one.
func (s *ShowTell) Done(id int64) (*ShowTellSignup, error) {
	if phase := s.events.State(); !phase.IsShowTell() {
		return nil, fmt.Errorf("%w: the event is in the %s phase", ErrShowTellClosed, phase)
	}
	signup, err := s.onStageByID(id)
	if err != nil {
		return nil, err
	}
	err = s.db.inTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE showtell_signups SET status = ? WHERE id = ?`, SignupPresented, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	signup.Status = SignupPresented
	s.db.LogActivity(fmt.Sprintf("✅ %s %s presented %q", signup.PersonaSymbol, signup.PersonaName, signup.Title))
	return signup, nil
}

// onStageByID reads the sign-up and guards that it is on stage: the identity
// reveal and Done act on the presenter only.
func (s *ShowTell) onStageByID(id int64) (*ShowTellSignup, error) {
	signup, err := s.ByID(id)
	if err != nil {
		return nil, err
	}
	if signup == nil {
		return nil, ErrSignupNotFound
	}
	if signup.Status != SignupOnStage {
		return nil, fmt.Errorf("%w: it is %s", ErrNotOnStage, signup.Status)
	}
	return signup, nil
}

// Move moves a queued sign-up one place up or down the queue, swapping places
// with its neighbour. Moving past either end — or moving a sign-up that is
// not queued — is a no-op.
func (s *ShowTell) Move(id int64, dir string) error {
	if dir != "up" && dir != "down" {
		return fmt.Errorf("%w: %q", ErrInvalidDirection, dir)
	}
	queue, err := s.Queue()
	if err != nil {
		return err
	}
	a, b, ok := swapPair(queue, id, dir)
	if !ok {
		return nil
	}
	return s.db.inTx(func(tx *sql.Tx) error {
		// One statement swaps the two places, so the reorder is atomic.
		_, err := tx.Exec(
			`UPDATE showtell_signups SET position = CASE id WHEN ? THEN ? WHEN ? THEN ? END WHERE id IN (?, ?)`,
			a.ID, b.Position, b.ID, a.Position, a.ID, b.ID)
		return err
	})
}

// swapPair finds the sign-up and the neighbour it swaps places with, ok=false
// when there is no swap to make: the sign-up is not queued, or it already
// sits at the head or the tail of the queue.
func swapPair(queue []ShowTellSignup, id int64, dir string) (a, b ShowTellSignup, ok bool) {
	from, queued := queuedIndex(queue, id)
	if from < 0 {
		return a, b, false
	}
	to := from - 1
	if dir == "down" {
		to = from + 1
	}
	if to < 0 || to >= len(queued) {
		return a, b, false
	}
	return queued[from], queued[to], true
}

// queuedIndex returns the queued sign-ups in queue order and the sign-up's
// place among them, -1 when it is not queued (unknown, on stage or
// presented).
func queuedIndex(queue []ShowTellSignup, id int64) (int, []ShowTellSignup) {
	from := -1
	var queued []ShowTellSignup
	for _, signup := range queue {
		if signup.Status != SignupQueued {
			continue
		}
		if signup.ID == id {
			from = len(queued)
		}
		queued = append(queued, signup)
	}
	return from, queued
}

// Remove takes a sign-up out of the queue, whatever its status — and the
// questions asked about it, in the same transaction.
func (s *ShowTell) Remove(id int64) error {
	return s.db.inTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM showtell_questions WHERE signup_id = ?`, id); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM showtell_signups WHERE id = ?`, id)
		return err
	})
}

// RemoveParticipant deletes a Participant's sign-up when they leave the event,
// and with it their questions — both the ones they asked and the ones their
// talk drew, in the same transaction.
func (s *ShowTell) RemoveParticipant(id string) error {
	return s.db.inTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`
			DELETE FROM showtell_questions
			WHERE participant_id = ? OR signup_id IN (SELECT id FROM showtell_signups WHERE participant_id = ?)`, id, id); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM showtell_signups WHERE participant_id = ?`, id)
		return err
	})
}

// HypeAgent writes the one-line hype intro for a presenter going on stage. It
// never blocks staging: on failure the generic line stands.
type HypeAgent struct {
	llm LLM
}

// NewHypeAgent creates the Hype Agent.
func NewHypeAgent(llm LLM) *HypeAgent {
	return &HypeAgent{llm: llm}
}

// genericHype is the fallback when the Hype Agent cannot answer.
const genericHype = "Please welcome our next speaker to the stage!"

// hypeReply is the Hype Agent's raw reply shape.
type hypeReply struct {
	Hype string `json:"hype"`
}

// Hype writes the presenter's intro line — "Please welcome… The YAML Wrangler
// 🐙 — Friday deployer, 4 a.m. rebaser". On any failure the generic line
// stands, so staging never waits on the LLM.
func (a *HypeAgent) Hype(signup *ShowTellSignup) string {
	hype, err := a.ask(signup)
	if err != nil {
		logger("showtell").Warn("hype intro, falling back", "signup", signup.ID, "err", err)
		return genericHype
	}
	return hype
}

// ask makes the one call and validates the reply: a single non-empty line.
func (a *HypeAgent) ask(signup *ShowTellSignup) (string, error) {
	system := `You are the host of the show & tell at a tech meetup blind date event.
A presenter is coming on stage. Write their hype intro: one line, max 120 chars, in the shape
"Please welcome… {persona name} {persona symbol} — {one punchy line from their persona tagline}".
Be funny and tongue-in-cheek. Respond with ONLY valid JSON — no markdown:
{"hype": "<the one line>"}`
	reply, err := AskStructured(a.llm, system, hypePrompt(signup), func(r hypeReply) error {
		if strings.TrimSpace(r.Hype) == "" {
			return errors.New("hype reply is empty")
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(reply.Hype), nil
}

// hypePrompt describes the presenter and their talk for the Hype Agent: the
// Persona and the talk — never the handle, identities stay off the LLM.
func hypePrompt(s *ShowTellSignup) string {
	var b strings.Builder
	fmt.Fprintf(&b, "PRESENTER: %s %s\nPersona tagline: %s\n", s.PersonaSymbol, s.PersonaName, s.PersonaTagline)
	fmt.Fprintf(&b, "TALK: %s\n", s.Title)
	if s.Description != "" {
		fmt.Fprintf(&b, "Description: %s\n", s.Description)
	}
	if s.RepoURL != "" {
		fmt.Fprintf(&b, "Repo: %s\n", s.RepoURL)
	}
	return b.String()
}
