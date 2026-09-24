package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// AskStructured asks the LLM for a reply of shape T: it sends the prompts,
// cuts the JSON out of the reply (tolerating prose or markdown fences around
// it), decodes it, and runs validate for the invariants JSON decoding can't
// check on its own (a required field, a non-empty slice). A decode failure
// or a failed validation always includes the raw reply, so a caller's log
// line shows exactly what the LLM said.
func AskStructured[T any](llm LLM, system, user string, validate func(T) error) (T, error) {
	var zero T
	reply, err := llm.Chat(system, user)
	if err != nil {
		return zero, err
	}

	var result T
	if err := json.Unmarshal([]byte(extractJSON(reply)), &result); err != nil {
		return zero, fmt.Errorf("structured reply parse error: %v (raw: %s)", err, reply)
	}
	if validate != nil {
		if err := validate(result); err != nil {
			return zero, fmt.Errorf("%v (raw: %s)", err, reply)
		}
	}
	return result, nil
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
