package main

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
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

func TestPipelineStatus_DoesNotSkipTheInterviewWhileQuestionsArePrepared(t *testing.T) {
	gh := newFakeGitHub().withProfile(&GitHubProfile{Login: "octo"})
	llm := newFakeLLM().on("interviewer", `{"questions": ["Why Go?", "Favourite repo?", "Tabs?"]}`)
	llm.delay = 300 * time.Millisecond
	srv, deps := newTestServer(t, llm, gh)

	post(t, srv, "/user/join", url.Values{"name": {"Octo"}, "github": {"octo"}})
	p, err := deps.db.GetParticipantByHandle("octo")
	if err != nil {
		t.Fatal(err)
	}

	// Poll like the onboarding page does while the question set is still being generated.
	for i := 0; i < 10; i++ {
		resp := get(t, srv, "/user/pipeline/"+p.ID)
		if loc := resp.Header.Get("HX-Redirect"); loc != "" {
			t.Fatalf("poll %d during question preparation redirected to %s", i, loc)
		}
		if step := reload(t, deps.db, p.ID).PipelineStep; step == "ready" {
			t.Fatalf("poll %d moved the Participant to ready before the Interview", i)
		}
		time.Sleep(20 * time.Millisecond)
	}

	eventually(t, "first question shown", func() bool {
		body := readBody(t, get(t, srv, "/user/pipeline/"+p.ID))
		return strings.Contains(body, "Tabs or spaces?")
	})
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCompletedInterview_LeadsToPersonaAndReady(t *testing.T) {
	gh := newFakeGitHub().withProfile(&GitHubProfile{Login: "octo", Languages: []string{"Go"}})
	llm := newFakeLLM().
		on("interviewer", `{"questions": ["Why Go?", "Favourite repo?", "Tabs?"]}`).
		on("personality generator", `{"name": "The Gopher Whisperer", "tagline": "Talks to goroutines"}`)
	srv, deps := newTestServer(t, llm, gh)

	post(t, srv, "/user/join", url.Values{"name": {"Octo"}, "github": {"octo"}})
	var p *Participant
	eventually(t, "interview started", func() bool {
		p, _ = deps.db.GetParticipantByHandle("octo")
		return p != nil && p.PipelineStep == "interviewing"
	})

	answerAll(t, srv, p)

	eventually(t, "participant ready with persona", func() bool {
		p = reload(t, deps.db, p.ID)
		return p.PipelineStep == "ready"
	})
	if p.PersonaName != "The Gopher Whisperer" {
		t.Errorf("PersonaName: got %q", p.PersonaName)
	}
}

// answerAll answers every question in p's Interview through the answer endpoint.
func answerAll(t *testing.T, srv *testSrv, p *Participant) {
	t.Helper()
	for _, q := range p.Questions {
		answer := "whatever"
		if len(q.Options) > 0 {
			answer = q.Options[0]
		}
		if q.MaxSelections > 1 {
			answer = `["` + q.Options[0] + `"]`
		}
		if resp := post(t, srv, "/user/answer/"+p.ID, url.Values{"answer": {answer}}); resp.StatusCode != 200 {
			t.Fatalf("answering %s: status %d", q.ID, resp.StatusCode)
		}
	}
}

func TestNonGitHubInterview_ProducesExtraAnswersThatDriveInterestsAndPersona(t *testing.T) {
	llm := newFakeLLM().on("personality generator", `{"name": "The Rusty Gopher", "tagline": "Borrow-checks goroutines"}`)
	srv, deps := newTestServer(t, llm, nil)

	resp := post(t, srv, "/user/join", url.Values{"name": {"Ada"}, "no_github": {"on"}})
	id := strings.TrimPrefix(resp.Header.Get("Location"), "/user/onboard/")
	var p *Participant
	eventually(t, "interview started", func() bool {
		p = reload(t, deps.db, id)
		return p.PipelineStep == "interviewing"
	})

	answers := map[string]string{
		"extra_0": `["Go","Rust"]`,
		"extra_1": "Backend Services",
		"extra_2": "VIM",
		"extra_3": "A race condition on Tuesdays",
		"extra_4": "Mechanical",
	}
	for _, q := range p.Questions {
		a, ok := answers[q.ID]
		if !ok {
			a = "whatever"
		}
		post(t, srv, "/user/answer/"+id, url.Values{"answer": {a}})
	}
	eventually(t, "participant ready", func() bool {
		p = reload(t, deps.db, id)
		return p.PipelineStep == "ready"
	})

	ea := p.Profile.ExtraAnswers
	if ea == nil {
		t.Fatal("ExtraAnswers not populated")
	}
	if strings.Join(ea.Languages, ",") != "Go,Rust" || ea.ProjectType != "Backend Services" ||
		strings.Join(ea.DevEnvironment, ",") != "VIM" || ea.WeirdestBug != "A race condition on Tuesdays" || ea.Keyboard != "Mechanical" {
		t.Errorf("ExtraAnswers: got %+v", ea)
	}

	langs, _ := p.Interests["languages"].([]any)
	domains, _ := p.Interests["domains"].([]any)
	if len(langs) != 2 || langs[0] != "Go" || len(domains) != 1 || domains[0] != "Backend Services" {
		t.Errorf("Interests should reflect the Non-GitHub answers, got %v", p.Interests)
	}

	call, _ := llm.lastCallMatching("personality generator")
	if !strings.Contains(call.User, "Languages: Go, Rust") {
		t.Errorf("persona prompt should include the ExtraAnswers, got:\n%s", call.User)
	}
	if strings.Contains(call.User, "no-github-") {
		t.Errorf("persona prompt leaks the generated handle:\n%s", call.User)
	}
}

func TestExplore_RepeatViewsOfAPairAreServedFromTheCache(t *testing.T) {
	llm := newFakeLLM().on("matchmaker", `{"score": 77, "reason": "Both love Go", "red_flags": [], "green_flags": ["Go"], "icebreakers": ["Why?"]}`)
	srv, deps := newTestServer(t, llm, nil)
	for _, id := range []string{"me", "other"} {
		deps.db.CreateParticipant(id, id, id)
	}

	for i := 0; i < 2; i++ {
		resp := get(t, srv, "/user/explore/me/other")
		if resp.StatusCode != 200 {
			t.Fatalf("view %d: status %d", i, resp.StatusCode)
		}
		if body := readBody(t, resp); !strings.Contains(body, "Both love Go") {
			t.Fatalf("view %d: assessment not rendered", i)
		}
	}

	if n := llm.callsMatching("matchmaker"); n != 1 {
		t.Errorf("LLM calls for two views of the same Pair: want 1, got %d", n)
	}
}
