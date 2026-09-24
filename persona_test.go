package main

import (
	"strings"
	"testing"
)

func octocat() *Participant {
	return &Participant{
		ID: "p1", GitHubHandle: "octocat", HasGitHub: true, Name: "Octo Cat",
		Profile: &GitHubProfile{Login: "octocat", Name: "Octo Cat", Languages: []string{"Go", "Rust"}},
		Answers: map[string]string{"fixed_0": "Tabs"},
	}
}

func TestPersonasCreate_AsksTheLLMWithTheProfileAndAnswers(t *testing.T) {
	llm := newFakeLLM().on("personality generator", `{"name": "The Tab Tyrant", "tagline": "Indents with conviction"}`)

	persona := NewPersonas(llm).Create(octocat())

	if persona.Name != "The Tab Tyrant" || persona.Tagline != "Indents with conviction" {
		t.Errorf("persona: got %+v", persona)
	}
	call, _ := llm.lastCallMatching("personality generator")
	if !strings.Contains(call.User, "Languages used: Go, Rust") || !strings.Contains(call.User, "Tabs") {
		t.Errorf("prompt should describe the profile and answers:\n%s", call.User)
	}
}

func TestPersonasCreate_FallbackIsAnonymous(t *testing.T) {
	for name, tc := range map[string]struct {
		llm  *fakeLLM
		p    func() *Participant
		want string
	}{
		"llm fails, GitHub languages": {newFakeLLM().onErr("personality generator", fakeError("401")), octocat, "The Go Developer"},
		"llm replies garbage":         {newFakeLLM().on("personality generator", "I am a teapot"), octocat, "The Go Developer"},
		"no languages at all": {newFakeLLM(), func() *Participant {
			p := octocat()
			p.Profile.Languages = nil
			return p
		}, "The Mysterious Coder"},
		"Non-GitHub User's languages": {newFakeLLM(), func() *Participant {
			return &Participant{ID: "p2", GitHubHandle: "no-github-1", Name: "Ada Lovelace",
				Profile: &GitHubProfile{ExtraAnswers: &ExtraAnswers{Languages: []string{"Python"}}}}
		}, "The Python Developer"},
		"go-to language answer": {newFakeLLM(), func() *Participant {
			p := octocat()
			p.Profile.Languages = nil
			p.Answers = map[string]string{"fixed_1": "zig"}
			return p
		}, "The Zig Developer"},
	} {
		t.Run(name, func(t *testing.T) {
			p := tc.p()

			persona := NewPersonas(tc.llm).Create(p)

			if persona.Name != tc.want {
				t.Errorf("fallback name: want %q, got %q", tc.want, persona.Name)
			}
			for _, secret := range []string{p.GitHubHandle, p.Name, "Octo", "octo"} {
				if strings.Contains(persona.Name+persona.Tagline, secret) {
					t.Errorf("fallback persona %+v reveals %q", persona, secret)
				}
			}
		})
	}
}
