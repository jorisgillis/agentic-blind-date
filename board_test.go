package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// This file covers the Board: Posts by ready Participants, the Host's approval,
// who sees what, the push to the personal pages, and the cleanup (Reset,
// Participant deletion). The Big Screen, the recap and the export are covered
// with their own surfaces.

//bodyclose:handled
func postTo(t *testing.T, srv *testSrv, id, text string) *http.Response {
	t.Helper()
	return post(t, srv, "/user/board/"+id, url.Values{"text": {text}})
}

// boardOf is the Board section of the Participant's personal page.
func boardOf(t *testing.T, srv *testSrv, id string) string {
	t.Helper()
	resp := get(t, srv, "/user/board/"+id)
	if resp.StatusCode != 200 {
		t.Fatalf("the board of %s: want 200, got %d", id, resp.StatusCode)
	}
	return readBody(t, resp)
}

// postID is the id of the Participant's nth Post (0-based, newest first among
// what they can see).
func postID(t *testing.T, deps *testDeps, participantID string, n int) int64 {
	t.Helper()
	var own []Post
	posts, err := deps.handler.board.Visible(participantID)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range posts {
		if p.ParticipantID == participantID {
			own = append(own, p)
		}
	}
	if len(own) <= n {
		t.Fatalf("%s has %d posts, want at least %d", participantID, len(own), n+1)
	}
	return own[n].ID
}

// moderate is the Host's decision on a Post, through the console's route.
//
//bodyclose:handled
func moderate(t *testing.T, srv *testSrv, id int64, action string) *http.Response {
	t.Helper()
	return post(t, srv, "/admin/board", url.Values{"id": {strconv.FormatInt(id, 10)}, "action": {action}})
}

// approved posts as a ready Participant and has the Host approve it.
func approved(t *testing.T, srv *testSrv, deps *testDeps, id, text string) int64 {
	t.Helper()
	if resp := postTo(t, srv, id, text); resp.StatusCode != 200 {
		t.Fatalf("posting %q: got %d", text, resp.StatusCode)
	}
	postID := postID(t, deps, id, 0)
	if resp := moderate(t, srv, postID, "approved"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("approving: got %d", resp.StatusCode)
	}
	return postID
}

func TestBoard_AParticipantPostsAndTheHostApproves(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")

	text := "Great read: https://example.org/agents?a=1&b=2 — worth it"
	resp := postTo(t, srv, "a", text)
	if resp.StatusCode != 200 {
		t.Fatalf("a valid post: want 200, got %d", resp.StatusCode)
	}
	mine := readBody(t, resp)
	for _, want := range []string{"Waiting for the Host", "The Gopher", `href="https://example.org/agents?a=1&amp;b=2"`, `rel="noopener noreferrer"`, "appears once the Host approves"} {
		if !strings.Contains(mine, want) {
			t.Errorf("the poster's board should contain %q", want)
		}
	}

	// Nobody else sees it, nor does it reach the ticker, before the Host approves.
	if other := boardOf(t, srv, "b"); strings.Contains(other, "Great read") {
		t.Error("a pending post should be visible to its poster only")
	}
	if activity := readBody(t, get(t, srv, "/data/activity")); strings.Contains(activity, "board") {
		t.Errorf("a pending post should not reach the ticker, got %s", activity)
	}

	// The Host sees it in the console and approves it.
	admin := readBody(t, get(t, srv, "/admin"))
	for _, want := range []string{"Great read", "1 waiting", `value="approved"`, `value="rejected"`} {
		if !strings.Contains(admin, want) {
			t.Errorf("the host console should contain %q", want)
		}
	}
	id := postID(t, deps, "a", 0)
	resp = moderate(t, srv, id, "approved")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin" {
		t.Fatalf("approving: want a redirect to the console, got %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}

	other := boardOf(t, srv, "b")
	for _, want := range []string{"Great read", "The Crab", "The Gopher", `rel="noopener noreferrer"`} {
		if want == "The Crab" {
			continue // b's own persona is not on a's post
		}
		if !strings.Contains(other, want) {
			t.Errorf("an approved post should show to everybody, missing %q", want)
		}
	}
	if strings.Contains(boardOf(t, srv, "a"), "Waiting for the Host") {
		t.Error("an approved post is no longer waiting")
	}
	if activity := readBody(t, get(t, srv, "/data/activity")); !strings.Contains(activity, "posted on the board") || strings.Contains(activity, "Great read") {
		t.Errorf("approving should log who posted, never the text, got %s", activity)
	}
}

func TestBoard_PostsAreValidatedAndGated(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "i", "The Newcomer", "interviewing")

	for _, tc := range []struct {
		name, id, text string
		want           int
		reason         string
	}{
		{"empty", "a", "", 400, "1–280 characters"},
		{"blank", "a", "  \n ", 400, "1–280 characters"},
		{"too long", "a", strings.Repeat("x", 281), 400, "1–280 characters"},
		{"too long in characters, not bytes", "a", strings.Repeat("é", 281), 400, "1–280 characters"},
		{"exactly the limit", "a", strings.Repeat("é", 280), 200, ""},
		{"not ready yet", "i", "hello", 400, "once your persona is ready"},
		{"unknown participant", "nobody", "hello", 404, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := postTo(t, srv, tc.id, tc.text)
			if resp.StatusCode != tc.want {
				t.Fatalf("want %d, got %d", tc.want, resp.StatusCode)
			}
			if tc.reason != "" && !strings.Contains(readBody(t, resp), tc.reason) {
				t.Errorf("the refusal should say %q", tc.reason)
			}
		})
	}
	if posts, _ := deps.handler.board.Visible("a"); len(posts) != 1 {
		t.Errorf("only the valid post should be stored, got %d", len(posts))
	}

	// Posting is open in every phase until the event closes.
	for _, phase := range []Phase{PhaseSocial, PhaseShowTell} {
		if err := deps.events.Advance(phase); err != nil {
			t.Fatal(err)
		}
		if resp := postTo(t, srv, "a", "during "+string(phase)); resp.StatusCode != 200 {
			t.Errorf("posting during %s: want 200, got %d", phase, resp.StatusCode)
		}
	}
	if err := deps.events.Advance(PhaseClosed); err != nil {
		t.Fatal(err)
	}
	resp := postTo(t, srv, "a", "too late")
	if resp.StatusCode != 400 || !strings.Contains(readBody(t, resp), "the evening is over") {
		t.Errorf("posting once closed: want 400 with the reason, got %d", resp.StatusCode)
	}
}

func TestBoard_AParticipantHoldsTenPosts(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	for i := 0; i < 10; i++ {
		if resp := postTo(t, srv, "a", "post "+strconv.Itoa(i)); resp.StatusCode != 200 {
			t.Fatalf("post %d: want 200, got %d", i, resp.StatusCode)
		}
	}
	if body := boardOf(t, srv, "a"); !strings.Contains(body, "Post (0 left)") {
		t.Error("the form should say no posts are left")
	}
	resp := postTo(t, srv, "a", "one too many")
	if resp.StatusCode != 400 || !strings.Contains(readBody(t, resp), "used all your posts") {
		t.Fatalf("the eleventh post: want 400 with the reason, got %d", resp.StatusCode)
	}

	// A deleted post frees its place, and so does a rejected one.
	if resp := post(t, srv, "/user/board/a/delete", url.Values{"post": {strconv.FormatInt(postID(t, deps, "a", 0), 10)}}); resp.StatusCode != 200 {
		t.Fatalf("deleting: got %d", resp.StatusCode)
	}
	if resp := postTo(t, srv, "a", "after a delete"); resp.StatusCode != 200 {
		t.Errorf("after a delete: want 200, got %d", resp.StatusCode)
	}
	moderate(t, srv, postID(t, deps, "a", 3), "rejected")
	if resp := postTo(t, srv, "a", "after a rejection"); resp.StatusCode != 200 {
		t.Errorf("after a rejection: want 200, got %d", resp.StatusCode)
	}
	if resp := postTo(t, srv, "a", "full again"); resp.StatusCode != 400 {
		t.Errorf("full again: want 400, got %d", resp.StatusCode)
	}
}

func TestBoard_WhoSeesWhat(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	postTo(t, srv, "a", "pending post")
	approved(t, srv, deps, "a", "approved post")
	postTo(t, srv, "a", "rejected post")
	moderate(t, srv, postID(t, deps, "a", 0), "rejected")

	mine, other := boardOf(t, srv, "a"), boardOf(t, srv, "b")
	for _, want := range []string{"pending post", "approved post", "rejected post", "Waiting for the Host", "Not approved"} {
		if !strings.Contains(mine, want) {
			t.Errorf("the poster sees all their posts with their status, missing %q", want)
		}
	}
	if !strings.Contains(other, "approved post") {
		t.Error("everybody sees the approved post")
	}
	for _, hidden := range []string{"pending post", "rejected post", "Waiting for the Host", "Not approved"} {
		if strings.Contains(other, hidden) {
			t.Errorf("others must not see %q", hidden)
		}
	}
	if strings.Index(mine, "rejected post") > strings.Index(mine, "approved post") ||
		strings.Index(mine, "approved post") > strings.Index(mine, "pending post") {
		t.Error("posts are listed newest first")
	}
	if !strings.Contains(mine, `action=`) && !strings.Contains(mine, "/user/board/a/delete") {
		t.Error("the poster can delete their own posts")
	}
	if strings.Contains(other, "/user/board/b/delete") {
		t.Error("others' posts have no delete button")
	}
}

func TestBoard_ThePagesCarryTheBoardInEveryPhase(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	seed(t, deps.db, "c", "The Snake", "ready")
	pair(t, deps.db, "a", "b")
	approved(t, srv, deps, "c", "board news")

	check := func(path string, phase Phase) {
		t.Helper()
		body := readBody(t, get(t, srv, path))
		if !strings.Contains(body, `id="board"`) || !strings.Contains(body, "board news") {
			t.Errorf("%s during %s should carry the board with its posts", path, phase)
		}
		hasForm := strings.Contains(body, `id="board-text"`)
		if hasForm == (phase == PhaseClosed) {
			t.Errorf("%s during %s: the post form should be there until the event closes (form present: %v)", path, phase, hasForm)
		}
	}
	check("/user/wait/c", PhaseOnboarding)
	for _, phase := range []Phase{PhaseSocial, PhaseShowTell, PhaseClosed} {
		if err := deps.events.Advance(phase); err != nil {
			t.Fatal(err)
		}
		check("/user/wait/c", phase)
		check("/user/match/a", phase)
	}
	if resp := get(t, srv, "/user/board/nobody"); resp.StatusCode != 404 {
		t.Errorf("an unknown participant: want 404, got %d", resp.StatusCode)
	}
}

func TestBoard_ADraftSurvivesARefreshAndAFailedPostIsShown(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	body := boardOf(t, srv, "a")
	if !strings.Contains(body, `id="board-text"`) || !strings.Contains(body, "hx-preserve") {
		t.Error("the draft should survive a refresh")
	}
	if !strings.Contains(body, "event.detail.xhr.status === 400") {
		t.Error("the section should swap a refused post in, so the reason is shown")
	}
	posted := readBody(t, postTo(t, srv, "a", "hello"))
	if strings.Contains(posted, "hx-preserve") {
		t.Error("a successful post clears the draft")
	}
}

func TestBoard_ThePosterDeletesTheirOwnPost(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	id := approved(t, srv, deps, "a", "regretted")
	idStr := strconv.FormatInt(id, 10)

	if resp := post(t, srv, "/user/board/b/delete", url.Values{"post": {idStr}}); resp.StatusCode != 404 {
		t.Errorf("deleting somebody else's post: want 404, got %d", resp.StatusCode)
	}
	if !strings.Contains(boardOf(t, srv, "b"), "regretted") {
		t.Error("somebody else's failed delete must leave the post")
	}
	for _, bad := range []string{"", "x"} {
		if resp := post(t, srv, "/user/board/a/delete", url.Values{"post": {bad}}); resp.StatusCode != 400 {
			t.Errorf("a bad post id %q: want 400, got %d", bad, resp.StatusCode)
		}
	}
	if resp := post(t, srv, "/user/board/nobody/delete", url.Values{"post": {idStr}}); resp.StatusCode != 404 {
		t.Errorf("an unknown participant: want 404, got %d", resp.StatusCode)
	}

	resp := post(t, srv, "/user/board/a/delete", url.Values{"post": {idStr}})
	if resp.StatusCode != 200 || strings.Contains(readBody(t, resp), "regretted") {
		t.Fatalf("deleting: want 200 without the post, got %d", resp.StatusCode)
	}
	if strings.Contains(boardOf(t, srv, "b"), "regretted") {
		t.Error("a deleted post is gone for everybody")
	}
	if resp := post(t, srv, "/user/board/a/delete", url.Values{"post": {idStr}}); resp.StatusCode != 404 {
		t.Errorf("deleting twice: want 404, got %d", resp.StatusCode)
	}
}

func TestBoard_TheHostModeratesInEveryPhase(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	postTo(t, srv, "a", "first")
	postTo(t, srv, "a", "second")
	first, second := postID(t, deps, "a", 1), postID(t, deps, "a", 0)

	if resp := moderate(t, srv, first, "rejected"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("rejecting: got %d", resp.StatusCode)
	}
	if resp := moderate(t, srv, first, "approved"); resp.StatusCode != 400 {
		t.Errorf("a rejected post is final: want 400, got %d", resp.StatusCode)
	}
	if resp := moderate(t, srv, first, "rejected"); resp.StatusCode != 400 {
		t.Errorf("rejecting twice: want 400, got %d", resp.StatusCode)
	}
	if resp := moderate(t, srv, second, "approved"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("approving: got %d", resp.StatusCode)
	}
	if resp := moderate(t, srv, second, "approved"); resp.StatusCode != 400 {
		t.Errorf("approving twice: want 400, got %d", resp.StatusCode)
	}

	// The console lists the approved post with a take-down, and not the rejected one.
	admin := readBody(t, get(t, srv, "/admin"))
	if !strings.Contains(admin, "second") || strings.Contains(admin, "first") || !strings.Contains(admin, "Take down") {
		t.Error("the console lists approved posts with a take-down, and no rejected ones")
	}

	// Taking an approved post down works after the close, too.
	if err := deps.events.Advance(PhaseClosed); err != nil {
		t.Fatal(err)
	}
	if resp := moderate(t, srv, second, "rejected"); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("taking down after the close: got %d", resp.StatusCode)
	}
	if strings.Contains(boardOf(t, srv, "b"), "second") {
		t.Error("a taken-down post is gone for others")
	}
	if !strings.Contains(boardOf(t, srv, "a"), "Not approved") {
		t.Error("its poster sees that it was not approved")
	}

	for name, form := range map[string]url.Values{
		"no id":            {"action": {"approved"}},
		"a bad id":         {"id": {"x"}, "action": {"approved"}},
		"an unknown id":    {"id": {"999"}, "action": {"approved"}},
		"a bad action":     {"id": {strconv.FormatInt(first, 10)}, "action": {"pending"}},
		"a missing action": {"id": {strconv.FormatInt(first, 10)}},
	} {
		want := 400
		if name == "an unknown id" {
			want = 404
		}
		if resp := post(t, srv, "/admin/board", form); resp.StatusCode != want {
			t.Errorf("%s: want %d, got %d", name, want, resp.StatusCode)
		}
	}
	if !strings.Contains(readBody(t, get(t, srv, "/admin")), "Nothing on the board yet") {
		t.Error("an empty console says so")
	}
}

func TestBoard_PersonalStreamsRefreshWhenWhatIsVisibleChanges(t *testing.T) {
	for _, path := range []string{"/user/wait-stream/w", "/user/match-stream/w"} {
		t.Run(path, func(t *testing.T) {
			srv, deps := newTestServer(t, nil, nil)
			seed(t, deps.db, "w", "W", "ready")
			seed(t, deps.db, "x", "X", "ready")
			s := open(t, srv, path)
			waitFor(t, s, "event: refresh") // the sync on connect
			before := s.String()
			sentNothingNew(t, s, 1, before, "nothing changed")

			// Somebody else's pending post is invisible to w: nothing to refresh.
			postTo(t, srv, "x", "hello")
			sentNothingNew(t, s, 2, before, "a pending post of another")

			id := postID(t, deps, "x", 0)
			moderate(t, srv, id, "approved")
			waitForCount(t, s, "event: refresh", strings.Count(before, "event: refresh")+1)

			before = s.String()
			moderate(t, srv, id, "rejected")
			eventually(t, "the take-down to refresh the page", func() bool { return len(s.String()) > len(before) })

			before = s.String()
			postTo(t, srv, "w", "mine")
			eventually(t, "the poster's own post to refresh the page", func() bool { return len(s.String()) > len(before) })
		})
	}
}

func TestBoard_AResetDeletesThePosts(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	approved(t, srv, deps, "a", "one")
	postTo(t, srv, "a", "two")

	restore := failWrites(t, deps.db, "reset_board_posts")
	if resp := post(t, srv, "/admin/reset", url.Values{}); resp.StatusCode != 500 {
		t.Fatalf("a failed reset: want 500, got %d", resp.StatusCode)
	}
	if posts, _ := deps.handler.board.Approved(-1); len(posts) != 1 {
		t.Fatal("a failed reset should leave everything as it was")
	}
	restore()

	if resp := post(t, srv, "/admin/reset", url.Values{}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("reset: want a redirect, got %d", resp.StatusCode)
	}
	var left int
	must(t, deps.db.db.QueryRow(`SELECT COUNT(*) FROM board_posts`).Scan(&left))
	if left != 0 {
		t.Errorf("a reset should delete every post, %d are left", left)
	}
}

func TestBoard_ADeletedParticipantTakesTheirPostsAway(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	approved(t, srv, deps, "a", "from a")
	approved(t, srv, deps, "b", "from b")

	req, _ := http.NewRequest("DELETE", srv.URL+"/data/participant/a", nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != 200 {
		t.Fatalf("deleting the participant: want 200, got %d", resp.StatusCode)
	}
	body := boardOf(t, srv, "b")
	if strings.Contains(body, "from a") || !strings.Contains(body, "from b") {
		t.Error("only the deleted participant's posts should go")
	}
	var left int
	must(t, deps.db.db.QueryRow(`SELECT COUNT(*) FROM board_posts WHERE participant_id = 'a'`).Scan(&left))
	if left != 0 {
		t.Errorf("the rows should go, %d are left", left)
	}

	seed(t, deps.db, "c", "The Snake", "ready")
	approved(t, srv, deps, "c", "from c")
	failWrites(t, deps.db, "board_delete")
	req, _ = http.NewRequest("DELETE", srv.URL+"/data/participant/c", nil)
	resp, err = srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != 500 {
		t.Errorf("a failed cleanup: want 500, got %d", resp.StatusCode)
	}
}

func TestBoard_WriteFailuresAreServerErrors(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	id := approved(t, srv, deps, "a", "stays")
	idStr := strconv.FormatInt(id, 10)

	breakOnFault(t, deps.db, "board_insert")
	if resp := postTo(t, srv, "a", "fails"); resp.StatusCode != 500 {
		t.Errorf("a failed post: want 500, got %d", resp.StatusCode)
	}
	breakOnFault(t, deps.db, "board_update")
	if resp := moderate(t, srv, id, "rejected"); resp.StatusCode != 500 {
		t.Errorf("a failed moderation: want 500, got %d", resp.StatusCode)
	}
	breakOnFault(t, deps.db, "board_delete")
	if resp := post(t, srv, "/user/board/a/delete", url.Values{"post": {idStr}}); resp.StatusCode != 500 {
		t.Errorf("a failed delete: want 500, got %d", resp.StatusCode)
	}
	if !strings.Contains(boardOf(t, srv, "a"), "stays") {
		t.Error("failed writes should leave the post")
	}
}

func TestBoard_DatabaseReadFailuresAreReported(t *testing.T) {
	for _, tag := range []string{"board_read", "board_query_read"} {
		t.Run(tag, func(t *testing.T) {
			srv, deps := newTestServer(t, nil, nil)
			seed(t, deps.db, "a", "The Gopher", "ready")
			id := approved(t, srv, deps, "a", "a post")
			board := deps.handler.board
			breakOnFault(t, deps.db, tag)

			if _, err := board.Visible("a"); err == nil {
				t.Error("Visible should report the failure")
			}
			if _, err := board.Approved(-1); err == nil {
				t.Error("Approved should report the failure")
			}
			if _, err := board.Moderation(); err == nil {
				t.Error("Moderation should report the failure")
			}
			if _, err := board.Moderate(id, PostRejected); err == nil || errors.Is(err, ErrPostNotFound) {
				t.Errorf("Moderate should report the failure, got %v", err)
			}
			if sig := board.Signature("a"); sig != "" {
				t.Errorf("the signature should be empty, got %q", sig)
			}

			// The pages carry on without posts; posting itself is a server error
			// when the cap cannot be checked.
			for _, path := range []string{"/user/board/a", "/user/wait/a", "/admin"} {
				resp := get(t, srv, path)
				if resp.StatusCode != 200 {
					t.Errorf("%s: want 200, got %d", path, resp.StatusCode)
				}
				if body := readBody(t, resp); strings.Contains(body, "a post") {
					t.Errorf("%s should show no posts while they cannot be read", path)
				}
			}
			if tag == "board_query_read" {
				if resp := postTo(t, srv, "a", "cannot count"); resp.StatusCode != 500 {
					t.Errorf("posting when the cap cannot be checked: want 500, got %d", resp.StatusCode)
				}
			}
		})
	}
}

// bigScreenBoard is the Board in the Big Screen's payload, nil when it has none.
func bigScreenBoard(t *testing.T, srv *testSrv) []any {
	t.Helper()
	resp := get(t, srv, "/bigscreen/graph-data")
	if resp.StatusCode != 200 {
		t.Fatalf("the screen's payload: want 200, got %d", resp.StatusCode)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(readBody(t, resp)), &payload); err != nil {
		t.Fatal(err)
	}
	board, _ := payload["board"].([]any)
	return board
}

func TestBoard_TheBigScreenRotatesTheApprovedPosts(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")

	if board := bigScreenBoard(t, srv); board != nil {
		t.Errorf("without posts the payload has no board, got %v", board)
	}
	postTo(t, srv, "a", "pending, so not for the screen")
	if board := bigScreenBoard(t, srv); board != nil {
		t.Errorf("a pending post must not reach the screen, got %v", board)
	}

	approved(t, srv, deps, "a", "first: https://example.org/one.")
	approved(t, srv, deps, "b", "second, no link here")
	board := bigScreenBoard(t, srv)
	if len(board) != 2 {
		t.Fatalf("want the 2 approved posts, got %v", board)
	}
	newest, older := board[0].(map[string]any), board[1].(map[string]any)
	if newest["text"] != "second, no link here" || newest["link"] != "" || newest["persona_name"] != "The Crab" || newest["persona_symbol"] == "" {
		t.Errorf("the newest post comes first, with its persona and no link, got %v", newest)
	}
	if older["link"] != "https://example.org/one" || older["persona_name"] != "The Gopher" {
		t.Errorf("the older post carries its first link without the full stop, got %v", older)
	}
	for _, key := range []string{"participant_id", "handle", "name"} {
		if _, leaks := newest[key]; leaks {
			t.Errorf("the screen shows personas only, but the post carries %q", key)
		}
	}

	page := readBody(t, get(t, srv, "/bigscreen"))
	for _, want := range []string{`id="board"`, `id="board-qr"`, "updateBoard(data.board)"} {
		if !strings.Contains(page, want) {
			t.Errorf("the Big Screen page should contain %q", want)
		}
	}
}

func TestBoard_TheBigScreenCarriesTheTenNewestPostsInEveryPhase(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	for i := 0; i < 6; i++ {
		approved(t, srv, deps, "a", "a"+strconv.Itoa(i))
		approved(t, srv, deps, "b", "b"+strconv.Itoa(i))
	}
	for _, phase := range []Phase{PhaseOnboarding, PhaseSocial, PhaseShowTell, PhaseClosed} {
		if phase != PhaseOnboarding {
			if err := deps.events.Advance(phase); err != nil {
				t.Fatal(err)
			}
		}
		board := bigScreenBoard(t, srv)
		if len(board) != 10 {
			t.Fatalf("during %s: want the 10 newest posts, got %d", phase, len(board))
		}
		if first := board[0].(map[string]any)["text"]; first != "b5" {
			t.Errorf("during %s: the newest post comes first, got %v", phase, first)
		}
	}
}

func TestBoard_TheBigScreenLeavesTheBoardOutWhileAPresenterIsOnStage(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	approved(t, srv, deps, "b", "board news")
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	if board := bigScreenBoard(t, srv); len(board) != 1 {
		t.Fatalf("with nobody on stage the board rotates, got %v", board)
	}

	signup := stagePresenter(t, deps, "a")
	if board := bigScreenBoard(t, srv); board != nil {
		t.Errorf("while a presenter is on stage the board is left out, got %v", board)
	}
	if _, err := deps.showtell.Done(signup.ID); err != nil {
		t.Fatal(err)
	}
	if board := bigScreenBoard(t, srv); len(board) != 1 {
		t.Errorf("once the stage is free the board is back, got %v", board)
	}
}

func TestBoard_TheBigScreenPushesTheBoardOverTheStream(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	s := open(t, srv, "/bigscreen/stream")
	waitFor(t, s, "data: ")

	approved(t, srv, deps, "a", "live news")
	eventually(t, "the approved post to be pushed", func() bool { return strings.Contains(s.String(), "live news") })
}

func TestBoard_TheBigScreenCarriesOnWhenTheBoardCannotBeRead(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	approved(t, srv, deps, "a", "unreadable")
	breakOnFault(t, deps.db, "board_read")
	if board := bigScreenBoard(t, srv); board != nil {
		t.Errorf("a board that cannot be read is left out, got %v", board)
	}
}

func TestBoard_TheRecapAndTheExportCarryTheApprovedPosts(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "handle-ada", "The Gopher", "ready")
	seed(t, deps.db, "handle-bob", "The Crab", "ready")

	postTo(t, srv, "handle-ada", "still pending")
	postTo(t, srv, "handle-ada", "refused post")
	moderate(t, srv, postID(t, deps, "handle-ada", 0), "rejected")
	postTo(t, srv, "handle-ada", "deleted post")
	post(t, srv, "/user/board/handle-ada/delete", url.Values{"post": {strconv.FormatInt(postID(t, deps, "handle-ada", 0), 10)}})
	approved(t, srv, deps, "handle-ada", "older: https://example.org/talk.")
	approved(t, srv, deps, "handle-bob", "newer\nover two   lines")
	if err := deps.events.Advance(PhaseClosed); err != nil {
		t.Fatal(err)
	}

	absent := []string{"still pending", "refused post", "deleted post", "handle-ada", "handle-bob"}
	check := func(name, body string) {
		t.Helper()
		for _, hidden := range absent {
			if strings.Contains(body, hidden) {
				t.Errorf("%s should not carry %q", name, hidden)
			}
		}
	}

	recap := readBody(t, get(t, srv, "/recap"))
	for _, want := range []string{"Shared on the board", "older:", `href="https://example.org/talk"`, `rel="noopener noreferrer"`, "The Gopher", "The Crab"} {
		if !strings.Contains(recap, want) {
			t.Errorf("the recap should contain %q", want)
		}
	}
	if strings.Index(recap, "newer") > strings.Index(recap, "older:") {
		t.Error("the recap lists the newest post first")
	}
	check("the recap", recap)

	var pack struct {
		Board []struct {
			Text   string
			Poster map[string]any
		}
	}
	jsonBody := readBody(t, get(t, srv, "/admin/export"))
	if err := json.Unmarshal([]byte(jsonBody), &pack); err != nil {
		t.Fatal(err)
	}
	if len(pack.Board) != 2 || pack.Board[0].Text != "newer\nover two   lines" || pack.Board[0].Poster["name"] != "The Crab" || pack.Board[1].Poster["name"] != "The Gopher" {
		t.Errorf("the export's board: want the 2 approved posts, newest first, with their personas, got %+v", pack.Board)
	}
	check("the JSON export", jsonBody)

	md := readBody(t, get(t, srv, "/admin/export?format=md"))
	for _, want := range []string{"## 📣 Board", "**The Crab**: newer over two lines", "**The Gopher**: older: https://example.org/talk."} {
		if !strings.Contains(md, want) {
			t.Errorf("the Markdown export should contain %q", want)
		}
	}
	check("the Markdown export", md)
}

func TestBoard_AnEmptyBoardSaysSoInTheRecapAndTheExport(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	if err := deps.events.Advance(PhaseClosed); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"/recap": "Nothing was posted.", "/admin/export?format=md": "Nothing was posted."} {
		if body := readBody(t, get(t, srv, path)); !strings.Contains(body, want) {
			t.Errorf("%s should say %q", path, want)
		}
	}
	if body := readBody(t, get(t, srv, "/admin/export")); !strings.Contains(body, `"board": []`) {
		t.Errorf("the JSON export should carry an empty board list, got %s", body)
	}
}

func TestBoard_TheRecapAndTheExportCarryOnWhenTheBoardCannotBeRead(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	approved(t, srv, deps, "a", "unreadable")
	if err := deps.events.Advance(PhaseClosed); err != nil {
		t.Fatal(err)
	}
	breakOnFault(t, deps.db, "board_read")
	for _, path := range []string{"/recap", "/admin/export", "/admin/export?format=md"} {
		resp := get(t, srv, path)
		if resp.StatusCode != 200 {
			t.Errorf("%s: want 200, got %d", path, resp.StatusCode)
		}
		if body := readBody(t, resp); strings.Contains(body, "unreadable") {
			t.Errorf("%s should leave out a board that cannot be read", path)
		}
	}
}
