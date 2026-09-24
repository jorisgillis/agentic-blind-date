package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// liveStream is an SSE response that a test can read while the handler is still writing.
type liveStream struct {
	mu     sync.Mutex
	header http.Header
	body   bytes.Buffer
	status int
	cancel context.CancelFunc
	done   chan struct{}
}

func (s *liveStream) Header() http.Header { return s.header }
func (s *liveStream) WriteHeader(code int) {
	s.mu.Lock()
	s.status = code
	s.mu.Unlock()
}
func (s *liveStream) Write(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.body.Write(b)
}
func (s *liveStream) Flush() {}

func (s *liveStream) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.body.String()
}

// open starts an SSE handler in the background; close() stops it and waits.
func open(t *testing.T, srv *testSrv, path string) *liveStream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	s := &liveStream{header: http.Header{}, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(s.done)
		srv.h.ServeHTTP(s, httptest.NewRequest("GET", path, nil).WithContext(ctx))
	}()
	t.Cleanup(s.close)
	return s
}

func (s *liveStream) close() {
	s.cancel()
	<-s.done
}

// ended reports whether the handler finished on its own (after a redirect).
func (s *liveStream) ended() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

func waitFor(t *testing.T, s *liveStream, want string) {
	t.Helper()
	eventually(t, "stream to contain "+want, func() bool { return strings.Contains(s.String(), want) })
}

// quiet asserts the stream writes nothing more for a short while.
func quiet(t *testing.T, s *liveStream, what string) {
	t.Helper()
	before := s.String()
	time.Sleep(50 * time.Millisecond)
	if after := s.String(); after != before {
		t.Errorf("%s: unexpected events %q", what, strings.TrimPrefix(after, before))
	}
}

func TestPipelineStream_RefreshesWhenTheInterviewStartsAndRedirectsWhenDone(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	deps.db.CreateParticipant("p", "p", "P", true)
	deps.db.SetQuestions("p", []Question{{ID: "q1", Text: "Tabs?"}})

	s := open(t, srv, "/user/pipeline-stream/p")
	quiet(t, s, "while preparing")

	forceStep(deps.db, "p", StepInterviewing)
	waitFor(t, s, "event: refresh")

	before := s.String()
	deps.db.LogActivity("someone else did something")
	quiet(t, s, "a change that does not concern this Participant")
	if s.String() != before {
		t.Fatal("unrelated changes must not refresh the question form")
	}

	deps.db.UpdateAnswers("p", map[string]string{"q1": "yes"})
	waitFor(t, s, "event: redirect\ndata: /user/wait/p")
	eventually(t, "stream ends after redirecting", s.ended)
}

func TestPipelineStream_RedirectsImmediatelyWhenAlreadyDone(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "r", "R", "ready")

	s := open(t, srv, "/user/pipeline-stream/r")

	waitFor(t, s, "data: /user/wait/r")
}

func TestWaitStream_RefreshesOnChangesAndRedirectsOnTheReveal(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")
	seed(t, deps.db, "b", "B", "ready")

	s := open(t, srv, "/user/wait-stream/a")
	quiet(t, s, "nothing changed")

	pair(t, deps.db, "a", "b")
	waitFor(t, s, "event: refresh")

	post(t, srv, "/admin/reveal", nil)
	waitFor(t, s, "event: redirect\ndata: /user/match/a")
}

func TestScreenStream_PushesTheGraphNowAndOnEveryChange(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")

	s := open(t, srv, "/bigscreen/stream")
	waitFor(t, s, "The Gopher")

	seed(t, deps.db, "b", "The Crab", "ready")
	waitFor(t, s, "The Crab")
}

func TestStreams_UnknownParticipantIs404(t *testing.T) {
	srv, _ := newTestServer(t, nil, nil)
	for _, path := range []string{"/user/pipeline-stream/nobody", "/user/wait-stream/nobody"} {
		s := open(t, srv, path)
		eventually(t, path+" to end", s.ended)
		if s.status != 404 {
			t.Errorf("%s: want 404, got %d", path, s.status)
		}
	}
}

// waitForAny waits for the first event and returns what was written so far.
func (s *liveStream) waitForAny(t *testing.T) *http.Response {
	t.Helper()
	eventually(t, "a first event", func() bool { return s.String() != "" })
	rec := httptest.NewRecorder()
	rec.WriteString(s.String())
	return rec.Result()
}
