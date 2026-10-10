package main

import (
	"errors"
	"strings"
	"testing"
)

type structuredReply struct {
	Name string `json:"name"`
}

func nonEmptyName(r structuredReply) error {
	if r.Name == "" {
		return errors.New("no name")
	}
	return nil
}

func TestAskStructured_DecodesAValidReply(t *testing.T) {
	llm := newFakeLLM().on("s", `{"name": "Ada"}`)

	got, err := AskStructured(llm, "s", "u", nonEmptyName)

	if err != nil || got.Name != "Ada" {
		t.Fatalf("want Ada, got %+v (err %v)", got, err)
	}
}

// TestAskStructured_MalformedRepliesAreOneUniformErrorWithTheRawReply covers
// #56: prose, markdown fences and a reply missing a required field are all
// tested once here, not once per caller, and every error carries the raw
// reply.
func TestAskStructured_MalformedRepliesAreOneUniformErrorWithTheRawReply(t *testing.T) {
	for name, reply := range map[string]string{
		"prose":           `Sure, here you go: {"name": "Ada"} — hope that helps!`,
		"markdown fences": "```json\n{\"name\": \"Ada\"}\n```",
		"missing field":   `{"tagline": "no name here"}`,
		"not json at all": `I refuse to produce JSON today.`,
	} {
		t.Run(name, func(t *testing.T) {
			llm := newFakeLLM().on("s", reply)

			got, err := AskStructured(llm, "s", "u", nonEmptyName)

			switch name {
			case "prose", "markdown fences":
				if err != nil || got.Name != "Ada" {
					t.Errorf("should tolerate wrapping, got %+v (err %v)", got, err)
				}
			default:
				if err == nil {
					t.Fatal("want an error")
				}
				if !strings.Contains(err.Error(), reply) {
					t.Errorf("error should include the raw reply %q, got %v", reply, err)
				}
			}
		})
	}
}

func TestAskStructured_AChatFailureIsReturnedAsIs(t *testing.T) {
	llm := newFakeLLM().onErr("s", fakeError("network down"))

	_, err := AskStructured(llm, "s", "u", nonEmptyName)

	if err == nil || err.Error() != "network down" {
		t.Errorf("want the chat error unwrapped, got %v", err)
	}
}
