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
		model:       "mistral-large-4",
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
	provider, defaults, err := resolveProvider(lookup)
	if err != nil {
		return nil, err
	}
	apiKey, err := resolveAPIKey(lookup, provider, defaults)
	if err != nil {
		return nil, err
	}
	model, err := resolveModel(lookup, provider, defaults)
	if err != nil {
		return nil, err
	}
	baseURL := withDefault(lookup("LLM_BASE_URL"), defaults.baseURL)
	timeout, jsonMode, err := resolveCallSettings(lookup, defaults)
	if err != nil {
		return nil, err
	}
	return NewOpenAIClient(baseURL, apiKey, model, jsonMode, &http.Client{Timeout: timeout}, realClock{}), nil
}

// resolveProvider reads LLM_PROVIDER, defaulting to mistral, with the
// provider's defaults — or an error for an unknown provider.
func resolveProvider(lookup func(string) string) (string, providerDefaults, error) {
	provider := withDefault(lookup("LLM_PROVIDER"), "mistral")
	defaults, ok := llmProviders[provider]
	if !ok {
		return "", providerDefaults{}, fmt.Errorf("unknown LLM_PROVIDER %q (want mistral, scaleway or ollama)", provider)
	}
	return provider, defaults, nil
}

// resolveAPIKey reads LLM_API_KEY, falling back to MISTRAL_API_KEY for the
// default provider — or an error when the provider requires a key and none
// is set.
func resolveAPIKey(lookup func(string) string, provider string, defaults providerDefaults) (string, error) {
	apiKey := lookup("LLM_API_KEY")
	if apiKey == "" && provider == "mistral" {
		apiKey = lookup("MISTRAL_API_KEY") // legacy fallback
	}
	if apiKey == "" && defaults.keyRequired {
		return "", missingAPIKeyError(provider)
	}
	return apiKey, nil
}

// missingAPIKeyError is the error for a provider that requires a key.
func missingAPIKeyError(provider string) error {
	hint := ""
	if provider == "mistral" {
		hint = " (or MISTRAL_API_KEY)"
	}
	return fmt.Errorf("LLM_PROVIDER=%s requires LLM_API_KEY%s", provider, hint)
}

// resolveModel reads LLM_MODEL, applying the provider's default model — or an
// error for a provider without one and no setting.
func resolveModel(lookup func(string) string, provider string, defaults providerDefaults) (string, error) {
	model := withDefault(lookup("LLM_MODEL"), defaults.model)
	if model == "" {
		return "", fmt.Errorf("LLM_PROVIDER=%s requires LLM_MODEL (for example llama3.1:8b)", provider)
	}
	return model, nil
}

// resolveCallSettings reads the call pacing and format settings: the timeout
// (the provider's default unless LLM_TIMEOUT overrides it) and JSON mode (on
// unless LLM_JSON_MODE turns it off).
func resolveCallSettings(lookup func(string) string, defaults providerDefaults) (timeout time.Duration, jsonMode bool, err error) {
	timeout, err = resolveTimeout(lookup, defaults.timeout)
	if err != nil {
		return 0, false, err
	}
	jsonMode, err = resolveJSONMode(lookup)
	if err != nil {
		return 0, false, err
	}
	return timeout, jsonMode, nil
}

// resolveTimeout parses LLM_TIMEOUT over the provider's default.
func resolveTimeout(lookup func(string) string, def time.Duration) (time.Duration, error) {
	v := lookup("LLM_TIMEOUT")
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("invalid LLM_TIMEOUT %q: %w", v, err)
	}
	return d, nil
}

// resolveJSONMode parses LLM_JSON_MODE, on by default.
func resolveJSONMode(lookup func(string) string) (bool, error) {
	v := lookup("LLM_JSON_MODE")
	if v == "" {
		return true, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("invalid LLM_JSON_MODE %q: %w", v, err)
	}
	return b, nil
}

// withDefault returns v, or def when v is empty.
func withDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
