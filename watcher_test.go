package main

import (
	"testing"
	"time"
)

func TestRealClock_TicksAndStops(t *testing.T) {
	ticks, stop := realClock{}.NewTicker(5 * time.Millisecond)
	defer stop()

	select {
	case <-ticks:
	case <-time.After(time.Second):
		t.Fatal("want a tick")
	}
}

func TestNewWatcher_UsesTheRealClock(t *testing.T) {
	w := NewWatcher()

	if _, ok := w.clock.(realClock); !ok {
		t.Errorf("want the real Clock, got %T", w.clock)
	}
}
