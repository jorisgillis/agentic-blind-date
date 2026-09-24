package main

import (
	"fmt"
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

func TestPersonaLooks_AreDistinctAndCycleAfterTheyRunOut(t *testing.T) {
	db := newTestDB(t)
	combos := len(personaPalette) * len(personaSymbols)

	for i := 0; i < combos+5; i++ {
		if err := db.CreateParticipant(fmt.Sprintf("p%d", i), fmt.Sprintf("h%d", i), "", true); err != nil {
			t.Fatal(err)
		}
	}

	all, _ := db.GetAllParticipants()
	uses := map[string]int{}
	for _, p := range all {
		uses[p.PersonaColor+p.PersonaSymbol]++
	}
	if len(uses) != combos {
		t.Errorf("every combination should be used before any repeats: %d of %d used", len(uses), combos)
	}
	for look, n := range uses {
		if n > 2 {
			t.Errorf("%s used %d times: after running out, looks should cycle, not pile onto one", look, n)
		}
	}
}

func TestPersonaPalette_IsTheOneSourceForEveryDisplay(t *testing.T) {
	srv, _ := newTestServer(t, nil, nil)

	body := readBody(t, get(t, srv, "/bigscreen"))

	for _, c := range personaPalette {
		if !strings.Contains(body, c.Hex) {
			t.Errorf("the Big Screen should colour %s from the persona palette (%s)", c.Class, c.Hex)
		}
	}
}
