package main

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// providerDefaults are one LLM provider's sensible defaults: an organiser
// only needs to set a key (or nothing) to get started (#35).
type providerDefaults struct {
	baseURL     string
	model       string
	timeout     time.Duration
	keyRequired bool
}

var llmProviders = map[string]providerDefaults{
	"mistral": {
		baseURL:     "https://api.mistral.ai/v1",
		model:       "mistral-medium-latest",
		timeout:     30 * time.Second,
		keyRequired: true,
	},
	"scaleway": {
		baseURL:     "https://api.scaleway.ai/v1",
		model:       "mistral-small-3.2-24b-instruct-2506",
		timeout:     30 * time.Second,
		keyRequired: true,
	},
	"ollama": {
		baseURL: "http://localhost:11434/v1",
		// no default model: Ollama has no universal one, so the organiser
		// names one they've pulled (for example llama3.1:8b)
		timeout: 180 * time.Second, // a local model can take minutes to load on first use
	},
}

// SelectLLM reads the LLM_* settings through lookup (never the process
// environment directly, so this stays testable) and returns a configured
// LLM, or an error for an unknown provider or a missing required setting.
// main() calls this once and exits on error (ADR-0005): nothing else reads
// these settings.
func SelectLLM(lookup func(string) string) (LLM, error) {
	provider := lookup("LLM_PROVIDER")
	if provider == "" {
		provider = "mistral"
	}
	defaults, ok := llmProviders[provider]
	if !ok {
		return nil, fmt.Errorf("unknown LLM_PROVIDER %q (want mistral, scaleway or ollama)", provider)
	}

	apiKey := lookup("LLM_API_KEY")
	if apiKey == "" && provider == "mistral" {
		apiKey = lookup("MISTRAL_API_KEY") // legacy fallback
	}
	if apiKey == "" && defaults.keyRequired {
		hint := ""
		if provider == "mistral" {
			hint = " (or MISTRAL_API_KEY)"
		}
		return nil, fmt.Errorf("LLM_PROVIDER=%s requires LLM_API_KEY%s", provider, hint)
	}

	model := lookup("LLM_MODEL")
	if model == "" {
		model = defaults.model
	}
	if model == "" {
		return nil, fmt.Errorf("LLM_PROVIDER=%s requires LLM_MODEL (for example llama3.1:8b)", provider)
	}

	baseURL := lookup("LLM_BASE_URL")
	if baseURL == "" {
		baseURL = defaults.baseURL
	}

	timeout := defaults.timeout
	if v := lookup("LLM_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid LLM_TIMEOUT %q: %w", v, err)
		}
		timeout = d
	}

	jsonMode := true
	if v := lookup("LLM_JSON_MODE"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid LLM_JSON_MODE %q: %w", v, err)
		}
		jsonMode = b
	}

	return NewOpenAIClient(baseURL, apiKey, model, jsonMode, &http.Client{Timeout: timeout}), nil
}
