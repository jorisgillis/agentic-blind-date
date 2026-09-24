package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// LLM sends a system and user prompt to a language model and returns its reply.
// OpenAIClient is the production adapter; tests use an in-memory fake.
type LLM interface {
	Chat(system, user string) (string, error)
}

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
	retryDelay func(attempt int) time.Duration // wait before retry attempt 1, 2, ...
}

// NewOpenAIClient creates an OpenAIClient. apiKey may be empty (no
// Authorization header is sent, as Ollama needs none). httpClient's Timeout
// bounds every call.
func NewOpenAIClient(baseURL, apiKey, model string, jsonMode bool, httpClient *http.Client) *OpenAIClient {
	return &OpenAIClient{
		baseURL:    baseURL,
		apiKey:     apiKey,
		model:      model,
		jsonMode:   jsonMode,
		httpClient: httpClient,
		retryDelay: func(attempt int) time.Duration { return time.Duration(1<<uint(attempt)) * time.Second },
	}
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

	httpReq, _ := http.NewRequest("POST", c.baseURL+"/chat/completions", bytes.NewReader(body)) // built from a configured base URL
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", &chatError{msg: err.Error(), retryable: true}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", &chatError{msg: err.Error(), retryable: true}
	}

	if resp.StatusCode != http.StatusOK {
		text := errorText(respBody)
		if resp.StatusCode == http.StatusNotFound {
			log.Printf("LLM model %q not found (try pulling it first): %s", c.model, text)
		}
		return "", &chatError{
			msg:       fmt.Sprintf("LLM HTTP %d: %s", resp.StatusCode, text),
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
			var s string
			if json.Unmarshal(e.Detail, &s) == nil && s != "" {
				return s
			}
			return string(e.Detail)
		}
	}
	return string(body)
}

// Chat sends the system and user prompts and returns the reply, retrying a
// transient failure up to twice more with back-off. A permanent failure
// returns immediately, so a caller's fallback doesn't wait through pointless
// retries.
func (c *OpenAIClient) Chat(system, user string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(c.retryDelay(attempt))
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

// extractJSON returns the outermost {...} of an LLM reply, which may wrap the
// JSON in prose or markdown fences.
func extractJSON(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start == -1 || end == -1 || end <= start {
		return s
	}
	return s[start : end+1]
}
