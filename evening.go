package main

import "fmt"

// Evening owns the host's two semantic operations and every consequence
// they fire: advancing the Phase — the ticker line, the mission cards at
// the social, the champion at the close — and resetting the event — the
// slide-deck file sweep and the LLM matcher cache clear. The EventStore
// keeps the Phase fact and the forward-only rules (ADR-0010); the Evening
// module sequences what a transition sets off, so the host's routes stay
// thin HTTP mapping and the sequencing is unit-testable without HTTP or
// templates.
type Evening struct {
	db       *DB
	events   *EventStore
	missions *Missions
	meetings *Meetings
	slides   *SlideDecks
	matcher  *Matcher
}

// NewEvening creates the Evening module. Like the other domain modules it
// builds its own Event-state store from the database it receives
// (ADR-0005).
func NewEvening(db *DB, missions *Missions, meetings *Meetings, slides *SlideDecks, matcher *Matcher) *Evening {
	return &Evening{
		db: db, events: NewEventStore(db), missions: missions, meetings: meetings,
		slides: slides, matcher: matcher,
	}
}

// Advance moves the event to the given phase and fires the transition's
// consequences: the ticker line is logged before anything else, mission
// cards are generated at the social (in a background worker pool, so the
// transition returns immediately), and the champion is named at the close.
// A rejected transition — an unknown phase or one before the current one —
// reports ErrIllegalPhaseTransition; advancing to the current phase is a
// no-op that logs and fires nothing.
func (e *Evening) Advance(to Phase) error {
	before := e.events.State()
	if err := e.events.Advance(to); err != nil {
		return err
	}
	if after := e.events.State(); after != before {
		logger("evening").Info("phase advanced", "from", before, "to", after)
		e.db.LogActivity(after.Activity())
		// Mission cards go live at the social: generated in a background
		// worker pool, so the transition returns immediately.
		if after.MissionCardsLive() {
			e.missions.GenerateAll()
		}
		// The champion is named at the close.
		if after == PhaseClosed {
			e.announceChampion()
		}
	}
	return nil
}

// Reset starts the event over and sweeps what the store's transaction cannot
// reach: the Slide Deck rows went with the reset, their files go now, and
// the LLM matcher cache is cleared so the next event asks fresh. A sweep
// that fails is warned and carried on — the reset stands.
func (e *Evening) Reset() error {
	if err := e.events.Reset(); err != nil {
		return err
	}
	if err := e.slides.Sweep(); err != nil {
		logger("evening").Warn("removing the slide files after the reset", "err", err)
	}
	e.matcher.ClearCache()
	logger("evening").Info("event reset")
	return nil
}

// announceChampion names the Merge Trophy winner on the ticker when the event
// closes: the Participant with the most completed meetings, ties broken by
// persona name. With no Participants there is no champion to name.
func (e *Evening) announceChampion() {
	champion, err := e.meetings.Champion()
	if err != nil {
		logger("evening").Warn("champion at the close", "err", err)
		return
	}
	if champion == nil {
		return
	}
	noun := "connections"
	if champion.Connections == 1 {
		noun = "connection"
	}
	e.db.LogActivity(fmt.Sprintf("👑 %s %s takes the Merge Trophy with %d %s!", champion.Symbol, champion.Name, champion.Connections, noun))
}
