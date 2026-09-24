package main

import (
	"strings"
	"testing"
	"time"
)

// fakeEnv builds a lookup function for SelectLLM from a plain map, so tests
// never touch the process environment (#38's "injected lookup" requirement).
func fakeEnv(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

func asOpenAI(t *testing.T, llm LLM) *OpenAIClient {
	t.Helper()
	c, ok := llm.(*OpenAIClient)
	if !ok {
		t.Fatalf("want *OpenAIClient, got %T", llm)
	}
	return c
}

func TestSelectLLM_MistralIsTheDefaultProvider(t *testing.T) {
	llm, err := SelectLLM(fakeEnv(map[string]string{"LLM_API_KEY": "key"}))
	if err != nil {
		t.Fatal(err)
	}

	c := asOpenAI(t, llm)
	if c.baseURL != "https://api.mistral.ai/v1" || c.model != "mistral-medium-latest" || c.apiKey != "key" {
		t.Errorf("mistral defaults: %+v", c)
	}
	if !c.jsonMode {
		t.Error("JSON mode should default to on")
	}
	if c.httpClient.Timeout != 30*time.Second {
		t.Errorf("timeout: got %v", c.httpClient.Timeout)
	}
}

func TestSelectLLM_ScalewayDefaults(t *testing.T) {
	llm, err := SelectLLM(fakeEnv(map[string]string{"LLM_PROVIDER": "scaleway", "LLM_API_KEY": "iam-key"}))
	if err != nil {
		t.Fatal(err)
	}

	c := asOpenAI(t, llm)
	if c.baseURL != "https://api.scaleway.ai/v1" || c.model != "mistral-small-3.2-24b-instruct-2506" || c.apiKey != "iam-key" {
		t.Errorf("scaleway defaults: %+v", c)
	}
}

func TestSelectLLM_EveryLLMSettingOverridesTheProviderDefault(t *testing.T) {
	llm, err := SelectLLM(fakeEnv(map[string]string{
		"LLM_PROVIDER":  "mistral",
		"LLM_API_KEY":   "key",
		"LLM_BASE_URL":  "https://mistral.internal/v1",
		"LLM_MODEL":     "mistral-large-latest",
		"LLM_TIMEOUT":   "5s",
		"LLM_JSON_MODE": "false",
	}))
	if err != nil {
		t.Fatal(err)
	}

	c := asOpenAI(t, llm)
	if c.baseURL != "https://mistral.internal/v1" || c.model != "mistral-large-latest" {
		t.Errorf("overrides: %+v", c)
	}
	if c.jsonMode {
		t.Error("LLM_JSON_MODE=false should turn JSON mode off")
	}
	if c.httpClient.Timeout != 5*time.Second {
		t.Errorf("LLM_TIMEOUT override: got %v", c.httpClient.Timeout)
	}
}

func TestSelectLLM_LegacyMistralAPIKeyStillWorksWhenLLMAPIKeyIsUnset(t *testing.T) {
	llm, err := SelectLLM(fakeEnv(map[string]string{"MISTRAL_API_KEY": "legacy-key"}))
	if err != nil {
		t.Fatal(err)
	}

	if got := asOpenAI(t, llm).apiKey; got != "legacy-key" {
		t.Errorf("want the legacy key, got %q", got)
	}
}

func TestSelectLLM_LLMAPIKeyTakesPrecedenceOverTheLegacyKey(t *testing.T) {
	llm, err := SelectLLM(fakeEnv(map[string]string{"LLM_API_KEY": "new-key", "MISTRAL_API_KEY": "legacy-key"}))
	if err != nil {
		t.Fatal(err)
	}

	if got := asOpenAI(t, llm).apiKey; got != "new-key" {
		t.Errorf("want the new key, got %q", got)
	}
}

func TestSelectLLM_TheLegacyKeyDoesNotApplyToScaleway(t *testing.T) {
	_, err := SelectLLM(fakeEnv(map[string]string{"LLM_PROVIDER": "scaleway", "MISTRAL_API_KEY": "legacy-key"}))

	if err == nil || !strings.Contains(err.Error(), "LLM_API_KEY") {
		t.Errorf("scaleway needs its own key, got %v", err)
	}
}

func TestSelectLLM_UnknownProviderIsAnError(t *testing.T) {
	_, err := SelectLLM(fakeEnv(map[string]string{"LLM_PROVIDER": "openai"}))

	if err == nil || !strings.Contains(err.Error(), "openai") {
		t.Errorf("want an error naming the unknown provider, got %v", err)
	}
}

func TestSelectLLM_MissingKeyIsAnError(t *testing.T) {
	for _, provider := range []string{"mistral", "scaleway"} {
		vars := map[string]string{}
		if provider != "mistral" {
			vars["LLM_PROVIDER"] = provider
		}

		_, err := SelectLLM(fakeEnv(vars))

		if err == nil || !strings.Contains(err.Error(), "LLM_API_KEY") {
			t.Errorf("%s: want a missing-key error, got %v", provider, err)
		}
	}
}

func TestSelectLLM_InvalidTimeoutIsAnError(t *testing.T) {
	_, err := SelectLLM(fakeEnv(map[string]string{"LLM_API_KEY": "key", "LLM_TIMEOUT": "not-a-duration"}))

	if err == nil || !strings.Contains(err.Error(), "LLM_TIMEOUT") {
		t.Errorf("want an invalid-timeout error, got %v", err)
	}
}

func TestSelectLLM_InvalidJSONModeIsAnError(t *testing.T) {
	_, err := SelectLLM(fakeEnv(map[string]string{"LLM_API_KEY": "key", "LLM_JSON_MODE": "sometimes"}))

	if err == nil || !strings.Contains(err.Error(), "LLM_JSON_MODE") {
		t.Errorf("want an invalid-json-mode error, got %v", err)
	}
}
