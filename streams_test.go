package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// liveStream is an SSE response that a test can read while the handler is still writing.
type liveStream struct {
	srv    *testSrv
	path   string
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
	s := &liveStream{srv: srv, path: path, header: http.Header{}, cancel: cancel, done: make(chan struct{})}
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

// sentNothingNew waits until the stream has handled its checks-th change (its
// first look counts as one) and asserts it sent nothing beyond before.
func sentNothingNew(t *testing.T, s *liveStream, checks int, before, what string) {
	t.Helper()
	eventually(t, "the stream to catch up", func() bool { return s.srv.streamChecks(s.path) >= checks })
	if after := s.String(); after != before {
		t.Errorf("%s: unexpected events %q", what, strings.TrimPrefix(after, before))
	}
}

func TestPipelineStream_RefreshesWhenTheInterviewStartsAndRedirectsWhenDone(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	must(t, deps.db.CreateParticipant("p", "p", "P", true))
	forceQuestions(deps.db, "p", []Question{{ID: "q1", Text: "Tabs?"}})

	s := open(t, srv, "/user/pipeline-stream/p")
	waitFor(t, s, "event: refresh") // the connect-time sync
	synced := s.String()
	sentNothingNew(t, s, 1, synced, "while preparing")

	forceStep(deps.db, "p", StepInterviewing)
	eventually(t, "a second refresh when the Interview starts", func() bool {
		return strings.Count(s.String(), "event: refresh") >= 2
	})

	before := s.String()
	deps.db.LogActivity("someone else did something")
	sentNothingNew(t, s, 3, before, "a change that does not concern this Participant")

	updateAnswers(t, deps.db, "p", map[string]string{"q1": "yes"})
	waitFor(t, s, "event: redirect\ndata: /user/wait/p")
	eventually(t, "stream ends after redirecting", s.ended)
}

func TestPipelineStream_RedirectsImmediatelyWhenAlreadyDone(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "r", "R", "ready")

	s := open(t, srv, "/user/pipeline-stream/r")

	waitFor(t, s, "data: /user/wait/r")
}

func TestPipelineStream_SyncsOnConnectWhenTheStepAlreadySettled(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	must(t, deps.db.CreateParticipant("p", "p", "P", true))
	forceQuestions(deps.db, "p", []Question{{ID: "q1", Text: "Tabs?"}})
	forceStep(deps.db, "p", StepInterviewing)

	s := open(t, srv, "/user/pipeline-stream/p")

	// Nothing ever changes: the sync comes from connecting, so a page that
	// rendered before the step settled (a reload, a restart) still catches up.
	waitFor(t, s, "event: refresh")
}

// TestPipelineStream_SkipsTheOpeningSyncWhenThePageIsCurrent pins the fix for
// the e2e flake (#188): the Onboard page passes the step it rendered with, so
// a page that already shows the Interview gets no opening swap — that swap
// replaced the pipeline area over an in-flight answer and reset the
// multi-select form mid-interaction.
func TestPipelineStream_SkipsTheOpeningSyncWhenThePageIsCurrent(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	must(t, deps.db.CreateParticipant("p", "p", "P", true))
	forceQuestions(deps.db, "p", []Question{{ID: "q1", Text: "Tabs?"}})
	forceStep(deps.db, "p", StepInterviewing)

	s := open(t, srv, "/user/pipeline-stream/p?from=interviewing")

	// The stream runs its checks without ever emitting: the page is current.
	// (The handler records check passes under the bare path, without query.)
	base := "/user/pipeline-stream/p"
	eventually(t, "the stream to catch up", func() bool { return s.srv.streamChecks(base) >= 1 })
	if after := s.String(); after != "" {
		t.Errorf("unexpected opening sync for a current page: %q", after)
	}
}

// TestPipelineStream_SyncsWhenThePageRenderedBeforeTheStepSettled pins the
// catch-up half of the same contract: a page rendered at an older step still
// gets its opening refresh, so the stale pipeline area catches up.
func TestPipelineStream_SyncsWhenThePageRenderedBeforeTheStepSettled(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	must(t, deps.db.CreateParticipant("p", "p", "P", true))
	forceQuestions(deps.db, "p", []Question{{ID: "q1", Text: "Tabs?"}})
	forceStep(deps.db, "p", StepInterviewing)

	s := open(t, srv, "/user/pipeline-stream/p?from=fetching_github")

	waitFor(t, s, "event: refresh")
}

// TestWaitStream_SyncsOnConnectWhenThePageRenderedEarly covers the race the
// E2E journeys caught (#144): the wait page renders the persona placeholder,
// the persona lands, and only then does the stream connect — its opening
// snapshot already carries the persona, so no change ever fires and the
// placeholder would stay forever. Like the pipeline stream, the wait stream
// syncs once on connect.
func TestWaitStream_SyncsOnConnectWhenThePageRenderedEarly(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	if err := deps.db.CreateParticipant("a", "a", "A", true); err != nil {
		t.Fatal(err)
	}
	forceStep(deps.db, "a", StepCreatingPersona)
	setPersona(t, deps.db, "a", "The Bard", "Ships things")

	s := open(t, srv, "/user/wait-stream/a")

	// Nothing ever changes: the refresh comes from connecting, so a page
	// that rendered before the persona landed still catches up.
	waitFor(t, s, "event: refresh")
}

// TestMatchStream_SyncsOnConnectWhenThePageRenderedEarly covers the same race
// on the match page: a meeting can complete between the page's render and
// the stream's connect, and the opening signature already carries it — the
// meet section would stay stale forever. The match stream syncs on connect
// too.
func TestMatchStream_SyncsOnConnectWhenThePageRenderedEarly(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")
	seed(t, deps.db, "b", "B", "ready")
	pair(t, deps.db, "a", "b")
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}

	s := open(t, srv, "/user/match-stream/a")

	// Nothing ever changes: the refresh comes from connecting.
	waitFor(t, s, "event: refresh")
}

func TestWaitStream_RefreshesOnChangesAndRedirectsOnTheReveal(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")
	seed(t, deps.db, "b", "B", "ready")

	s := open(t, srv, "/user/wait-stream/a")
	waitFor(t, s, "event: refresh") // the sync on connect
	before := s.String()
	sentNothingNew(t, s, 1, before, "nothing changed")

	pair(t, deps.db, "a", "b")
	eventually(t, "the pairing to refresh", func() bool {
		return strings.Count(s.String(), "event: refresh") >= 2
	})

	post(t, srv, "/admin/reveal", nil)
	waitFor(t, s, "event: redirect\ndata: /user/match/a")
}

// TestWaitStream_RefreshesWhenThePersonaIsCrafted covers #125: the persona is
// crafted in the background after the last interview answer, and the wait
// page's card appears by push when it lands.
func TestWaitStream_RefreshesWhenThePersonaIsCrafted(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	if err := deps.db.CreateParticipant("a", "a", "A", true); err != nil {
		t.Fatal(err)
	}
	forceStep(deps.db, "a", StepCreatingPersona)

	s := open(t, srv, "/user/wait-stream/a")
	waitFor(t, s, "event: refresh") // the sync on connect
	before := s.String()
	sentNothingNew(t, s, 1, before, "the persona is still being crafted")

	setPersona(t, deps.db, "a", "The Bard", "Ships things")
	eventually(t, "the persona to refresh the card", func() bool {
		return strings.Count(s.String(), "event: refresh") >= 2
	})
}

// TestMatchStream_EmitsAPhaseEventWhenTheHostAdvancesThePhase covers #136's
// push half on the match page's stream: a phase transition emits a distinct
// 'phase' event — in addition to the 'refresh' the signature change already
// causes — and unrelated changes emit nothing.
func TestMatchStream_EmitsAPhaseEventWhenTheHostAdvancesThePhase(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")
	seed(t, deps.db, "b", "B", "ready")
	pair(t, deps.db, "a", "b")

	s := open(t, srv, "/user/match-stream/a")
	waitFor(t, s, "event: refresh") // the sync on connect
	before := s.String()
	sentNothingNew(t, s, 1, before, "nothing changed")

	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, "event: phase")
	if strings.Count(s.String(), "event: refresh") < 2 {
		t.Errorf("a phase change still changes the signature, so 'refresh' should fire again, got %q", s.String())
	}

	before = s.String()
	deps.db.LogActivity("someone else did something")
	sentNothingNew(t, s, 3, before, "a change that does not concern this Participant")

	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	eventually(t, "a second phase event", func() bool {
		return strings.Count(s.String(), "event: phase") >= 2
	})
}

// TestWaitStream_EmitsAPhaseEventWhenTheHostAdvancesThePhase covers #136's
// push half on the wait page's stream. The Participant stays on the wait page
// — unmatched Participants do, in every phase — so the stream keeps tracking
// the phase across the evening; a matched Participant would be redirected to
// the match page on the reveal instead.
func TestWaitStream_EmitsAPhaseEventWhenTheHostAdvancesThePhase(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "w", "W", "ready")

	s := open(t, srv, "/user/wait-stream/w")
	waitFor(t, s, "event: refresh") // the sync on connect
	before := s.String()
	sentNothingNew(t, s, 1, before, "nothing changed")

	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, "event: phase")
	if strings.Count(s.String(), "event: refresh") < 2 {
		t.Errorf("a phase change still changes the signature, so 'refresh' should fire again, got %q", s.String())
	}

	before = s.String()
	deps.db.LogActivity("someone else did something")
	sentNothingNew(t, s, 3, before, "a change that does not concern this Participant")

	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	eventually(t, "a second phase event", func() bool {
		return strings.Count(s.String(), "event: phase") >= 2
	})

	// The unmatched Participant stays on the wait page once closed, so the
	// stream keeps tracking the phase.
	if err := deps.events.Advance(PhaseClosed); err != nil {
		t.Fatal(err)
	}
	eventually(t, "a third phase event", func() bool {
		return strings.Count(s.String(), "event: phase") >= 3
	})
}

// TestPersonalStreams_RefreshThePhaseHeaderOnQueueChanges covers #139's push
// half: the queue badge rides the header fragment on the personal pages'
// existing 'refresh' events, which already fire when the host reorders the
// queue or stages a sign-up — the show & tell signature both streams watch
// carries the Participant's sign-up position and status, and the on-stage
// sign-up. No new stream or event is involved.
func TestPersonalStreams_RefreshThePhaseHeaderOnQueueChanges(t *testing.T) {
	t.Run("wait stream", func(t *testing.T) {
		srv, deps := newTestServer(t, nil, nil)
		seed(t, deps.db, "w", "W", "ready")
		seed(t, deps.db, "other", "Other", "ready")
		if err := deps.events.Advance(PhaseShowTell); err != nil {
			t.Fatal(err)
		}
		signUp(t, deps, "other", "Other talk")
		signUp(t, deps, "w", "W talk")

		s := open(t, srv, "/user/wait-stream/w")
		waitFor(t, s, "event: refresh") // the sync on connect
		before := s.String()
		sentNothingNew(t, s, 1, before, "nothing changed")

		// The host reorders the queue: the Participant's place changes.
		if err := deps.showtell.Move(signupID(t, deps, "w"), "up"); err != nil {
			t.Fatal(err)
		}
		eventually(t, "the reorder to refresh the header", func() bool {
			return strings.Count(s.String(), "event: refresh") >= 2
		})

		// The host stages them: the badge flips to the on-stage state.
		before = s.String()
		stageSignup(t, deps, "w")
		eventually(t, "staging to refresh the header", func() bool {
			return len(s.String()) > len(before)
		})
	})

	t.Run("match stream", func(t *testing.T) {
		srv, deps := newTestServer(t, nil, nil)
		seed(t, deps.db, "a", "A", "ready")
		seed(t, deps.db, "b", "B", "ready")
		pair(t, deps.db, "a", "b")
		if err := deps.events.Advance(PhaseShowTell); err != nil {
			t.Fatal(err)
		}
		signUp(t, deps, "b", "B talk")
		signUp(t, deps, "a", "A talk")

		s := open(t, srv, "/user/match-stream/a")
		waitFor(t, s, "event: refresh") // the sync on connect
		before := s.String()
		sentNothingNew(t, s, 1, before, "nothing changed")

		// The host reorders the queue: the Participant's place changes.
		if err := deps.showtell.Move(signupID(t, deps, "a"), "up"); err != nil {
			t.Fatal(err)
		}
		eventually(t, "the reorder to refresh the header", func() bool {
			return strings.Count(s.String(), "event: refresh") >= 2
		})

		// The host stages them: the badge flips to the on-stage state.
		before = s.String()
		stageSignup(t, deps, "a")
		eventually(t, "staging to refresh the header", func() bool {
			return len(s.String()) > len(before)
		})
	})
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
