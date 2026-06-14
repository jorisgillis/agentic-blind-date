package main

import (
	"net/http"
	"testing"
)

func TestNewMistralClient(t *testing.T) {
	// Test with empty API key
	client := NewMistralClient("", "", &http.Client{})
	if client.apiKey != "" {
		t.Errorf("expected empty apiKey, got %s", client.apiKey)
	}
	if client.model != "" {
		t.Errorf("expected empty model, got %s", client.model)
	}

	// Test with values
	client = NewMistralClient("test-key", "test-model", &http.Client{})
	if client.apiKey != "test-key" {
		t.Errorf("expected apiKey 'test-key', got %s", client.apiKey)
	}
	if client.model != "test-model" {
		t.Errorf("expected model 'test-model', got %s", client.model)
	}
}
