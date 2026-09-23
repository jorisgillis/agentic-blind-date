package main

import (
	"net/url"
	"testing"
)

func TestJoin_GitHubUserSetupUsesInjectedGitHubAndLLM(t *testing.T) {
	gh := newFakeGitHub().withProfile(&GitHubProfile{Login: "octo", Name: "Octo Cat", Languages: []string{"Go"}})
	llm := newFakeLLM().on("interviewer", `{"questions": ["Why Go?", "Favourite repo?", "Tabs?"]}`)
	srv, deps := newTestServer(t, llm, gh)

	post(t, srv, "/user/join", url.Values{"name": {"Octo"}, "github": {"octo"}})

	var p *Participant
	eventually(t, "interview questions stored", func() bool {
		p, _ = deps.db.GetParticipantByHandle("octo")
		return p != nil && len(p.Questions) > len(FixedQuestions)
	})
	if p.Profile == nil || len(p.Profile.Languages) != 1 || p.Profile.Languages[0] != "Go" {
		t.Errorf("profile should come from the GitHub adapter, got %+v", p.Profile)
	}
	if got := p.Questions[len(FixedQuestions)].Text; got != "Why Go?" {
		t.Errorf("first Custom Question: want %q, got %q", "Why Go?", got)
	}
}
