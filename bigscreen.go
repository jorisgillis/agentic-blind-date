package main

import "sync"

// ScreenNode is one Participant as shown on the Big Screen's graph: their
// Persona always, their identity only once revealed (#45).
type ScreenNode struct {
	ID          string `json:"id"`
	PersonaName string `json:"persona_name"`
	Color       string `json:"color"`
	Symbol      string `json:"symbol"`
	Step        string `json:"step"`
	Ready       bool   `json:"ready"`
	Matched     bool   `json:"matched"`
	Handle      string `json:"handle,omitempty"`
}

// ScreenEdge connects two nodes: either a stored Match, or a potential
// connection from the Matcher's ranking.
type ScreenEdge struct {
	Source  string `json:"source"`
	Target  string `json:"target"`
	Score   int    `json:"score"`
	Matched bool   `json:"matched"`
}

// ScreenPersona is how one side of a Match identifies itself on the Big
// Screen: its Persona always, its identity only once revealed (#45).
type ScreenPersona struct {
	Symbol string `json:"symbol"`
	Color  string `json:"color"`
	Name   string `json:"name"`
	Handle string `json:"handle,omitempty"`
}

// ScreenMatch is one Match, listed once, with both Participants' own Persona (#55).
type ScreenMatch struct {
	A      ScreenPersona `json:"a"`
	B      ScreenPersona `json:"b"`
	Score  int           `json:"score"`
	Reason string        `json:"reason"`
}

// Snapshot is the room's public view of the event, computed once per change
// and shared by every open Big Screen (#55). Keys use Event State vocabulary
// rather than "phase".
type Snapshot struct {
	EventState    EventState    `json:"event_state"`
	Label         string        `json:"label"`
	Revealed      bool          `json:"revealed"`
	ThemeKey      string        `json:"theme_key"` // the admin's Big Screen Theme choice (#42)
	Count         int           `json:"count"`
	ReadyCount    int           `json:"ready_count"`
	Nodes         []ScreenNode  `json:"nodes"`
	Edges         []ScreenEdge  `json:"edges"`
	Matches       []ScreenMatch `json:"matches"`
	Activity      []string      `json:"activity"`
	Format        EventFormat   `json:"format"`               // #78
	Segment       ShowSegment   `json:"segment,omitempty"`    // only meaningful when Format is GameShow
	Round         int           `json:"round,omitempty"`      // 1-based; only meaningful when Segment is "round"
	RoundOpen     bool          `json:"round_open,omitempty"` // #80
	RoundCallout  *RoundCallout `json:"round_callout,omitempty"`
	RevealPairIDs []string      `json:"reveal_pair_ids,omitempty"` // 1 (the wildcard) or 2 node IDs currently lit on stage (#83)
}

// BigScreen builds the Snapshot the Big Screen shows: Participants, Match
// edges, potential connections from the Matcher's ranking, stats and the
// latest ticker lines. Identity hiding is applied once, here, for every view
// (graph, state view, stream) instead of separately in each.
type BigScreen struct {
	store   *ParticipantStore
	db      *DB
	matcher *Matcher
	show    *Show

	mu      sync.Mutex
	changes <-chan struct{}
	cached  *Snapshot
}

// NewBigScreen creates the Big Screen module.
func NewBigScreen(db *DB, matcher *Matcher, show *Show) *BigScreen {
	changes, _ := db.Subscribe() // kept for the process lifetime, like the module itself
	return &BigScreen{store: NewParticipantStore(db), db: db, matcher: matcher, show: show, changes: changes}
}

// Snapshot returns the current Snapshot, building it only when something
// changed since the last call: however many screens are open, the
// Participant graph and potential-connection ranking are computed once (#55).
func (b *BigScreen) Snapshot() *Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	select {
	case <-b.changes:
		b.cached = nil
	default:
	}
	if b.cached == nil {
		b.cached = b.build()
	}
	return b.cached
}

func (b *BigScreen) build() *Snapshot {
	event := b.db.EventState()
	participants, _ := b.store.All()
	activity, _ := b.db.GetRecentActivity(8)
	format := b.db.EventFormat()

	snap := &Snapshot{
		EventState: event,
		Label:      event.Label(),
		ThemeKey:   b.db.BigScreenTheme().Key,
		Revealed:   event.IsRevealed(),
		Count:      len(participants),
		Activity:   activity,
		Format:     format,
	}

	seen := map[string]bool{}
	pairKey := func(a, b string) (string, string) { return a + ":" + b, b + ":" + a }
	byID := make(map[string]*Participant, len(participants))
	for _, p := range participants {
		byID[p.ID] = p
	}
	// revealedFor narrows the global Revealed flag to a Game Show's
	// pair-by-pair Reveal: a Participant's own identity, and their Match's,
	// only appear once their pair has had its turn on stage (#83).
	revealedFor := func(id string) bool {
		return snap.Revealed && (format != GameShow || b.show.PairRevealed(id))
	}

	for _, p := range participants {
		if p.PipelineStep.IsReady() {
			snap.ReadyCount++
		}
		handle := ""
		if revealedFor(p.ID) {
			handle = p.DisplayHandle()
		}
		snap.Nodes = append(snap.Nodes, ScreenNode{
			ID: p.ID, PersonaName: p.PersonaName, Color: p.PersonaColor, Symbol: p.PersonaSymbol,
			Step: string(p.PipelineStep), Ready: p.PipelineStep.IsReady(), Matched: p.IsMatched(), Handle: handle,
		})
	}

	for _, p := range participants {
		if p.MatchedWith == "" {
			continue
		}
		key, rev := pairKey(p.ID, p.MatchedWith)
		if seen[key] || seen[rev] {
			continue
		}
		seen[key] = true
		snap.Edges = append(snap.Edges, ScreenEdge{Source: p.ID, Target: p.MatchedWith, Score: p.CompatScore, Matched: true})
		// byID[p.MatchedWith] always exists: the Relationship module keeps both
		// sides of a Match consistent in one transaction (#50).
		if partner := byID[p.MatchedWith]; p.CompatScore > 0 {
			pairRevealed := revealedFor(p.ID)
			snap.Matches = append(snap.Matches, ScreenMatch{
				A: screenPersona(p, pairRevealed), B: screenPersona(partner, pairRevealed),
				Score: p.CompatScore, Reason: p.CompatReason,
			})
		}
	}

	const potentialPerNode = 3
	for _, p := range participants {
		added := 0
		for _, c := range b.matcher.RankCandidates(p, participants) {
			if added == potentialPerNode {
				break
			}
			key, rev := pairKey(p.ID, c.Participant.ID)
			if seen[key] || seen[rev] {
				continue
			}
			seen[key] = true
			snap.Edges = append(snap.Edges, ScreenEdge{Source: p.ID, Target: c.Participant.ID, Score: c.Score, Matched: false})
			added++
		}
	}

	if format == GameShow {
		state := b.show.State()
		snap.Segment = state.Segment
		snap.Round = state.Round + 1
		snap.RoundOpen = state.RoundOpen
		snap.RoundCallout = b.show.RoundCallout()
		if pair, ok := b.show.CurrentRevealPair(); ok {
			snap.RevealPairIDs = []string{pair.A.ID}
			if pair.B != nil {
				snap.RevealPairIDs = append(snap.RevealPairIDs, pair.B.ID)
			}
		}
	}

	return snap
}

func screenPersona(p *Participant, revealed bool) ScreenPersona {
	sp := ScreenPersona{Symbol: p.PersonaSymbol, Color: p.PersonaColor, Name: p.PersonaName}
	if revealed {
		sp.Handle = p.DisplayHandle()
	}
	return sp
}
