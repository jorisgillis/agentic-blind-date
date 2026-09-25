package main

import "time"

// Clock abstracts real time so a stream's heartbeat can be tested without
// waiting on the wall clock (#58): NewTicker returns a channel that fires
// every d, and a function to stop it — the same shape as time.NewTicker.
type Clock interface {
	NewTicker(d time.Duration) (<-chan time.Time, func())
}

// realClock is production's Clock.
type realClock struct{}

func (realClock) NewTicker(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTicker(d)
	return t.C, t.Stop
}

// Watcher lets an SSE stream heartbeat on its Clock and report when it has
// caught up: looked at the current state once. It is a constructor
// dependency (ADR-0005), not a test-only field set on Handler after
// construction or a package variable (#58).
type Watcher struct {
	clock          Clock
	heartbeatEvery time.Duration
	onCheck        func(path string)
}

// NewWatcher creates the production Watcher: a real Clock, and the interval
// that keeps idle SSE connections open through proxies.
func NewWatcher() *Watcher {
	return &Watcher{clock: realClock{}, heartbeatEvery: 25 * time.Second}
}

// heartbeat starts the Watcher's ticker.
func (w *Watcher) heartbeat() (<-chan time.Time, func()) {
	return w.clock.NewTicker(w.heartbeatEvery)
}

// caughtUp reports that a stream has looked at the current state for path.
func (w *Watcher) caughtUp(path string) {
	if w.onCheck != nil {
		w.onCheck(path)
	}
}
