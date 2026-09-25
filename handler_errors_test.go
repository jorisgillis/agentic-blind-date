package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestHandlers_ReportFailedSavesAsErrors(t *testing.T) {
	t.Run("joining", func(t *testing.T) {
		srv, deps := newTestServer(t, nil, nil)
		failWrites(t, deps.db)
		if resp := post(t, srv, "/user/join", url.Values{"name": {"Ada"}, "no_github": {"on"}}); resp.StatusCode != 500 {
			t.Errorf("want 500, got %d", resp.StatusCode)
		}
	})
	t.Run("answering", func(t *testing.T) {
		srv, deps := newTestServer(t, nil, nil)
		seed(t, deps.db, "p", "P", "interviewing")
		failWrites(t, deps.db, "answers")
		if resp := post(t, srv, "/user/answer/p", url.Values{"answer": {"Tabs"}}); resp.StatusCode != 500 {
			t.Errorf("want 500, got %d", resp.StatusCode)
		}
	})
	t.Run("deleting", func(t *testing.T) {
		srv, deps := newTestServer(t, nil, nil)
		seed(t, deps.db, "p", "P", "ready")
		failWrites(t, deps.db, "delete")
		if resp := del(t, srv, "/data/participant/p"); resp.StatusCode != 500 {
			t.Errorf("want 500, got %d", resp.StatusCode)
		}
	})
	t.Run("resetting", func(t *testing.T) {
		srv, deps := newTestServer(t, nil, nil)
		seed(t, deps.db, "p", "P", "ready")
		failWrites(t, deps.db, "reset_participants")
		if resp := post(t, srv, "/admin/reset", nil); resp.StatusCode != 500 {
			t.Errorf("want 500, got %d", resp.StatusCode)
		}
	})
}

func TestHandlers_ReportABrokenDatabaseAsErrors(t *testing.T) {
	for _, tc := range []struct {
		method, path string
	}{
		{"GET", "/data/participants"},
		{"GET", "/data/activity"},
		{"POST", "/admin/reveal"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			srv, deps := newTestServer(t, nil, nil)
			breakDB(deps.db)
			req, _ := http.NewRequest(tc.method, srv.URL+tc.path, nil)
			resp, err := srv.Client().Do(req)
			if err != nil || resp.StatusCode != 500 {
				t.Errorf("want 500, got %v (err %v)", resp.StatusCode, err)
			}
		})
	}
}

func del(t *testing.T, srv *testSrv, path string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("DELETE", srv.URL+path, nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestDelete_AnUnknownParticipantIsNotFound(t *testing.T) {
	srv, _ := newTestServer(t, nil, nil)

	if resp := del(t, srv, "/data/participant/nobody"); resp.StatusCode != 404 {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

func TestRematch_AFailureIsShownInTheActivityFeed(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")

	post(t, srv, "/admin/rematch", nil)

	eventually(t, "the failure in the activity feed", func() bool {
		msgs, _ := deps.db.GetRecentActivity(10)
		return strings.Contains(strings.Join(msgs, "\n"), "Rematch failed")
	})
}

func TestMatchPage_WithAPartnerWhoIsGoneStillRenders(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	deps.db.Reveal()
	seed(t, deps.db, "a", "A", "ready")
	deps.db.db.Exec(`UPDATE participants SET matched_with = 'gone' WHERE id = 'a'`)

	if resp := get(t, srv, "/user/match/a"); resp.StatusCode != 200 {
		t.Errorf("want the page without a partner, got %d", resp.StatusCode)
	}
}

func TestWaitPage_WithoutAProfileStillRenders(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")
	deps.db.db.Exec(`UPDATE participants SET profile_json = 'null' WHERE id = 'a'`)

	if resp := get(t, srv, "/user/wait/a"); resp.StatusCode != 200 {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
}

func TestBigScreen_ShowsOnlyTheTopThreePotentialConnectionsPerParticipant(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		seed(t, deps.db, id, id, "ready")
	}

	body := readBody(t, get(t, srv, "/bigscreen/graph-data"))

	// 5 Participants, at most 3 potential connections each, shared edges counted once.
	if n := strings.Count(body, `"matched": false`); n == 0 || n > 5*3 {
		t.Errorf("potential connections: got %d", n)
	}
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		if n := strings.Count(body, `"source": "`+id+`"`); n > 3 {
			t.Errorf("%s starts %d potential connections, at most 3", id, n)
		}
	}
}

// plainWriter is a ResponseWriter that cannot stream.
type plainWriter struct{ http.ResponseWriter }

func TestStreams_NeedAWriterThatCanStream(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "p", "P", "ready")
	for _, path := range []string{"/user/pipeline-stream/p", "/user/wait-stream/p", "/bigscreen/stream"} {
		rec := httptest.NewRecorder()
		srv.h.ServeHTTP(plainWriter{rec}, httptest.NewRequest("GET", path, nil))
		if rec.Code != 500 {
			t.Errorf("%s: want 500 without streaming support, got %d", path, rec.Code)
		}
	}
}

func TestStreams_EndWhenTheParticipantIsRemoved(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	deps.db.CreateParticipant("i", "i", "I", true)
	seed(t, deps.db, "w", "W", "ready")
	pipeline := open(t, srv, "/user/pipeline-stream/i")
	wait := open(t, srv, "/user/wait-stream/w")
	sentNothingNew(t, pipeline, 1, "", "before removal")
	sentNothingNew(t, wait, 1, "", "before removal")

	NewRelationships(deps.db).Remove("i")
	NewRelationships(deps.db).Remove("w")

	eventually(t, "the pipeline stream to end", pipeline.ended)
	eventually(t, "the wait stream to end", wait.ended)
}

func TestStreams_SendAHeartbeatWhenIdle(t *testing.T) {
	defer func(d time.Duration) { heartbeatEvery = d }(heartbeatEvery)
	heartbeatEvery = 10 * time.Millisecond
	srv, _ := newTestServer(t, nil, nil)

	s := open(t, srv, "/bigscreen/stream")

	waitFor(t, s, ": ping")
}

func TestRender_AMissingTemplateIsAnError(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	h := NewHandler(deps.db, deps.onboarding, nil, nil, nil, deps.matchmaking)
	rec := httptest.NewRecorder()

	h.render(rec, "no-such-template.html", nil)

	if rec.Code != 500 {
		t.Errorf("want 500, got %d", rec.Code)
	}
}
