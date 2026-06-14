package main

import (
	"testing"
)

func TestDefaultMatchResult(t *testing.T) {
	result := defaultMatchResult()

	if result.Score != 42 {
		t.Errorf("expected score 42, got %d", result.Score)
	}
	if result.Reason == "" {
		t.Error("expected non-empty reason")
	}
	if len(result.RedFlags) != 0 {
		t.Errorf("expected 0 red flags, got %d", len(result.RedFlags))
	}
	if len(result.GreenFlags) != 1 {
		t.Errorf("expected 1 green flag, got %d", len(result.GreenFlags))
	}
	if len(result.Icebreakers) != 3 {
		t.Errorf("expected 3 icebreakers, got %d", len(result.Icebreakers))
	}
}
