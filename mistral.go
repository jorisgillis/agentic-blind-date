package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type MistralClient struct {
	apiKey string
	model  string
}

type mistralReq struct {
	Model       string          `json:"model"`
	Messages    []mistralMsg    `json:"messages"`
	Temperature float64         `json:"temperature"`
	MaxTokens   int             `json:"max_tokens"`
}

type mistralMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type mistralResp struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (m *MistralClient) Chat(system, user string) (string, error) {
	req := mistralReq{
		Model: m.model,
		Messages: []mistralMsg{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: 0.7,
		MaxTokens:   800,
	}

	body, _ := json.Marshal(req)

	httpReq, err := http.NewRequest("POST", "https://api.mistral.ai/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Authorization", "Bearer "+m.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("mistral HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result mistralResp
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("mistral parse error: %v (body: %s)", err, string(respBody))
	}

	if result.Error != nil {
		return "", fmt.Errorf("mistral API error: %s", result.Error.Message)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("mistral returned no choices")
	}

	return result.Choices[0].Message.Content, nil
}
