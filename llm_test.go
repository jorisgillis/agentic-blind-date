package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

const chatPath = "/chat/completions"

// openAIFor builds an OpenAIClient against the fake upstream with a zero
// retry delay, so retry tests run instantly.
func openAIFor(u *upstream, apiKey, model string, jsonMode bool) *OpenAIClient {
	c := NewOpenAIClient("https://llm.test", apiKey, model, jsonMode, u.client())
	c.retryDelay = func(int) time.Duration { return 0 }
	return c
}

func TestOpenAIClient_SendsBothPromptsWithAnExplicitTemperatureAndReturnsTheReply(t *testing.T) {
	u := newUpstream().on(chatPath, 200, `{"choices": [{"message": {"content": "hello"}}]}`)

	got, err := openAIFor(u, "key", "a-model", true).Chat("be nice", "hi")

	if err != nil || got != "hello" {
		t.Fatalf("want hello, got %q (err %v)", got, err)
	}
	var req chatReq
	if err := json.Unmarshal([]byte(u.bodies[0]), &req); err != nil {
		t.Fatal(err)
	}
	if req.Model != "a-model" || len(req.Messages) != 2 || req.Messages[0].Content != "be nice" || req.Messages[1].Content != "hi" {
		t.Errorf("request body: %+v", req)
	}
	if req.Temperature != 0.7 {
		t.Errorf("temperature should always be sent explicitly, got %v", req.Temperature)
	}
	if req.Stream {
		t.Error("stream should be false")
	}
	if got := u.requests[0].Header.Get("Authorization"); got != "Bearer key" {
		t.Errorf("Authorization header: got %q", got)
	}
}

func TestOpenAIClient_WithNoKeySendsNoAuthorizationHeader(t *testing.T) {
	u := newUpstream().on(chatPath, 200, `{"choices": [{"message": {"content": "hi"}}]}`)

	if _, err := openAIFor(u, "", "m", true).Chat("s", "u"); err != nil {
		t.Fatal(err)
	}

	if got := u.requests[0].Header.Get("Authorization"); got != "" {
		t.Errorf("no key means no Authorization header, got %q", got)
	}
}

func TestOpenAIClient_JSONModeOnAndOff(t *testing.T) {
	for _, jsonMode := range []bool{true, false} {
		u := newUpstream().on(chatPath, 200, `{"choices": [{"message": {"content": "hi"}}]}`)

		if _, err := openAIFor(u, "key", "m", jsonMode).Chat("s", "u"); err != nil {
			t.Fatal(err)
		}

		var req chatReq
		json.Unmarshal([]byte(u.bodies[0]), &req)
		hasFormat := req.ResponseFormat != nil && req.ResponseFormat.Type == "json_object"
		if hasFormat != jsonMode {
			t.Errorf("jsonMode=%v: response_format present=%v", jsonMode, hasFormat)
		}
	}
}

func TestOpenAIClient_ErrorTextPrefersErrorMessageThenTopLevelMessageThenDetailThenRawBody(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		want string
	}{
		"error.message":     {`{"error": {"message": "bad key"}}`, "bad key"},
		"top-level message": {`{"message": "slow down"}`, "slow down"},
		"detail string":     {`{"detail": "invalid request"}`, "invalid request"},
		"detail structured": {`{"detail": [{"loc": ["body"], "msg": "field required"}]}`, "field required"},
		"raw body":          {`not json at all`, "not json at all"},
	} {
		t.Run(name, func(t *testing.T) {
			u := newUpstream().on(chatPath, 400, tc.body)

			_, err := openAIFor(u, "key", "m", true).Chat("s", "u")

			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want error containing %q, got %v", tc.want, err)
			}
			if err == nil || !strings.Contains(err.Error(), "400") {
				t.Errorf("want the status code in the error, got %v", err)
			}
		})
	}
}

func TestOpenAIClient_RetriesTransientFailures(t *testing.T) {
	attempts := 0
	u := newUpstream().onFunc(chatPath, func(*http.Request) (int, string) {
		attempts++
		if attempts < 3 {
			return 429, `{"error": {"message": "slow down"}}`
		}
		return 200, `{"choices": [{"message": {"content": "finally"}}]}`
	})

	got, err := openAIFor(u, "key", "m", true).Chat("s", "u")

	if err != nil || got != "finally" || attempts != 3 {
		t.Errorf("want success on attempt 3, got %q after %d attempts (err %v)", got, attempts, err)
	}
}

func TestOpenAIClient_RetriesEvery5xxAndGivesUpAfterThreeAttempts(t *testing.T) {
	for _, status := range []int{500, 502, 503, 504} {
		u := newUpstream().on(chatPath, status, "boom")

		_, err := openAIFor(u, "key", "m", true).Chat("s", "u")

		if err == nil {
			t.Fatalf("%d: want an error", status)
		}
		if len(u.requests) != 3 {
			t.Errorf("%d: attempts: want 3, got %d", status, len(u.requests))
		}
	}
}

func TestOpenAIClient_NeverRetriesPermanentFailures(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 422} {
		u := newUpstream().on(chatPath, status, `{"error": {"message": "nope"}}`)

		_, err := openAIFor(u, "key", "m", true).Chat("s", "u")

		if err == nil {
			t.Fatalf("%d: want an error", status)
		}
		if len(u.requests) != 1 {
			t.Errorf("%d: should not be retried, got %d attempts", status, len(u.requests))
		}
	}
}

func TestOpenAIClient_MalformedRepliesAreErrorsAndNotRetried(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
		want   string
	}{
		"no choices": {200, `{"choices": []}`, "no choices"},
		"not json":   {200, `<html>`, "parse error"},
	} {
		t.Run(name, func(t *testing.T) {
			u := newUpstream().on(chatPath, tc.status, tc.body)

			_, err := openAIFor(u, "key", "m", true).Chat("s", "u")

			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want error containing %q, got %v", tc.want, err)
			}
			if len(u.requests) != 1 {
				t.Errorf("should not be retried, got %d attempts", len(u.requests))
			}
		})
	}
}

func TestOpenAIClient_WithoutAnHTTPClient(t *testing.T) {
	c := NewOpenAIClient("", "key", "m", true, nil)
	c.retryDelay = func(int) time.Duration { return 0 }

	if _, err := c.Chat("s", "u"); err == nil {
		t.Error("want error without an HTTP client")
	}
}

func TestOpenAIClient_NetworkFailures(t *testing.T) {
	for name, transport := range map[string]http.RoundTripper{
		"unreachable":     brokenTransport{},
		"body breaks off": truncatedTransport{},
	} {
		t.Run(name, func(t *testing.T) {
			c := NewOpenAIClient("https://llm.test", "key", "m", true, &http.Client{Transport: transport})
			c.retryDelay = func(int) time.Duration { return 0 }
			if _, err := c.Chat("s", "u"); err == nil {
				t.Error("want an error")
			}
		})
	}
}

func TestOpenAIClient_ATimeoutIsRetriedLikeAnyOtherTransientFailure(t *testing.T) {
	u := newUpstream().on(chatPath, 200, `{"choices": [{"message": {"content": "hi"}}]}`)
	calls := 0
	c := NewOpenAIClient("https://llm.test", "key", "m", true, &http.Client{Timeout: 20 * time.Millisecond})
	c.retryDelay = func(int) time.Duration { return 0 }
	c.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls < 2 {
			<-req.Context().Done()
			return nil, req.Context().Err()
		}
		return u.RoundTrip(req)
	})

	got, err := c.Chat("s", "u")

	if err != nil || got != "hi" {
		t.Errorf("a timeout should be retried like any transient failure, got %q (err %v)", got, err)
	}
	if calls != 2 {
		t.Errorf("want 2 calls (one timeout, one success), got %d", calls)
	}
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOpenAIClient_BacksOffExponentially(t *testing.T) {
	c := NewOpenAIClient("", "key", "m", true, nil)

	if c.retryDelay(1) != 2*time.Second || c.retryDelay(2) != 4*time.Second {
		t.Errorf("want 2s then 4s, got %v then %v", c.retryDelay(1), c.retryDelay(2))
	}
}

// TestOpenAIClient_Ollama covers #39: an Ollama-shaped adapter (no key, its
// model) still treats an unpulled model (404) as a configuration error and a
// full queue (503) as transient.
func TestOpenAIClient_Ollama(t *testing.T) {
	t.Run("a model that hasn't been pulled is a configuration error, not retried", func(t *testing.T) {
		u := newUpstream().on(chatPath, 404, `{"error": "model 'llama3.1:8b' not found, try pulling it first"}`)

		_, err := openAIFor(u, "", "llama3.1:8b", true).Chat("s", "u")

		if err == nil || !strings.Contains(err.Error(), "404") {
			t.Errorf("want a 404 error, got %v", err)
		}
		if len(u.requests) != 1 {
			t.Errorf("a 404 should not be retried, got %d attempts", len(u.requests))
		}
	})

	t.Run("a full queue is retried", func(t *testing.T) {
		attempts := 0
		u := newUpstream().onFunc(chatPath, func(*http.Request) (int, string) {
			attempts++
			if attempts < 2 {
				return 503, `{"error": "queue is full"}`
			}
			return 200, `{"choices": [{"message": {"content": "hi"}}]}`
		})

		got, err := openAIFor(u, "", "llama3.1:8b", true).Chat("s", "u")

		if err != nil || got != "hi" || attempts != 2 {
			t.Errorf("want success after a retried 503, got %q after %d attempts (err %v)", got, attempts, err)
		}
	})
}
