package main

import (
	"sort"
)

// Screen owns the Big Screen's payload: the graph of persona nodes and
// match/meeting/potential edges, the top-3 heuristic behind the potential
// edges, the connection leaderboard, the champion and the closing stats, and
// presenter mode with the host's identity reveal. Everything it produces is
// personas only, in every phase — the single sanctioned exception is the
// reveal, and it happens here, in one auditable place. The SSE transport
// (the watch loop, the heartbeat) stays in the handler: it is transport, not
// projection.
type Screen struct {
	db       *DB
	store    *ParticipantStore
	events   *EventStore
	matcher  *Matcher
	meetings *Meetings
	showtell *ShowTell
	closing  *Closing
	board    *Board
}

// NewScreen creates the Screen module. Like the other domain modules it
// builds its own Participant store, Event-state store and Board from the
// database it receives (ADR-0005).
func NewScreen(db *DB, matcher *Matcher, meetings *Meetings, showtell *ShowTell, closing *Closing) *Screen {
	return &Screen{
		db: db, store: NewParticipantStore(db), events: NewEventStore(db),
		matcher: matcher, meetings: meetings, showtell: showtell, closing: closing,
		board: NewBoard(db, NewEventStore(db)),
	}
}

// graphNode is one Participant as the graph draws them: their persona and
// where they stand — never their identity.
type graphNode struct {
	ID          string `json:"id"`
	PersonaName string `json:"persona_name"`
	Color       string `json:"color"`
	Symbol      string `json:"symbol"`
	Step        string `json:"step"`
	Ready       bool   `json:"ready"`
	Matched     bool   `json:"matched"`
}

// graphEdge is one connection on the graph: a match (carrying the
// compatibility score), a completed meeting, or a potential heuristic
// connection.
type graphEdge struct {
	Source  string `json:"source"`
	Target  string `json:"target"`
	Score   int    `json:"score"`
	Matched bool   `json:"matched"`
	Meeting bool   `json:"meeting"`
}

// ScreenPayload is the Big Screen's wire payload. The fields are declared in
// the order the wire has always carried them (encoding/json sorts map keys,
// and the payload was a map): the JSON stays byte-identical. The keys that
// ride only in some phases are pointers — nil, not empty — so a key is
// present exactly when the screen has something for it, and the Leaderboard
// pointer keeps even an empty scoreboard present, as the map did.
type ScreenPayload struct {
	Activity    []string              `json:"activity"`
	Board       []BoardCard           `json:"board,omitempty"`
	Champion    *ConnectionStanding   `json:"champion,omitempty"`
	Edges       []graphEdge           `json:"edges"`
	Leaderboard *[]ConnectionStanding `json:"leaderboard,omitempty"`
	Nodes       []graphNode           `json:"nodes"`
	Phase       Phase                 `json:"phase"`
	PhaseLabel  string                `json:"phase_label"`
	Presenter   *PresenterCard        `json:"presenter,omitempty"`
	RecapURL    string                `json:"recap_url,omitempty"`
	Revealed    bool                  `json:"revealed"`
	Stats       *EventStats           `json:"stats,omitempty"`
}

// PresenterCard is presenter mode: the on-stage Sign-up's persona card with
// the Hype Agent's intro line. The handle and name ride only once the host's
// explicit identity reveal set IdentityRevealed, and the current audience
// question rides with the card.
type PresenterCard struct {
	Description      string             `json:"description"`
	Handle           *string            `json:"handle,omitempty"`
	Hype             string             `json:"hype"`
	IdentityRevealed bool               `json:"identity_revealed"`
	Name             *string            `json:"name,omitempty"`
	PersonaColor     string             `json:"persona_color"`
	PersonaName      string             `json:"persona_name"`
	PersonaSymbol    string             `json:"persona_symbol"`
	PersonaTagline   string             `json:"persona_tagline"`
	Question         *PresenterQuestion `json:"question,omitempty"`
	RepoURL          string             `json:"repo_url"`
	SignupID         int64              `json:"signup_id"`
	Title            string             `json:"title"`
}

// PresenterQuestion is the current audience question: the text and the
// asker's persona only — personas, never identities.
type PresenterQuestion struct {
	PersonaColor  string `json:"persona_color"`
	PersonaName   string `json:"persona_name"`
	PersonaSymbol string `json:"persona_symbol"`
	Text          string `json:"text"`
}

// BoardCard is one approved Post as the screen rotates it: the text, the
// first link (the QR's target) and the poster's persona.
type BoardCard struct {
	ID            int64  `json:"id"`
	Link          string `json:"link"`
	PersonaColor  string `json:"persona_color"`
	PersonaName   string `json:"persona_name"`
	PersonaSymbol string `json:"persona_symbol"`
	Text          string `json:"text"`
}

// Payload projects the Big Screen's entire payload for the current phase. A
// read that fails degrades, never breaks the screen: the graph still renders
// (its nodes present but empty), the scoreboard is simply absent, and a
// presenter whose identity cannot be read keeps the stage without the handle.
func (s *Screen) Payload() ScreenPayload {
	phase := s.events.State()
	participants, _ := s.store.All()
	activity, _ := s.db.GetRecentActivity(3)

	nodes := make([]graphNode, 0, len(participants))
	for _, p := range participants {
		// The Big Screen shows persona names only, in every phase — identities
		// never leave the server in the graph payload.
		nodes = append(nodes, graphNode{
			ID:          p.ID,
			PersonaName: p.PersonaName,
			Color:       p.PersonaColor,
			Symbol:      p.PersonaSymbol,
			Step:        string(p.PipelineStep),
			Ready:       p.PipelineStep.IsReady(),
			Matched:     p.IsMatched(),
		})
	}

	edges := make([]graphEdge, 0)
	seen := map[string]bool{}

	for _, p := range participants {
		if p.MatchedWith == "" {
			continue
		}
		key := p.ID + ":" + p.MatchedWith
		rev := p.MatchedWith + ":" + p.ID
		if seen[key] || seen[rev] {
			continue
		}
		seen[key] = true
		edges = append(edges, graphEdge{
			Source:  p.ID,
			Target:  p.MatchedWith,
			Score:   p.Assessment.Score,
			Matched: true,
		})
	}

	// Meeting edges: every completed meeting joins the graph alongside the
	// match edges. A pair that are matched and met keeps one edge, flagged as
	// both — the meeting is the stronger fact on the screen. The map holds
	// edge indexes, not pointers: appending below may reallocate the slice.
	byPair := map[string]int{}
	for i := range edges {
		byPair[edges[i].Source+":"+edges[i].Target] = i
		byPair[edges[i].Target+":"+edges[i].Source] = i
	}
	meetings, _ := s.meetings.All()
	for _, m := range meetings {
		if !m.Completed() {
			continue
		}
		key := m.A + ":" + m.B
		if i, ok := byPair[key]; ok {
			edges[i].Meeting = true
			continue
		}
		seen[key] = true
		edges = append(edges, graphEdge{Source: m.A, Target: m.B, Meeting: true})
	}

	type scored struct {
		id    string
		score int
	}
	topEdges := map[string][]scored{}
	for i, a := range participants {
		for j, b := range participants {
			if i >= j {
				continue
			}
			key := a.ID + ":" + b.ID
			rev := b.ID + ":" + a.ID
			if seen[key] || seen[rev] {
				continue
			}
			score := s.matcher.PairScore(a, b)
			topEdges[a.ID] = append(topEdges[a.ID], scored{b.ID, score})
			topEdges[b.ID] = append(topEdges[b.ID], scored{a.ID, score})
		}
	}
	for _, p := range participants {
		candidates := topEdges[p.ID]
		sort.Slice(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
		if len(candidates) > 3 {
			candidates = candidates[:3]
		}
		for _, c := range candidates {
			key := p.ID + ":" + c.id
			rev := c.id + ":" + p.ID
			if seen[key] || seen[rev] {
				continue
			}
			seen[key] = true
			edges = append(edges, graphEdge{
				Source:  p.ID,
				Target:  c.id,
				Score:   c.score,
				Matched: false,
			})
		}
	}

	payload := ScreenPayload{
		Phase:      phase,
		PhaseLabel: phase.Label(),
		Revealed:   phase.AtLeast(PhaseSocial),
		Nodes:      nodes,
		Edges:      edges,
		Activity:   activity,
	}

	// The social scoreboard: the top 5 by connections, from the social on.
	// During onboarding the Big Screen is the graph and the ticker only.
	if phase.AtLeast(PhaseSocial) {
		board, err := s.meetings.Leaderboard()
		if err != nil {
			logger("screen").Warn("connection leaderboard", "err", err)
		} else {
			top := board
			if len(top) > 5 {
				top = top[:5]
			}
			payload.Leaderboard = &top
			// The champion is named at the close: the Merge Trophy winner,
			// the top of the final leaderboard.
			if phase == PhaseClosed && len(top) > 0 {
				payload.Champion = &top[0]
			}
		}
	}

	// The closing view: from closed on, the Big Screen also carries the
	// evening's stats and the recap QR — alongside the final leaderboard,
	// the champion and the graph with all meeting edges.
	if phase == PhaseClosed {
		stats := s.closing.Stats()
		payload.Stats = &stats
		payload.RecapURL = "/recap"
	}

	// Presenter mode: from show & tell on, the Big Screen shows the on-stage
	// presenter's card with the hype intro — personas only, until the host's
	// explicit identity reveal flips the card to the GitHub handle/name.
	if phase == PhaseShowTell {
		if presenter := s.presenter(); presenter != nil {
			payload.Presenter = presenter
		}
	}

	// The Board: the newest approved Posts rotate on the screen in every phase,
	// except while a presenter is on stage. Personas only.
	if payload.Presenter == nil {
		if cards := s.boardCards(); len(cards) > 0 {
			payload.Board = cards
		}
	}
	return payload
}

// boardCardsLimit is how many of the newest approved Posts the Big Screen
// rotates through.
const boardCardsLimit = 10

// boardCards is the Big Screen's Board: the newest approved Posts with their
// poster's Persona and the first link of each (the QR's target, "" when the
// Post has none). Only approved Posts ever reach the screen.
func (s *Screen) boardCards() []BoardCard {
	posts, err := s.board.Approved(boardCardsLimit)
	if err != nil {
		logger("screen").Warn("big screen board", "err", err)
		return nil
	}
	out := make([]BoardCard, 0, len(posts))
	for _, post := range posts {
		out = append(out, BoardCard{
			ID:            post.ID,
			Text:          post.Text,
			Link:          firstLink(post.Text),
			PersonaName:   post.PersonaName,
			PersonaSymbol: post.PersonaSymbol,
			PersonaColor:  post.PersonaColor,
		})
	}
	return out
}

// presenter is the Big Screen's presenter mode: the on-stage sign-up with its
// presenter's Persona, the talk and the hype intro. Personas only — the
// identity (the GitHub handle/name) is carried only once the host's explicit
// reveal action set IdentityRevealed; it is the only place a handle reaches
// the screen besides the owner's own recap.
func (s *Screen) presenter() *PresenterCard {
	signup, err := s.showtell.OnStage()
	if err != nil {
		logger("screen").Warn("on-stage presenter", "err", err)
		return nil
	}
	if signup == nil {
		return nil
	}
	card := &PresenterCard{
		SignupID:         signup.ID,
		Title:            signup.Title,
		Description:      signup.Description,
		RepoURL:          signup.RepoURL,
		PersonaName:      signup.PersonaName,
		PersonaSymbol:    signup.PersonaSymbol,
		PersonaColor:     signup.PersonaColor,
		PersonaTagline:   signup.PersonaTagline,
		Hype:             signup.Hype,
		IdentityRevealed: signup.IdentityRevealed,
	}
	if signup.IdentityRevealed {
		p, err := s.store.Get(signup.ParticipantID)
		if err != nil {
			logger("screen").Warn("revealed presenter", "participant", signup.ParticipantID, "err", err)
			// The reveal is the screen's one sanctioned identity leak, and it
			// failed: the presenter keeps the stage without the identity —
			// and without the question, exactly as before.
			return card
		}
		handle, name := p.DisplayHandle(), p.Name
		card.Handle, card.Name = &handle, &name
	}
	// The current audience question rides with the presenter card — an
	// extension of presenter mode. The asker's persona and the text only:
	// personas, never identities.
	if question, err := s.showtell.CurrentQuestion(); err != nil {
		logger("screen").Warn("current show & tell question", "err", err)
	} else if question != nil {
		card.Question = &PresenterQuestion{
			Text:          question.Text,
			PersonaName:   question.PersonaName,
			PersonaSymbol: question.PersonaSymbol,
			PersonaColor:  question.PersonaColor,
		}
	}
	return card
}
