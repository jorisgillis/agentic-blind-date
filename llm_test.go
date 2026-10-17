package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

const chatPath = "/chat/completions"

// instantClock is a Clock whose sleeps return at once, so retry tests run instantly.
type instantClock struct{}

func (instantClock) Sleep(time.Duration) {}

// openAIFor builds an OpenAIClient against the fake upstream with an
// instant clock, so retry tests run instantly.
func openAIFor(u *upstream, apiKey, model string, jsonMode bool) *OpenAIClient {
	return NewOpenAIClient("https://llm.test", apiKey, model, jsonMode, u.client(), instantClock{})
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
		must(t, json.Unmarshal([]byte(u.bodies[0]), &req))
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
		"error.message":             {`{"error": {"message": "bad key"}}`, "bad key"},
		"top-level message":         {`{"message": "slow down"}`, "slow down"},
		"detail string":             {`{"detail": "invalid request"}`, "invalid request"},
		"detail structured":         {`{"detail": [{"loc": ["body"], "msg": "field required"}]}`, "field required"},
		"detail multiple errors":    {`{"detail": [{"msg": "field required"}, {"msg": "must be positive"}]}`, "field required; must be positive"},
		"detail unrecognized shape": {`{"detail": {"foo": "bar"}}`, `{"foo": "bar"}`},
		"raw body":                  {`not json at all`, "not json at all"},
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

// TestOpenAIClient_RetryPolicyByStatus consolidates the retry rules into one
// table: transient statuses (429 and 5xx) are retried up to three attempts
// before giving up, while permanent 4xx failures return after a single attempt.
func TestOpenAIClient_RetryPolicyByStatus(t *testing.T) {
	for name, tc := range map[string]struct {
		status    int
		wantTries int
	}{
		"429 is retried":   {429, 3},
		"500 is retried":   {500, 3},
		"502 is retried":   {502, 3},
		"503 is retried":   {503, 3},
		"504 is retried":   {504, 3},
		"400 is permanent": {400, 1},
		"401 is permanent": {401, 1},
		"403 is permanent": {403, 1},
		"404 is permanent": {404, 1},
		"422 is permanent": {422, 1},
	} {
		t.Run(name, func(t *testing.T) {
			u := newUpstream().on(chatPath, tc.status, `{"error": {"message": "nope"}}`)

			_, err := openAIFor(u, "key", "m", true).Chat("s", "u")

			if err == nil {
				t.Fatalf("%d: want an error", tc.status)
			}
			if len(u.requests) != tc.wantTries {
				t.Errorf("%d: attempts: want %d, got %d", tc.status, tc.wantTries, len(u.requests))
			}
		})
	}
}

// TestOpenAIClient_A429HintsAtASpendingCap covers #35 (via #103): a 429 error
// must hint that a workspace or organisation spending cap can masquerade as an
// ordinary rate limit.
func TestOpenAIClient_A429HintsAtASpendingCap(t *testing.T) {
	u := newUpstream().on(chatPath, 429, `{"error": {"message": "slow down"}}`)

	_, err := openAIFor(u, "key", "m", true).Chat("s", "u")

	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("want the 429 status in the error, got %v", err)
	}
	if !strings.Contains(err.Error(), "spending cap") {
		t.Errorf("a 429 should hint at a workspace or organisation spending cap, got %v", err)
	}
}

// TestOpenAIClient_AMalformedBaseURLReturnsAClearError covers #103: a
// configured base URL that cannot be parsed must return a clear error from
// doChat instead of nil-panicking on the request later.
func TestOpenAIClient_AMalformedBaseURLReturnsAClearError(t *testing.T) {
	c := NewOpenAIClient("http://\x7f.example", "key", "m", true, &http.Client{}, instantClock{})

	_, err := c.Chat("s", "u")

	if err == nil || !strings.Contains(err.Error(), "base URL") {
		t.Errorf("want a clear invalid-base-URL error, got %v", err)
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
	c := NewOpenAIClient("", "key", "m", true, nil, instantClock{})

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
			c := NewOpenAIClient("https://llm.test", "key", "m", true, &http.Client{Transport: transport}, instantClock{})
			if _, err := c.Chat("s", "u"); err == nil {
				t.Error("want an error")
			}
		})
	}
}

func TestOpenAIClient_ATimeoutIsRetriedLikeAnyOtherTransientFailure(t *testing.T) {
	u := newUpstream().on(chatPath, 200, `{"choices": [{"message": {"content": "hi"}}]}`)
	calls := 0
	c := NewOpenAIClient("https://llm.test", "key", "m", true, &http.Client{Timeout: 20 * time.Millisecond}, instantClock{})
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
	c := NewOpenAIClient("", "key", "m", true, nil, instantClock{})

	if c.retryDelay(1) != 2*time.Second || c.retryDelay(2) != 4*time.Second {
		t.Errorf("want 2s then 4s, got %v then %v", c.retryDelay(1), c.retryDelay(2))
	}
}

func TestRealClock_SleepsForTheGivenDuration(t *testing.T) {
	start := time.Now()
	realClock{}.Sleep(time.Millisecond)
	if elapsed := time.Since(start); elapsed < time.Millisecond {
		t.Errorf("realClock.Sleep returned after %v, want at least 1ms", elapsed)
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

// blockingLLM counts how many calls run at once and blocks every call until
// released, so a gate's bound is observable.
type blockingLLM struct {
	mu      sync.Mutex
	running int
	maxSeen int
	release chan struct{}
}

func newBlockingLLM() *blockingLLM { return &blockingLLM{release: make(chan struct{})} }

func (b *blockingLLM) Chat(system, user string) (string, error) {
	b.mu.Lock()
	b.running++
	if b.running > b.maxSeen {
		b.maxSeen = b.running
	}
	b.mu.Unlock()
	<-b.release
	b.mu.Lock()
	b.running--
	b.mu.Unlock()
	return "ok", nil
}

func (b *blockingLLM) maxConcurrent() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.maxSeen
}

// TestGatedLLM_BoundsConcurrentCalls covers the shared LLM call bound: with
// a limit of 2, six parallel calls never run more than two at once — the
// rush of registrations queues instead of storming the provider.
func TestGatedLLM_BoundsConcurrentCalls(t *testing.T) {
	inner := newBlockingLLM()
	gated := NewGatedLLM(inner, 2)

	const calls = 6
	done := make(chan struct{}, calls)
	for i := 0; i < calls; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			if _, err := gated.Chat("s", "u"); err != nil {
				t.Error(err)
			}
		}()
	}

	// Two calls run; the rest wait at the gate.
	eventually(t, "two calls running", func() bool {
		inner.mu.Lock()
		defer inner.mu.Unlock()
		return inner.running == 2
	})
	if got := inner.maxConcurrent(); got != 2 {
		t.Fatalf("want at most 2 concurrent calls, saw %d", got)
	}

	close(inner.release)
	for i := 0; i < calls; i++ {
		<-done
	}
	if got := inner.maxConcurrent(); got != 2 {
		t.Errorf("the bound held after the release: saw %d", got)
	}
}

// TestGatedLLM_ALimitBelowOneIsOne covers the clamp: a misconfigured bound
// still serializes instead of panicking or blocking forever.
func TestGatedLLM_ALimitBelowOneIsOne(t *testing.T) {
	inner := newBlockingLLM()
	gated := NewGatedLLM(inner, 0)

	done := make(chan struct{}, 3)
	for i := 0; i < 3; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			_, _ = gated.Chat("s", "u")
		}()
	}
	eventually(t, "one call running", func() bool {
		inner.mu.Lock()
		defer inner.mu.Unlock()
		return inner.running == 1
	})
	close(inner.release)
	for i := 0; i < 3; i++ {
		<-done
	}
	if got := inner.maxConcurrent(); got != 1 {
		t.Errorf("a limit below 1 is 1: saw %d concurrent", got)
	}
}

func TestLLMConcurrency_ReadsTheEnvAndDefaults(t *testing.T) {
	env := func(v string) func(string) string {
		return func(k string) string {
			if k == "LLM_CONCURRENCY" {
				return v
			}
			return ""
		}
	}
	if got := LLMConcurrency(env("")); got != defaultLLMConcurrency {
		t.Errorf("unset: want the default %d, got %d", defaultLLMConcurrency, got)
	}
	if got := LLMConcurrency(env("7")); got != 7 {
		t.Errorf("LLM_CONCURRENCY=7: want 7, got %d", got)
	}
	if got := LLMConcurrency(env("nope")); got != defaultLLMConcurrency {
		t.Errorf("malformed: want the default %d, got %d", defaultLLMConcurrency, got)
	}
	if got := LLMConcurrency(env("-3")); got != defaultLLMConcurrency {
		t.Errorf("non-positive: want the default %d, got %d", defaultLLMConcurrency, got)
	}
}
