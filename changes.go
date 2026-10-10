package main

import "sync"

// changeFeed announces that some Participant or the Event State changed.
// Subscribers get a signal on a 1-buffered channel, so bursts of changes
// coalesce into one wake-up; they then re-read whatever they show.
type changeFeed struct {
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
}

func newChangeFeed() *changeFeed {
	return &changeFeed{subs: map[chan struct{}]struct{}{}}
}

func (f *changeFeed) subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	f.mu.Lock()
	f.subs[ch] = struct{}{}
	f.mu.Unlock()
	return ch, func() {
		f.mu.Lock()
		delete(f.subs, ch)
		f.mu.Unlock()
	}
}

func (f *changeFeed) notify() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for ch := range f.subs {
		select {
		case ch <- struct{}{}:
		default: // a wake-up is already pending
		}
	}
}

// Subscribe returns a channel that signals after any change to a Participant or
// the Event State, and a function to stop listening.
func (db *DB) Subscribe() (<-chan struct{}, func()) {
	return db.changes.subscribe()
}

// changed announces a write to subscribers.
func (db *DB) changed() {
	db.changes.notify()
}
