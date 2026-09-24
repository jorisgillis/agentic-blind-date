package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// upstream is a fake HTTP upstream: requests are answered by path, in-process.
type upstream struct {
	mu       sync.Mutex
	handlers map[string]func(r *http.Request) (int, string)
	requests []*http.Request
	bodies   []string
}

func newUpstream() *upstream {
	return &upstream{handlers: map[string]func(*http.Request) (int, string){}}
}

func (u *upstream) on(path string, status int, body string) *upstream {
	return u.onFunc(path, func(*http.Request) (int, string) { return status, body })
}

func (u *upstream) onFunc(path string, h func(r *http.Request) (int, string)) *upstream {
	u.handlers[path] = h
	return u
}

func (u *upstream) client() *http.Client { return &http.Client{Transport: u} }

func (u *upstream) RoundTrip(req *http.Request) (*http.Response, error) {
	var body string
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		body = string(b)
	}
	u.mu.Lock()
	u.requests = append(u.requests, req)
	u.bodies = append(u.bodies, body)
	h, ok := u.handlers[req.URL.Path]
	u.mu.Unlock()

	rec := httptest.NewRecorder()
	if !ok {
		rec.WriteHeader(http.StatusNotFound)
	} else {
		status, respBody := h(req)
		rec.WriteHeader(status)
		io.WriteString(rec, respBody)
	}
	return rec.Result(), nil
}

func githubClientFor(u *upstream, token string) *GitHubClient {
	c := NewGitHubClient(token)
	c.httpClient = u.client()
	return c
}

const octoUser = `{"login": "octo", "name": "Octo Cat", "bio": "Tentacles", "company": " @github ",
	"location": "Sea", "created_at": "2014-01-01T00:00:00Z", "public_repos": 7, "followers": 42}`

const octoRepos = `[
	{"name": "octo", "description": "", "stargazers_count": 0, "language": "", "topics": []},
	{"name": "gopher", "description": "Go things", "stargazers_count": 10, "language": "Go", "topics": ["cli", "go"]},
	{"name": "crab", "description": "Rust things", "stargazers_count": 5, "language": "Rust", "topics": ["cli"]},
	{"name": "forked", "description": "not mine", "stargazers_count": 900, "language": "C", "fork": true, "topics": ["c"]},
	{"name": "more-go", "description": "", "stargazers_count": 1, "language": "Go", "topics": []}
]`

func TestGitHubClientFetchProfile_ParsesUserAndOwnRepos(t *testing.T) {
	u := newUpstream().on("/users/octo", 200, octoUser).on("/users/octo/repos", 200, octoRepos)

	p, err := githubClientFor(u, "secret").FetchProfile("octo")
	if err != nil {
		t.Fatal(err)
	}

	if p.Login != "octo" || p.Name != "Octo Cat" || p.Company != "github" || p.Followers != 42 || p.PublicRepos != 7 {
		t.Errorf("user fields: %+v", p)
	}
	if p.AccountAgeDays < 365*10 {
		t.Errorf("account age: got %d days", p.AccountAgeDays)
	}
	if p.TotalStars != 16 {
		t.Errorf("total stars should skip forks: want 16, got %d", p.TotalStars)
	}
	if strings.Join(p.Languages, ",") != "Go,Rust" {
		t.Errorf("languages: want Go,Rust, got %v", p.Languages)
	}
	if len(p.TopTopics) == 0 || p.TopTopics[0] != "cli" {
		t.Errorf("most common topic first: got %v", p.TopTopics)
	}
	if !p.HasProfileReadme {
		t.Error("a repo named after the handle means a profile README")
	}
	if len(p.TopRepos) != 3 || p.TopRepos[0].Name != "gopher" {
		t.Errorf("top repos: got %+v", p.TopRepos)
	}
	if got := u.requests[0].Header.Get("Authorization"); got != "Bearer secret" {
		t.Errorf("Authorization header: got %q", got)
	}
}

func TestGitHubClientFetchProfile_UnknownUserIsAnError(t *testing.T) {
	u := newUpstream()

	if _, err := githubClientFor(u, "").FetchProfile("nobody"); err == nil {
		t.Error("want error for an unknown user")
	}
	if got := u.requests[0].Header.Get("Authorization"); got != "" {
		t.Errorf("no token means no Authorization header, got %q", got)
	}
}

func TestGitHubClientFetchProfile_ReposAreBestEffort(t *testing.T) {
	u := newUpstream().on("/users/octo", 200, octoUser).on("/users/octo/repos", 500, "")

	p, err := githubClientFor(u, "").FetchProfile("octo")

	if err != nil || p.Login != "octo" || len(p.Languages) != 0 {
		t.Errorf("want the user without repo data, got %+v (err %v)", p, err)
	}
}

func TestGitHubClientFetchProfile_RateLimitIsAnError(t *testing.T) {
	u := newUpstream().on("/users/octo", 403, "")

	if _, err := githubClientFor(u, "").FetchProfile("octo"); err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("want a 403 error, got %v", err)
	}
}

func TestGitHubClientCheckMutualFollow(t *testing.T) {
	u := newUpstream().on("/users/octo/following/ferris", 204, "")

	aFollowsB, bFollowsA := githubClientFor(u, "").CheckMutualFollow("octo", "ferris")

	if !aFollowsB || bFollowsA {
		t.Errorf("want octo→ferris only, got %v %v", aFollowsB, bFollowsA)
	}
}

func mistralFor(u *upstream) *MistralClient {
	c := NewMistralClient("key", "mistral-test", u.client())
	c.retryDelay = func(int) time.Duration { return 0 }
	return c
}

const chatPath = "/v1/chat/completions"

func TestMistralClientChat_SendsBothPromptsAndReturnsTheReply(t *testing.T) {
	u := newUpstream().on(chatPath, 200, `{"choices": [{"message": {"content": "hello"}}]}`)

	got, err := mistralFor(u).Chat("be nice", "hi")

	if err != nil || got != "hello" {
		t.Fatalf("want hello, got %q (err %v)", got, err)
	}
	var req mistralReq
	if err := json.Unmarshal([]byte(u.bodies[0]), &req); err != nil {
		t.Fatal(err)
	}
	if req.Model != "mistral-test" || len(req.Messages) != 2 || req.Messages[0].Content != "be nice" || req.Messages[1].Content != "hi" {
		t.Errorf("request body: %+v", req)
	}
	if got := u.requests[0].Header.Get("Authorization"); got != "Bearer key" {
		t.Errorf("Authorization header: got %q", got)
	}
}

func TestMistralClientChat_RetriesTransientFailures(t *testing.T) {
	attempts := 0
	u := newUpstream().onFunc(chatPath, func(*http.Request) (int, string) {
		attempts++
		if attempts < 3 {
			return 429, `{"message": "slow down"}`
		}
		return 200, `{"choices": [{"message": {"content": "finally"}}]}`
	})

	got, err := mistralFor(u).Chat("s", "u")

	if err != nil || got != "finally" || attempts != 3 {
		t.Errorf("want success on attempt 3, got %q after %d attempts (err %v)", got, attempts, err)
	}
}

func TestMistralClientChat_GivesUpAfterThreeAttempts(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
		want   string
	}{
		"http error": {500, "boom", "mistral HTTP 500"},
		"api error":  {200, `{"error": {"message": "bad key"}}`, "bad key"},
		"no choices": {200, `{"choices": []}`, "no choices"},
		"not json":   {200, `<html>`, "parse error"},
	} {
		t.Run(name, func(t *testing.T) {
			u := newUpstream().on(chatPath, tc.status, tc.body)

			_, err := mistralFor(u).Chat("s", "u")

			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want error containing %q, got %v", tc.want, err)
			}
			if len(u.requests) != 3 {
				t.Errorf("attempts: want 3, got %d", len(u.requests))
			}
		})
	}
}

func TestMistralClientChat_WithoutAnHTTPClient(t *testing.T) {
	c := NewMistralClient("key", "m", nil)
	c.retryDelay = func(int) time.Duration { return 0 }

	if _, err := c.Chat("s", "u"); err == nil {
		t.Error("want error without an HTTP client")
	}
}

// brokenTransport fails every request, like a network outage.
type brokenTransport struct{}

func (brokenTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fakeError("network is unreachable")
}

// failingBody is a response body that breaks off while being read.
type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, fakeError("connection reset") }
func (failingBody) Close() error             { return nil }

type truncatedTransport struct{}

func (truncatedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: failingBody{}, Header: http.Header{}}, nil
}

func TestGitHubClient_NetworkFailures(t *testing.T) {
	c := NewGitHubClient("secret")
	c.httpClient = &http.Client{Transport: brokenTransport{}}

	if _, err := c.FetchProfile("octo"); err == nil {
		t.Error("fetching a profile over a broken network should fail")
	}
	if a, b := c.CheckMutualFollow("octo", "ferris"); a || b {
		t.Error("follows are unknown, so false, over a broken network")
	}
}

// hangingTransport never responds, like a follow check stuck behind a dead connection.
type hangingTransport struct{}

func (hangingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	<-req.Context().Done()
	return nil, req.Context().Err()
}

func TestGitHubClient_ATimeoutIsTreatedLikeAnyOtherFailure(t *testing.T) {
	c := NewGitHubClient("secret")
	c.httpClient = &http.Client{Transport: hangingTransport{}, Timeout: 20 * time.Millisecond}

	start := time.Now()
	if _, err := c.FetchProfile("octo"); err == nil {
		t.Error("a hung fetch should fail")
	}
	if a, b := c.CheckMutualFollow("octo", "ferris"); a || b {
		t.Error("a hung follow check is treated as not following")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("calls should be bounded by the client timeout, took %v", elapsed)
	}
}

func TestGitHubClient_FollowChecksSendTheToken(t *testing.T) {
	u := newUpstream().on("/users/octo/following/ferris", 204, "")

	githubClientFor(u, "secret").CheckMutualFollow("octo", "ferris")

	if got := u.requests[0].Header.Get("Authorization"); got != "Bearer secret" {
		t.Errorf("Authorization header: got %q", got)
	}
}

func TestGitHubClientFetchProfile_UnreadableReposAreSkipped(t *testing.T) {
	u := newUpstream().on("/users/octo", 200, octoUser).on("/users/octo/repos", 200, "not json")

	p, err := githubClientFor(u, "").FetchProfile("octo")

	if err != nil || p.Login != "octo" || len(p.Languages) != 0 {
		t.Errorf("want the user without repo data, got %+v (err %v)", p, err)
	}
}

func TestGitHubClientFetchProfile_KeepsTheFiveMostCommonTopics(t *testing.T) {
	u := newUpstream().on("/users/octo", 200, octoUser).on("/users/octo/repos", 200,
		`[{"name": "all", "language": "Go", "topics": ["a", "b", "c", "d", "e", "f", "g"]}]`)

	p, _ := githubClientFor(u, "").FetchProfile("octo")

	if len(p.TopTopics) != 5 {
		t.Errorf("topics: want 5, got %v", p.TopTopics)
	}
}

func TestMistralClientChat_NetworkFailures(t *testing.T) {
	for name, transport := range map[string]http.RoundTripper{
		"unreachable":     brokenTransport{},
		"body breaks off": truncatedTransport{},
	} {
		t.Run(name, func(t *testing.T) {
			c := NewMistralClient("key", "m", &http.Client{Transport: transport})
			c.retryDelay = func(int) time.Duration { return 0 }
			if _, err := c.Chat("s", "u"); err == nil {
				t.Error("want an error")
			}
		})
	}
}

func TestMistralClient_BacksOffExponentially(t *testing.T) {
	c := NewMistralClient("key", "m", nil)

	if c.retryDelay(1) != 2*time.Second || c.retryDelay(2) != 4*time.Second {
		t.Errorf("want 2s then 4s, got %v then %v", c.retryDelay(1), c.retryDelay(2))
	}
}
