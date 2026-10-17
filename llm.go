package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// LLM sends a system and user prompt to a language model and returns its reply.
// OpenAIClient is the production adapter; tests use an in-memory fake.
type LLM interface {
	Chat(system, user string) (string, error)
}

// Clock paces the wait between retries: production sleeps for real, tests
// inject a clock that returns at once so retry tests run instantly.
type Clock interface {
	Sleep(d time.Duration)
}

// realClock is the production Clock.
type realClock struct{}

func (realClock) Sleep(d time.Duration) { time.Sleep(d) }

// OpenAIClient is an OpenAI-compatible chat completions adapter. Mistral,
// Scaleway Generative APIs and Ollama all expose the same request and
// response shape at POST {base}/chat/completions, so one adapter, configured
// with a base URL, key, model and JSON mode, serves every provider (#35).
type OpenAIClient struct {
	baseURL    string
	apiKey     string
	model      string
	jsonMode   bool
	httpClient *http.Client
	clock      Clock
}

// NewOpenAIClient creates an OpenAIClient. apiKey may be empty (no
// Authorization header is sent, as Ollama needs none). httpClient's Timeout
// bounds every call. clock paces the back-off between retries.
func NewOpenAIClient(baseURL, apiKey, model string, jsonMode bool, httpClient *http.Client, clock Clock) *OpenAIClient {
	return &OpenAIClient{
		baseURL:    baseURL,
		apiKey:     apiKey,
		model:      model,
		jsonMode:   jsonMode,
		httpClient: httpClient,
		clock:      clock,
	}
}

// retryDelay is the exponential back-off before retry attempt 1, 2, ...
func (c *OpenAIClient) retryDelay(attempt int) time.Duration {
	return time.Duration(1<<uint(attempt)) * time.Second
}

type chatReq struct {
	Model          string          `json:"model"`
	Messages       []chatMsg       `json:"messages"`
	Temperature    float64         `json:"temperature"`
	MaxTokens      int             `json:"max_tokens"`
	Stream         bool            `json:"stream"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type chatMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatResp struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// chatError distinguishes failures worth retrying (network errors, timeouts,
// 429, 5xx) from permanent ones (other 4xx, a parse error, no choices): only
// the former are retried.
type chatError struct {
	msg       string
	retryable bool
}

func (e *chatError) Error() string { return e.msg }

func retryableStatus(status int) bool {
	switch status {
	case 429, 500, 502, 503, 504:
		return true
	default:
		return false
	}
}

func (c *OpenAIClient) doChat(system, user string) (string, error) {
	if c.httpClient == nil {
		return "", &chatError{msg: "httpClient not initialized"}
	}
	req := chatReq{
		Model: c.model,
		Messages: []chatMsg{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: 0.7,
		MaxTokens:   800,
		Stream:      false,
	}
	if c.jsonMode {
		req.ResponseFormat = &responseFormat{Type: "json_object"}
	}
	body, _ := json.Marshal(req)

	httpReq, err := http.NewRequest("POST", c.baseURL+"/chat/completions", bytes.NewReader(body)) //nolint:gosec // G704: the base URL is operator configuration, not user input
	if err != nil {
		return "", &chatError{msg: fmt.Sprintf("LLM: invalid base URL %q: %v", c.baseURL, err)}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq) //nolint:gosec // G704: carries the operator-configured base URL, not user input
	if err != nil {
		return "", &chatError{msg: err.Error(), retryable: true}
	}
	// The body is drained below before the return, so the close cannot matter.
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", &chatError{msg: err.Error(), retryable: true}
	}

	if resp.StatusCode != http.StatusOK {
		text := errorText(respBody)
		if resp.StatusCode == http.StatusNotFound {
			logger("llm").Warn("model not found — try pulling it first", "model", c.model, "detail", text)
		}
		msg := fmt.Sprintf("LLM HTTP %d: %s", resp.StatusCode, text)
		if resp.StatusCode == http.StatusTooManyRequests {
			// #35: a 429 often means a workspace or organisation spending cap, not a rate limit.
			msg += " — this may be a workspace or organisation spending cap rather than an ordinary rate limit"
		}
		return "", &chatError{
			msg:       msg,
			retryable: retryableStatus(resp.StatusCode),
		}
	}

	var result chatResp
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", &chatError{msg: fmt.Sprintf("LLM parse error: %v (body: %s)", err, respBody)}
	}
	if len(result.Choices) == 0 {
		return "", &chatError{msg: "LLM returned no choices"}
	}
	return result.Choices[0].Message.Content, nil
}

// errorText reads a provider's error explanation from a failed response
// body: error.message (Mistral, Scaleway), a top-level message (Scaleway's
// older shape), detail (Mistral validation errors), and finally the raw body.
func errorText(body []byte) string {
	var e struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string          `json:"message"`
		Detail  json.RawMessage `json:"detail"`
	}
	if json.Unmarshal(body, &e) == nil {
		if e.Error != nil && e.Error.Message != "" {
			return e.Error.Message
		}
		if e.Message != "" {
			return e.Message
		}
		if len(e.Detail) > 0 {
			if text := detailText(e.Detail); text != "" {
				return text
			}
		}
	}
	return string(body)
}

// detailText reads a validation-error "detail", either a plain string or
// FastAPI's shape (Mistral's own 422s): a list of {"msg": "..."} objects,
// joined into one line. Anything else falls back to the raw JSON.
func detailText(detail json.RawMessage) string {
	var s string
	if json.Unmarshal(detail, &s) == nil && s != "" {
		return s
	}
	var items []struct {
		Msg string `json:"msg"`
	}
	if json.Unmarshal(detail, &items) == nil && len(items) > 0 {
		msgs := make([]string, 0, len(items))
		for _, item := range items {
			if item.Msg != "" {
				msgs = append(msgs, item.Msg)
			}
		}
		if len(msgs) > 0 {
			return strings.Join(msgs, "; ")
		}
	}
	return string(detail)
}

// Chat sends the system and user prompts and returns the reply, retrying a
// transient failure up to twice more with back-off. A permanent failure
// returns immediately, so a caller's fallback doesn't wait through pointless
// retries.
func (c *OpenAIClient) Chat(system, user string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			c.clock.Sleep(c.retryDelay(attempt))
		}
		result, err := c.doChat(system, user)
		if err == nil {
			return result, nil
		}
		lastErr = err
		var ce *chatError
		if errors.As(err, &ce) && !ce.retryable {
			break
		}
	}
	return "", lastErr
}

// defaultLLMConcurrency is the shared bound on concurrent LLM calls across
// the whole app, overridable with LLM_CONCURRENCY. Onboarding starts a
// goroutine per Participant and each one calls the LLM (custom questions,
// persona), so without a shared ceiling a rush of registrations fires dozens
// of parallel calls into the provider, which rate-limits — and every call
// then fails into retries: the long, silent wait the first live event saw,
// and the failed match assessments behind every 42% score. The Matcher's
// two-at-a-time pool and the Mission Agent's workers keep their own bounds;
// this gate is the ceiling under them all.
var defaultLLMConcurrency = 4

// gatedLLM wraps an LLM with a shared concurrency bound: at most limit calls
// run at once, the rest wait their turn in arrival order.
type gatedLLM struct {
	llm LLM
	sem chan struct{}
}

// NewGatedLLM wraps llm so at most limit calls run concurrently. A limit
// below 1 is treated as 1.
func NewGatedLLM(llm LLM, limit int) LLM {
	if limit < 1 {
		limit = 1
	}
	return &gatedLLM{llm: llm, sem: make(chan struct{}, limit)}
}

// Chat runs the wrapped LLM once a slot is free.
func (g *gatedLLM) Chat(system, user string) (string, error) {
	g.sem <- struct{}{}
	defer func() { <-g.sem }()
	return g.llm.Chat(system, user)
}

// LLMConcurrency reads the shared LLM call bound through lookup (never the
// process environment directly, so this stays testable), defaulting to
// defaultLLMConcurrency. main() calls this once.
func LLMConcurrency(lookup func(string) string) int {
	if v := lookup("LLM_CONCURRENCY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultLLMConcurrency
}
