package main

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// These tests cover the render side of the interview question fragment (#143,
// the gap behind #141): the multi-select shape was never rendered before —
// the suite asserted on question data and answer validation, never on the
// rendered form — so a template error in any shape shipped unnoticed.

// escapeText renders s the way the page templates render interpolated values,
// so assertions compare against the escaped markup — html/template escapes
// more than html.HTMLEscapeString ("C++" becomes "C&#43;&#43;").
var escapeText = template.Must(template.New("escape").Parse(`{{.}}`))

func htmlRender(s string) string {
	var b strings.Builder
	if err := escapeText.Execute(&b, s); err != nil {
		panic(err)
	}
	return b.String()
}

// joinAndPrepare registers a Participant through the join endpoint and waits
// until their Interview has started, so the walks below render the question
// set the Interview module really writes.
func joinAndPrepare(t *testing.T, srv *testSrv, deps *testDeps, form url.Values) string {
	t.Helper()
	resp := post(t, srv, "/user/join", form)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /user/join: want 303, got %d", resp.StatusCode)
	}
	id := strings.TrimPrefix(resp.Header.Get("Location"), "/user/onboard/")
	eventually(t, "interview started", func() bool {
		return reload(t, deps.db, id).PipelineStep == StepInterviewing
	})
	return id
}

// wantQuestionSet pins the ids of the question set the walk renders, so the
// matrix stays tied to the sets the Interview module writes (ADR-0003).
func wantQuestionSet(t *testing.T, p *Participant, ids ...string) {
	t.Helper()
	if len(p.Questions) != len(ids) {
		t.Fatalf("question set: want %v, got %d questions", ids, len(p.Questions))
	}
	for i, id := range ids {
		if p.Questions[i].ID != id {
			t.Fatalf("question set: want %v, got %v", ids, questionIDs(p.Questions))
		}
	}
}

// walkQuestionSet walks a Participant's question set end to end and renders
// every question in both render contexts: the onboard page includes the
// fragment (GET /user/onboard/{id}), and the answer endpoint renders the
// fragment directly (POST /user/answer/{id}). The first question is reachable
// only through the page — no answer precedes it — and the last POST completes
// the Interview and redirects instead of rendering.
func walkQuestionSet(t *testing.T, srv *testSrv, deps *testDeps, id string) {
	t.Helper()
	p := reload(t, deps.db, id)
	for i, q := range p.Questions {
		assertRenderedQuestion(t, "the onboard page", get(t, srv, "/user/onboard/"+id), i, len(p.Questions), q)

		resp := post(t, srv, "/user/answer/"+id, url.Values{"answer": {answerFor(q)}})
		if i == len(p.Questions)-1 {
			if resp.StatusCode != 200 || resp.Header.Get("HX-Redirect") == "" {
				t.Errorf("answering the last question %s: want 200 with HX-Redirect, got %d %q", q.ID, resp.StatusCode, resp.Header.Get("HX-Redirect"))
			}
			continue
		}
		assertRenderedQuestion(t, "the answer response", resp, i+1, len(p.Questions), p.Questions[i+1])
	}
}

// answerFor returns a valid answer to q: the first option, its JSON-encoded
// single-element list for multi-select, or free text.
func answerFor(q Question) string {
	if q.MaxSelections > 1 {
		return `["` + q.Options[0] + `"]`
	}
	if len(q.Options) > 0 {
		return q.Options[0]
	}
	return "an honest answer"
}

// assertRenderedQuestion checks one rendered interview question: status 200,
// no render error text, the progress line, the question text, and the markup
// the question's shape requires — every option with its label for
// single-select, the n/max counter and the Continue button for multi-select,
// the free-text input for nil options.
func assertRenderedQuestion(t *testing.T, context string, resp *http.Response, index, total int, q Question) {
	t.Helper()
	if resp.StatusCode != 200 {
		t.Errorf("%s, %s: want 200, got %d", context, q.ID, resp.StatusCode)
	}
	body := readBody(t, resp)
	if strings.Contains(body, "render error") {
		t.Errorf("%s, %s: the rendered question carries a render error", context, q.ID)
	}
	for _, want := range []string{
		fmt.Sprintf("Question %d of %d", index+1, total),
		htmlRender(q.Text),
	} {
		if !strings.Contains(body, want) {
			t.Errorf("%s, %s: want %q in the rendered question", context, q.ID, want)
		}
	}

	// The fragment's inline script is literal template text, present in every
	// render, so the assertions use the markup only the shapes' branches emit.
	var wants []string
	switch {
	case q.MaxSelections > 1: // multi-select with options — the #141 regression
		wants = []string{
			`id="multiselect-form"`,
			`onchange="updateMultiSelect(this,`,
			fmt.Sprintf("0/%d selected", q.MaxSelections),
			`id="submit-multiselect"`,
			"Continue →",
		}
		for _, opt := range q.Options {
			wants = append(wants,
				`value="`+htmlRender(opt)+`"`,
				`<span class="text-sm">`+htmlRender(opt)+`</span>`,
			)
		}
	case len(q.Options) > 0: // single-select with options
		for _, opt := range q.Options {
			wants = append(wants,
				`value="`+htmlRender(opt)+`"`,
				`><span class="hover:text-[var(--color-accent-text)]">`+htmlRender(opt)+`</span></button>`,
			)
		}
	default: // free text — nil options
		wants = []string{
			`type="text"`,
			`name="answer"`,
			"Type your answer...",
			"Skip this question",
		}
	}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("%s, %s: want %q in the rendered question", context, q.ID, want)
		}
	}

	// The shapes are exclusive: an option question never renders the
	// free-text input, and a single-select never renders the multi-select form.
	if q.MaxSelections <= 1 {
		unwanted := []string{`id="multiselect-form"`, `onchange="updateMultiSelect(this,`}
		if len(q.Options) > 0 {
			unwanted = append(unwanted, `type="text"`)
		}
		for _, u := range unwanted {
			if strings.Contains(body, u) {
				t.Errorf("%s, %s: the rendered question must not contain %q", context, q.ID, u)
			}
		}
	}
}

// TestInterviewQuestion_RenderMatrix renders the question fragment for every
// question shape — single-select, multi-select (#141's regression) and free
// text — in both render contexts, across both question sets: the non-GitHub
// set (Extra Questions first) and the GitHub set (Fixed + LLM custom
// questions, and the Extra fallback when the LLM fails). Between the sets,
// every shape reaches both contexts: the non-GitHub set has the multi-select
// first (covered by the page), the GitHub fallback set has it after the Fixed
// Questions (covered by the page and by the answer response).
func TestInterviewQuestion_RenderMatrix(t *testing.T) {
	t.Run("the non-GitHub set, Extra Questions first", func(t *testing.T) {
		srv, deps := newTestServer(t, nil, nil)
		id := joinAndPrepare(t, srv, deps, url.Values{"name": {"Ada"}, "no_github": {"on"}})
		p := reload(t, deps.db, id)
		wantQuestionSet(t, p, "extra_0", "extra_1", "extra_2", "extra_3", "extra_4",
			"fixed_0", "fixed_2", "fixed_3", "fixed_4", "fixed_5")
		walkQuestionSet(t, srv, deps, id)
	})

	t.Run("the GitHub set, Fixed and LLM custom questions", func(t *testing.T) {
		gh := newFakeGitHub().withProfile(&GitHubProfile{Login: "octo", Languages: []string{"Go"}})
		llm := newFakeLLM().on("interviewer", `{"questions": ["Why do you love Go?", "Favourite repository?", "Tabs or spaces at 2am?"]}`)
		srv, deps := newTestServer(t, llm, gh)
		id := joinAndPrepare(t, srv, deps, url.Values{"name": {"Octo"}, "github": {"octo"}})
		p := reload(t, deps.db, id)
		wantQuestionSet(t, p, "fixed_0", "fixed_1", "fixed_2", "fixed_3", "fixed_4", "fixed_5",
			"custom_0", "custom_1", "custom_2")
		walkQuestionSet(t, srv, deps, id)
	})

	t.Run("the GitHub set, the Extra fallback when the LLM fails", func(t *testing.T) {
		gh := newFakeGitHub().withProfile(&GitHubProfile{Login: "octo", Languages: []string{"Go"}})
		llm := newFakeLLM().onErr("interviewer", fakeError("LLM down"))
		srv, deps := newTestServer(t, llm, gh)
		id := joinAndPrepare(t, srv, deps, url.Values{"name": {"Octo"}, "github": {"octo"}})
		p := reload(t, deps.db, id)
		wantQuestionSet(t, p, "fixed_0", "fixed_1", "fixed_2", "fixed_3", "fixed_4", "fixed_5",
			"extra_0", "extra_1", "extra_2", "extra_3", "extra_4")
		walkQuestionSet(t, srv, deps, id)
	})
}

// TestPages_RenderCompleteDocuments is the standing invariant behind the
// masking half of #141: a rendered page is a complete document and a 200
// response never carries a render error. The old render helper streamed a
// partial page with status 200 when a template failed, and nothing asserted
// against that shape.
func TestPages_RenderCompleteDocuments(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "i", "The Interviewee", "interviewing") // the onboard page with the question fragment
	seed(t, deps.db, "w", "The Waiter", "ready")             // the wait page
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	pair(t, deps.db, "a", "b")
	seed(t, deps.db, "m", "The Meetable", "ready") // the meet page's target
	must(t, deps.events.Reveal())                  // the match page opens from the social on

	pages := []struct{ name, path string }{
		{"onboard", "/user/onboard/i"},
		{"wait", "/user/wait/w"},
		{"match", "/user/match/a"},
		{"meet", "/user/m/m"},
		{"admin", "/admin"},
		{"bigscreen", "/bigscreen"},
	}
	for _, page := range pages {
		t.Run(page.name, func(t *testing.T) {
			resp := get(t, srv, page.path)
			if resp.StatusCode != 200 {
				t.Fatalf("GET %s: want 200, got %d", page.path, resp.StatusCode)
			}
			body := readBody(t, resp)
			if strings.Contains(body, "render error") {
				t.Errorf("GET %s: the page carries a render error", page.path)
			}
			if !strings.HasSuffix(strings.TrimRight(body, "\n\t "), "</html>") {
				t.Errorf("GET %s: the page is not a complete document, it ends with %q",
					page.path, body[max(0, len(body)-40):])
			}
		})
	}
}
