package main

// EventAdmin runs the admin's whole-event actions: Reveal, Reset and
// Rematch. Reset shares Matchmaking's lock, so it cannot interleave with
// Continuous Matching, and Rematch already has that guarantee from
// Matchmaking itself (#57).
type EventAdmin struct {
	db          *DB
	activity    *Activity
	matcher     *Matcher
	matchmaking *Matchmaking
}

// NewEventAdmin creates the Event administration module.
func NewEventAdmin(db *DB, matcher *Matcher, matchmaking *Matchmaking) *EventAdmin {
	return &EventAdmin{db: db, activity: NewActivity(db), matcher: matcher, matchmaking: matchmaking}
}

// Reveal switches the Event State to revealed.
func (a *EventAdmin) Reveal() error {
	if err := a.db.Reveal(); err != nil {
		return err
	}
	a.activity.Revealed()
	return nil
}

// Reset starts the event over: no Participants, no activity, no cached LLM
// assessments, and back to before the Reveal. It takes Matchmaking's lock,
// so it cannot interleave with Continuous Matching (#57).
func (a *EventAdmin) Reset() error {
	return a.matchmaking.WithLock(func() error {
		if err := a.db.Reset(); err != nil {
			return err
		}
		a.matcher.ClearCache()
		return nil
	})
}

// RematchHandle reports a Rematch's outcome once it finishes: the admin page
// can render right away, and tests can wait on it instead of polling (#57).
type RematchHandle struct {
	done chan struct{}
	err  error
}

// Wait blocks until the Rematch finishes and returns its outcome.
func (h *RematchHandle) Wait() error {
	<-h.done
	return h.err
}

// Rematch starts a full Rematch in the background and returns immediately.
func (a *EventAdmin) Rematch() *RematchHandle {
	h := &RematchHandle{done: make(chan struct{})}
	a.activity.RematchTriggered()
	go func() {
		defer close(h.done)
		if err := a.matchmaking.Rematch(); err != nil {
			h.err = err
			a.activity.RematchFailed(err)
		}
	}()
	return h
}
