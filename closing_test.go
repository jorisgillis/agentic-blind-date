package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// This file covers ticket #122: the closing ceremony — the Big Screen's
// closing view (stats, final graph, champion, recap QR), the public event
// recap (gated to closed, personas only) and the host export pack (JSON and
// Markdown, personas and counts only), plus the success-metric derivation
// and the disabled meeting confirmation when closed.

// closingSetup seeds the closing ceremony's stage: three Participants, aaa
// matched with bbb, a pending meeting between aaa and bbb, a completed
// meeting between aaa and ccc, contact channels on both sides, one show &
// tell sign-up on stage, and two audience questions (one answered, one
// dismissed). The event is left at show & tell — closeEvent closes it.
func closingSetup(t *testing.T) (*testSrv, *testDeps) {
	t.Helper()
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "aaa", "The Gopher", "ready")
	seed(t, deps.db, "bbb", "The YAML Wrangler", "ready")
	seed(t, deps.db, "ccc", "The Kernel Panicker", "ready")
	setProfile(t, deps.db, "aaa", &GitHubProfile{Login: "octocat-aaa"})
	setProfile(t, deps.db, "bbb", &GitHubProfile{Login: "octocat-bbb"})
	setProfile(t, deps.db, "ccc", &GitHubProfile{Login: "octocat-ccc"})
	pair(t, deps.db, "aaa", "bbb")
	mustAdvance(t, deps.events, PhaseSocial)

	// Show & tell: bbb signs up and goes on stage; the audience asks.
	if err := deps.showtell.Signup(reload(t, deps.db, "bbb"), "Shipping fearless", "Friday deploys, live", "https://talks.dev/fearless"); err != nil {
		t.Fatal(err)
	}
	mustAdvance(t, deps.events, PhaseShowTell)
	signup, err := deps.showtell.ForParticipant("bbb")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deps.showtell.Stage(signup.ID); err != nil {
		t.Fatal(err)
	}
	if err := deps.showtell.Ask(reload(t, deps.db, "ccc"), "How do you test a Friday deploy?"); err != nil {
		t.Fatal(err)
	}
	if err := deps.showtell.Ask(reload(t, deps.db, "aaa"), "What is your favourite bug?"); err != nil {
		t.Fatal(err)
	}
	questions, err := deps.showtell.Questions()
	if err != nil {
		t.Fatal(err)
	}
	// The host asks and answers the first question, dismisses the second.
	if _, err := deps.showtell.TransitionQuestion(questions[0].ID, QuestionAsked); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.showtell.TransitionQuestion(questions[0].ID, QuestionAnswered); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.showtell.TransitionQuestion(questions[1].ID, QuestionDismissed); err != nil {
		t.Fatal(err)
	}

	// Meetings: aaa–bbb pending (one side only), aaa–ccc completed.
	ms := NewMeetings(deps.db, deps.events)
	if _, _, err := ms.Confirm(reload(t, deps.db, "aaa"), reload(t, deps.db, "bbb"), OriginOther); err != nil {
		t.Fatal(err)
	}
	completeMeeting(t, deps.db, ms, "aaa", "ccc")

	// Contact channels: aaa shares two, ccc shares one.
	channels := NewContactChannels(deps.db)
	if err := channels.Save("aaa", []ContactChannel{
		{Channel: "email", Value: "gopher@example.com", Enabled: true},
		{Channel: "github", Value: "octocat-aaa", Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := channels.Save("ccc", []ContactChannel{
		{Channel: "github", Value: "octocat-ccc", Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}
	return srv, deps
}

// closeEvent closes the event.
func closeEvent(t *testing.T, deps *testDeps) {
	t.Helper()
	mustAdvance(t, deps.events, PhaseClosed)
}

// mustAdvance advances the event, failing the test when it cannot.
func mustAdvance(t *testing.T, events *EventStore, to Phase) {
	t.Helper()
	if err := events.Advance(to); err != nil {
		t.Fatal(err)
	}
}

// pairWith pairs two Participants with a given assessment — test setup for
// the best-matches ranking.
func pairWith(t *testing.T, db *DB, a, b string, score int, reason string) {
	t.Helper()
	result := &matchResult{Score: score, Reason: reason, RedFlags: []string{}, GreenFlags: []string{}, Icebreakers: []string{}}
	if _, err := NewRelationships(db).Pair(Match{A: reload(t, db, a), B: reload(t, db, b), Result: result}); err != nil {
		t.Fatal(err)
	}
}

// assertNoIdentities fails the test when body carries any identity or
// contact value of the Participants in db: participant ids, GitHub handles,
// names, meet codes, contact-channel values and question texts. The closing
// surfaces — the public recap and the export pack — are personas and counts
// only.
func assertNoIdentities(t *testing.T, body string, db *DB) {
	t.Helper()
	participants, err := NewParticipantStore(db).All()
	if err != nil {
		t.Fatal(err)
	}
	var secrets []string
	for _, p := range participants {
		secrets = append(secrets, p.ID, p.GitHubHandle, p.Name, p.MeetCode)
		if p.Profile != nil && p.Profile.Login != "" {
			secrets = append(secrets, p.Profile.Login, "@"+p.Profile.Login)
		}
	}
	channels := NewContactChannels(db)
	for _, p := range participants {
		list, err := channels.Channels(p.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, ch := range list {
			if ch.Value != "" {
				secrets = append(secrets, ch.Value)
			}
		}
	}
	questions, err := NewShowTell(db, nil).Questions()
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range questions {
		secrets = append(secrets, q.Text)
	}
	for _, secret := range secrets {
		if secret != "" && strings.Contains(body, secret) {
			t.Errorf("the body must not carry %q — personas and counts only", secret)
		}
	}
}

// ── the evening's stats ────────────────────────────────────────────────────

func TestClosing_StatsDerivesTheEveningsNumbers(t *testing.T) {
	_, deps := closingSetup(t)
	closeEvent(t, deps)

	stats := deps.handler.closing.Stats()
	want := EventStats{
		Participants:      3,
		Ready:             3,
		Matched:           2,
		Meetings:          1, // the aaa–ccc meeting; the pending aaa–bbb one does not count
		ContactExchanges:  3, // aaa's two opted-in channels + ccc's one, on the completed meeting
		Questions:         2,
		QuestionsAnswered: 1, // the dismissed question is not an answered one
	}
	if stats != want {
		t.Errorf("stats: want %+v, got %+v", want, stats)
	}
}

// TestClosing_StatsSurviveReadFailures covers the stats' error paths: a
// number that cannot be read comes back as zero.
func TestClosing_StatsSurviveReadFailures(t *testing.T) {
	t.Run("meetings unreadable", func(t *testing.T) {
		_, deps := closingSetup(t)
		closeEvent(t, deps)
		breakOnFault(t, deps.db, "meet_read")
		stats := deps.handler.closing.Stats()
		if stats.Meetings != 0 || stats.ContactExchanges != 0 {
			t.Errorf("without readable meetings the meeting stats are zero, got %+v", stats)
		}
		if stats.Questions != 2 {
			t.Errorf("the questions are still counted, got %+v", stats)
		}
	})
	t.Run("channels unreadable", func(t *testing.T) {
		_, deps := closingSetup(t)
		closeEvent(t, deps)
		breakOnFault(t, deps.db, "channel_read")
		stats := deps.handler.closing.Stats()
		if stats.Meetings != 1 {
			t.Errorf("the meeting is still counted, got %+v", stats)
		}
		if stats.ContactExchanges != 0 {
			t.Errorf("without readable channels the exchanges are zero, got %+v", stats)
		}
	})
	t.Run("questions unreadable", func(t *testing.T) {
		_, deps := closingSetup(t)
		closeEvent(t, deps)
		breakOnFault(t, deps.db, "showtell_question_read")
		stats := deps.handler.closing.Stats()
		if stats.Questions != 0 || stats.QuestionsAnswered != 0 {
			t.Errorf("without readable questions the question stats are zero, got %+v", stats)
		}
		if stats.Meetings != 1 {
			t.Errorf("the meetings are still counted, got %+v", stats)
		}
	})
}

// ── the success metrics ────────────────────────────────────────────────────

func TestClosing_SuccessMetrics(t *testing.T) {
	board := []ExportStanding{{Rank: 1, Connections: 3}, {Rank: 2, Connections: 1}}

	m := successMetrics(EventStats{Meetings: 2, QuestionsAnswered: 9}, board)
	if m.Meetings != 2 || m.Connections != 4 || m.QuestionsAnswered != 9 || m.AudienceParticipation {
		t.Errorf("below the target the audience participation is not met: %+v", m)
	}
	m = successMetrics(EventStats{Meetings: 2, QuestionsAnswered: 10}, board)
	if !m.AudienceParticipation {
		t.Errorf("ten answered questions meet the audience-participation target: %+v", m)
	}
	m = successMetrics(EventStats{}, nil)
	if m.Connections != 0 || m.AudienceParticipation {
		t.Errorf("an empty evening meets nothing: %+v", m)
	}
}

// ── the closing surfaces' building blocks ──────────────────────────────────

func TestClosing_LeaderboardIsPersonaOnly(t *testing.T) {
	_, deps := closingSetup(t)
	closeEvent(t, deps)

	board := deps.handler.closing.Leaderboard()
	if len(board) != 3 {
		t.Fatalf("want 3 standings, got %d", len(board))
	}
	// Ties break by persona name: The Gopher ranks above The Kernel Panicker.
	want := []struct {
		rank        int
		connections int
		name        string
	}{
		{1, 1, "The Gopher"},
		{2, 1, "The Kernel Panicker"},
		{3, 0, "The YAML Wrangler"},
	}
	for i, w := range want {
		if board[i].Rank != w.rank || board[i].Connections != w.connections || board[i].Name != w.name {
			t.Errorf("board[%d]: want %+v, got %+v", i, w, board[i])
		}
	}
	// Persona-only: the JSON carries no participant ids or handles.
	encoded, _ := json.Marshal(board)
	assertNoIdentities(t, string(encoded), deps.db)
}

func TestClosing_LeaderboardSurvivesReadFailures(t *testing.T) {
	_, deps := closingSetup(t)
	closeEvent(t, deps)
	breakOnFault(t, deps.db, "connection_read")
	if board := deps.handler.closing.Leaderboard(); board != nil {
		t.Errorf("an unreadable leaderboard comes back empty, got %+v", board)
	}
}

func TestClosing_Gallery(t *testing.T) {
	_, deps := closingSetup(t)
	closeEvent(t, deps)

	gallery := deps.handler.closing.Gallery()
	if len(gallery) != 3 {
		t.Fatalf("want 3 personas, got %d", len(gallery))
	}
	if gallery[0].Name != "The Gopher" || gallery[0].Tagline != "Ships things" {
		t.Errorf("the gallery carries the personas with their taglines: %+v", gallery[0])
	}
	encoded, _ := json.Marshal(gallery)
	assertNoIdentities(t, string(encoded), deps.db)
}

func TestClosing_GallerySurvivesReadFailures(t *testing.T) {
	_, deps := closingSetup(t)
	closeEvent(t, deps)
	breakOnFault(t, deps.db, "participant_read")
	if gallery := deps.handler.closing.Gallery(); gallery != nil {
		t.Errorf("an unreadable gallery comes back empty, got %+v", gallery)
	}
}

func TestClosing_Talks(t *testing.T) {
	_, deps := closingSetup(t)
	closeEvent(t, deps)

	talks := deps.handler.closing.Talks()
	if len(talks) != 1 {
		t.Fatalf("want 1 talk, got %d", len(talks))
	}
	talk := talks[0]
	if talk.Title != "Shipping fearless" || talk.Description != "Friday deploys, live" || talk.RepoURL != "https://talks.dev/fearless" {
		t.Errorf("the talk: %+v", talk)
	}
	if talk.Status != SignupOnStage || talk.Presenter.Name != "The YAML Wrangler" {
		t.Errorf("the talk's presenter persona: %+v", talk)
	}
	encoded, _ := json.Marshal(talks)
	assertNoIdentities(t, string(encoded), deps.db)
}

func TestClosing_TalksSurviveReadFailures(t *testing.T) {
	_, deps := closingSetup(t)
	closeEvent(t, deps)
	breakOnFault(t, deps.db, "showtell_read")
	if talks := deps.handler.closing.Talks(); talks != nil {
		t.Errorf("an unreadable queue comes back empty, got %+v", talks)
	}
}

// ── the best matches ───────────────────────────────────────────────────────

func TestClosing_BestMatches(t *testing.T) {
	t.Run("the best matches, anonymized to personas", func(t *testing.T) {
		_, deps := closingSetup(t)
		closeEvent(t, deps)

		matches := deps.handler.closing.BestMatches()
		if len(matches) != 1 {
			t.Fatalf("want 1 match, got %d", len(matches))
		}
		m := matches[0]
		if m.Score != 91 || m.Reason != "Both love tabs" || len(m.Icebreakers) != 1 || m.Icebreakers[0] != "Why tabs?" {
			t.Errorf("the match's assessment: %+v", m)
		}
		if m.A.Name != "The Gopher" || m.B.Name != "The YAML Wrangler" {
			t.Errorf("the match is anonymized to the pair's personas: %+v", m)
		}
		encoded, _ := json.Marshal(matches)
		assertNoIdentities(t, string(encoded), deps.db)
	})
	t.Run("ranked by score, capped at three", func(t *testing.T) {
		db := newTestDB(t)
		closing := NewClosing(db, NewMeetings(db, NewEventStore(db)), NewContactChannels(db), NewShowTell(db, nil))
		for _, s := range [][2]string{{"aa", "The Alpha"}, {"bb", "The Bravo"}, {"cc", "The Charlie"}, {"dd", "The Delta"},
			{"ee", "The Echo"}, {"ff", "The Foxtrot"}, {"gg", "The Golf"}, {"hh", "The Hotel"}} {
			seed(t, db, s[0], s[1], "ready")
		}
		pairWith(t, db, "aa", "bb", 91, "first")
		pairWith(t, db, "cc", "dd", 80, "second")
		pairWith(t, db, "ee", "ff", 70, "third")
		pairWith(t, db, "gg", "hh", 60, "fourth")

		matches := closing.BestMatches()
		if len(matches) != bestMatchesLimit {
			t.Fatalf("the list is capped at %d, got %d", bestMatchesLimit, len(matches))
		}
		for i, want := range []int{91, 80, 70} {
			if matches[i].Score != want {
				t.Errorf("matches[%d]: want score %d, got %+v", i, want, matches[i])
			}
		}
	})
	t.Run("ties break by persona name", func(t *testing.T) {
		db := newTestDB(t)
		closing := NewClosing(db, NewMeetings(db, NewEventStore(db)), NewContactChannels(db), NewShowTell(db, nil))
		seed(t, db, "aa", "The Zulu", "ready")
		seed(t, db, "bb", "The Yankee", "ready")
		seed(t, db, "cc", "The Alpha", "ready")
		seed(t, db, "dd", "The Bravo", "ready")
		pairWith(t, db, "aa", "bb", 90, "zulu-yankee")
		pairWith(t, db, "cc", "dd", 90, "alpha-bravo")

		matches := closing.BestMatches()
		if len(matches) != 2 || matches[0].A.Name != "The Alpha" || matches[1].A.Name != "The Zulu" {
			t.Errorf("ties break by persona name: %+v", matches)
		}
	})
	t.Run("full ties break by the partner's persona name", func(t *testing.T) {
		db := newTestDB(t)
		closing := NewClosing(db, NewMeetings(db, NewEventStore(db)), NewContactChannels(db), NewShowTell(db, nil))
		seed(t, db, "aa", "The Twin", "ready")
		seed(t, db, "bb", "The Bravo", "ready")
		seed(t, db, "cc", "The Twin", "ready")
		seed(t, db, "dd", "The Alpha", "ready")
		pairWith(t, db, "aa", "bb", 90, "twin-bravo")
		pairWith(t, db, "cc", "dd", 90, "twin-alpha")

		matches := closing.BestMatches()
		if len(matches) != 2 || matches[0].B.Name != "The Alpha" || matches[1].B.Name != "The Bravo" {
			t.Errorf("full ties break by the partner's persona name: %+v", matches)
		}
	})
	t.Run("a match whose partner left is skipped", func(t *testing.T) {
		db := newTestDB(t)
		closing := NewClosing(db, NewMeetings(db, NewEventStore(db)), NewContactChannels(db), NewShowTell(db, nil))
		seed(t, db, "aa", "The Alpha", "ready")
		seed(t, db, "bb", "The Bravo", "ready")
		pair(t, db, "aa", "bb")
		// Test setup that deliberately bypasses guarded transitions: a
		// dangling matched_with names a partner that does not exist, which
		// no pairing operation produces.
		db.db.Exec(`UPDATE participants SET matched_with = 'ghost' WHERE id = 'aa'`)
		db.db.Exec(`UPDATE participants SET matched_with = '' WHERE id = 'bb'`)
		if matches := closing.BestMatches(); len(matches) != 0 {
			t.Errorf("a pair whose partner left is skipped, got %+v", matches)
		}
	})
	t.Run("participants unreadable", func(t *testing.T) {
		_, deps := closingSetup(t)
		closeEvent(t, deps)
		breakOnFault(t, deps.db, "participant_read")
		if matches := deps.handler.closing.BestMatches(); matches != nil {
			t.Errorf("an unreadable participant list comes back empty, got %+v", matches)
		}
	})
}

// ── the export pack ────────────────────────────────────────────────────────

func TestClosing_Pack(t *testing.T) {
	_, deps := closingSetup(t)
	closeEvent(t, deps)

	pack := deps.handler.closing.Pack()
	if pack.Stats.Meetings != 1 || pack.Stats.ContactExchanges != 3 || pack.Stats.QuestionsAnswered != 1 {
		t.Errorf("the pack's stats: %+v", pack.Stats)
	}
	if pack.Success.Meetings != 1 || pack.Success.Connections != 2 || pack.Success.AudienceParticipation {
		t.Errorf("the pack's success metrics: %+v", pack.Success)
	}
	if len(pack.Leaderboard) != 3 || len(pack.TopPersonas) != 3 || len(pack.BestMatches) != 1 || len(pack.ShowTell) != 1 {
		t.Errorf("the pack's lists: %+v", pack)
	}
	if pack.TopPersonas[0].Name != "The Gopher" || pack.TopPersonas[0].Tagline != "Ships things" {
		t.Errorf("the top personas carry taglines: %+v", pack.TopPersonas[0])
	}
	// Personas and counts only.
	encoded, _ := json.Marshal(pack)
	assertNoIdentities(t, string(encoded), deps.db)
}

// TestClosing_PackSurvivesReadFailures covers the pack builder's error
// paths: a list that cannot be read comes back empty.
func TestClosing_PackSurvivesReadFailures(t *testing.T) {
	t.Run("leaderboard unreadable", func(t *testing.T) {
		_, deps := closingSetup(t)
		closeEvent(t, deps)
		breakOnFault(t, deps.db, "connection_read")
		pack := deps.handler.closing.Pack()
		if len(pack.Leaderboard) != 0 || len(pack.TopPersonas) != 0 {
			t.Errorf("without a readable leaderboard the pack's ranking is empty: %+v", pack)
		}
		if pack.Stats.Questions != 2 {
			t.Errorf("the stats are still derived: %+v", pack.Stats)
		}
	})
	t.Run("participants unreadable", func(t *testing.T) {
		_, deps := closingSetup(t)
		closeEvent(t, deps)
		breakOnFault(t, deps.db, "participant_read")
		pack := deps.handler.closing.Pack()
		if len(pack.Leaderboard) != 0 || len(pack.TopPersonas) != 0 || len(pack.BestMatches) != 0 {
			t.Errorf("without readable participants the pack's persona lists are empty: %+v", pack)
		}
	})
}

func TestClosing_PackCapsTheTopPersonas(t *testing.T) {
	db := newTestDB(t)
	closing := NewClosing(db, NewMeetings(db, NewEventStore(db)), NewContactChannels(db), NewShowTell(db, nil))
	for i := 0; i < 7; i++ {
		seed(t, db, fmt.Sprintf("p%d", i), fmt.Sprintf("The Persona %d", i), "ready")
	}

	pack := closing.Pack()
	if len(pack.TopPersonas) != topPersonasLimit || len(pack.Leaderboard) != 7 {
		t.Errorf("top personas capped at %d, leaderboard full: %d/%d", topPersonasLimit, len(pack.TopPersonas), len(pack.Leaderboard))
	}
}

// ── the public event recap ─────────────────────────────────────────────────

func TestEventRecap_AvailableOnlyWhenClosed(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "aaa", "The Gopher", "ready")

	for _, phase := range []Phase{PhaseOnboarding, PhaseSocial, PhaseShowTell} {
		if phase != PhaseOnboarding {
			mustAdvance(t, deps.events, phase)
		}
		if resp := get(t, srv, "/recap"); resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET /recap during %s: want 404, got %d", phase, resp.StatusCode)
		}
	}
	mustAdvance(t, deps.events, PhaseClosed)
	if resp := get(t, srv, "/recap"); resp.StatusCode != http.StatusOK {
		t.Errorf("GET /recap when closed: want 200, got %d", resp.StatusCode)
	}
}

func TestEventRecap_ShowsPersonasOnly(t *testing.T) {
	srv, deps := closingSetup(t)
	closeEvent(t, deps)

	body := readBody(t, get(t, srv, "/recap"))
	// The content: the stats, the top connectors, the persona gallery and
	// the show & tell list with links.
	for _, want := range []string{
		"The evening, recapped",
		"Top connectors",
		"The Gopher", "The YAML Wrangler", "The Kernel Panicker",
		"Ships things",
		"Shipping fearless",
		"https://talks.dev/fearless",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the recap should show %q", want)
		}
	}
	// Personas only: no contact values, no identities.
	assertNoIdentities(t, body, deps.db)
}

func TestClosing_Recap(t *testing.T) {
	t.Run("the recap's content", func(t *testing.T) {
		_, deps := closingSetup(t)
		closeEvent(t, deps)

		recap := deps.handler.closing.Recap()
		if recap.Stats.Participants != 3 || recap.Stats.QuestionsAnswered != 1 {
			t.Errorf("the recap's stats: %+v", recap.Stats)
		}
		if len(recap.TopConnectors) != 3 || len(recap.Gallery) != 3 || len(recap.ShowTell) != 1 {
			t.Errorf("the recap's content: %+v", recap)
		}
	})
	t.Run("the top connectors are capped", func(t *testing.T) {
		db := newTestDB(t)
		closing := NewClosing(db, NewMeetings(db, NewEventStore(db)), NewContactChannels(db), NewShowTell(db, nil))
		for i := 0; i < 7; i++ {
			seed(t, db, fmt.Sprintf("p%d", i), fmt.Sprintf("The Persona %d", i), "ready")
		}

		recap := closing.Recap()
		if len(recap.TopConnectors) != topConnectorsLimit || len(recap.Gallery) != 7 {
			t.Errorf("top connectors capped at %d, gallery full: %d/%d", topConnectorsLimit, len(recap.TopConnectors), len(recap.Gallery))
		}
	})
}

// ── the host export pack: the endpoints ────────────────────────────────────

func TestAdminExport_JSON(t *testing.T) {
	srv, deps := closingSetup(t)
	closeEvent(t, deps)

	resp := get(t, srv, "/admin/export")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /admin/export: want 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: want application/json, got %q", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, ".json") {
		t.Errorf("Content-Disposition: got %q", cd)
	}
	body := readBody(t, resp)

	// The content: personas and counts.
	for _, want := range []string{
		"The Gopher", "The YAML Wrangler",
		"Both love tabs", "Why tabs?",
		"Shipping fearless",
		`"meetings": 1`,
		`"contact_exchanges": 3`,
		`"questions": 2`,
		`"questions_answered": 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the export should contain %q", want)
		}
	}
	var pack ExportPack
	if err := json.Unmarshal([]byte(body), &pack); err != nil {
		t.Fatalf("decoding the export: %v", err)
	}
	if pack.Stats.Participants != 3 || pack.Stats.Matched != 2 {
		t.Errorf("the export's stats: %+v", pack.Stats)
	}
	if len(pack.Leaderboard) != 3 || pack.Leaderboard[0].Rank != 1 || pack.Leaderboard[0].Name != "The Gopher" || pack.Leaderboard[0].Connections != 1 {
		t.Errorf("the export's leaderboard: %+v", pack.Leaderboard)
	}
	if len(pack.TopPersonas) != 3 || pack.TopPersonas[0].Tagline != "Ships things" {
		t.Errorf("the export's top personas: %+v", pack.TopPersonas)
	}
	if len(pack.BestMatches) != 1 || pack.BestMatches[0].Score != 91 ||
		pack.BestMatches[0].A.Name != "The Gopher" || pack.BestMatches[0].B.Name != "The YAML Wrangler" {
		t.Errorf("the export's best matches: %+v", pack.BestMatches)
	}
	if len(pack.ShowTell) != 1 || pack.ShowTell[0].Title != "Shipping fearless" || pack.ShowTell[0].Presenter.Name != "The YAML Wrangler" {
		t.Errorf("the export's show & tell: %+v", pack.ShowTell)
	}
	if pack.Success.Meetings != 1 || pack.Success.Connections != 2 || pack.Success.QuestionsAnswered != 1 || pack.Success.AudienceParticipation {
		t.Errorf("the export's success metrics: %+v", pack.Success)
	}

	// The privacy line: zero contact-channel values, zero identity mappings.
	assertNoIdentities(t, body, deps.db)
}

func TestAdminExport_Markdown(t *testing.T) {
	srv, deps := closingSetup(t)
	closeEvent(t, deps)

	resp := get(t, srv, "/admin/export?format=md")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /admin/export?format=md: want 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/markdown; charset=utf-8" {
		t.Errorf("Content-Type: want text/markdown, got %q", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, ".md") {
		t.Errorf("Content-Disposition: got %q", cd)
	}
	body := readBody(t, resp)

	for _, want := range []string{
		"# Agentic Blind Date — the evening, recapped",
		"## 🏆 Leaderboard",
		"The Gopher — 1 connection 👑",
		"## 💘 Best matches",
		"Both love tabs",
		"## 🎤 Show & tell",
		"Shipping fearless",
		"<https://talks.dev/fearless>",
		"not met yet",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the Markdown export should contain %q", want)
		}
	}

	// The privacy line: zero contact-channel values, zero identity mappings.
	assertNoIdentities(t, body, deps.db)
}

func TestAdminExport_UnknownFormatFallsBackToJSON(t *testing.T) {
	srv, deps := closingSetup(t)
	closeEvent(t, deps)

	resp := get(t, srv, "/admin/export?format=xml")
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("an unknown format falls back to JSON, got %q", ct)
	}
}

// ── the Markdown renderer ──────────────────────────────────────────────────

func TestRenderExportMarkdown(t *testing.T) {
	full := ExportPack{
		Stats:   EventStats{Participants: 4, Meetings: 2, ContactExchanges: 5, Questions: 12, QuestionsAnswered: 10},
		Success: SuccessMetrics{Meetings: 2, Connections: 4, QuestionsAnswered: 10, AudienceParticipation: true},
		Leaderboard: []ExportStanding{
			{Rank: 1, Name: "The Gopher", Symbol: "🐹", Color: "bg-amber-400", Connections: 2},
			{Rank: 2, Name: "The Crab", Symbol: "🦀", Color: "bg-red-400", Connections: 1},
		},
		TopPersonas: []ExportPersona{
			{Name: "The Gopher", Symbol: "🐹", Tagline: "Ships things"},
			{Name: "The Hermit", Symbol: "🐚"},
		},
		BestMatches: []ExportMatch{
			{Score: 91, Reason: "Both love tabs", Icebreakers: []string{"Why tabs?"},
				A: ExportPersona{Name: "The Gopher", Symbol: "🐹"}, B: ExportPersona{Name: "The Crab", Symbol: "🦀"}},
			{Score: 80, A: ExportPersona{Name: "The Hermit", Symbol: "🐚"}, B: ExportPersona{Name: "The Zebra", Symbol: "🦓"}},
		},
		ShowTell: []ExportTalk{
			{Title: "Shipping fearless", Description: "Friday deploys, live", RepoURL: "https://talks.dev/fearless", Status: SignupPresented,
				Presenter: ExportPersona{Name: "The YAML Wrangler", Symbol: "🐙"}},
			{Title: "Queued talk", Status: SignupQueued, Presenter: ExportPersona{Name: "The Hermit", Symbol: "🐚"}},
		},
	}
	md := renderExportMarkdown(full)
	for _, want := range []string{
		"# Agentic Blind Date — the evening, recapped",
		"4 participants · 2 completed meetings · 5 contact details exchanged · 12 questions asked (10 answered)",
		"- 🤝 2 completed meetings, 4 connections made",
		"- ❓ 10 questions answered — the audience-participation goal (10+) is met ✅",
		"1. 🐹 The Gopher — 2 connections 👑",
		"2. 🦀 The Crab — 1 connection\n",
		"- 🐹 **The Gopher** — Ships things",
		"- 🐚 **The Hermit**\n",
		"- 🐹 The Gopher 💘 🦀 The Crab — 91%",
		"  > Both love tabs",
		"  - Why tabs?",
		"- 🐚 The Hermit 💘 🦓 The Zebra — 80%",
		"- **Shipping fearless** — 🐙 The YAML Wrangler (presented)",
		"  Friday deploys, live",
		"  <https://talks.dev/fearless>",
		"- **Queued talk** — 🐚 The Hermit\n",
		"_Personas and counts only — no contact details, no handles._",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the Markdown should contain %q", want)
		}
	}

	// An empty pack renders its empty states.
	empty := renderExportMarkdown(ExportPack{})
	for _, want := range []string{
		"0 participants · 0 completed meetings · 0 contact details exchanged · 0 questions asked (0 answered)",
		"- 🤝 0 completed meetings, 0 connections made",
		"- ❓ 0 questions answered — the audience-participation goal (10+) is not met yet",
		"No connections were made.",
		"No participants.",
		"No matches were scored.",
		"No talks were signed up.",
	} {
		if !strings.Contains(empty, want) {
			t.Errorf("the empty Markdown should contain %q", want)
		}
	}
}

func TestPlural(t *testing.T) {
	if got := plural("connection", 1); got != "connection" {
		t.Errorf("one: got %q", got)
	}
	if got := plural("connection", 0); got != "connections" {
		t.Errorf("zero: got %q", got)
	}
	if got := plural("connection", 2); got != "connections" {
		t.Errorf("two: got %q", got)
	}
}

// ── the Big Screen's closing view ──────────────────────────────────────────

// closingPayload is the Big Screen's graph payload with the closing view's
// additions.
type closingPayload struct {
	screenPayload
	Stats    EventStats `json:"stats"`
	RecapURL string     `json:"recap_url"`
}

// closingGraphPayload fetches and decodes the graph payload with the closing
// view's additions.
func closingGraphPayload(t *testing.T, srv *testSrv) closingPayload {
	t.Helper()
	var p closingPayload
	if err := json.Unmarshal([]byte(graphPayloadBody(t, srv)), &p); err != nil {
		t.Fatalf("decoding the graph payload: %v", err)
	}
	return p
}

func TestGraphPayload_TheClosingView(t *testing.T) {
	srv, deps := closingSetup(t)

	// Before the close the payload carries no closing view.
	if body := graphPayloadBody(t, srv); strings.Contains(body, `"stats"`) || strings.Contains(body, `"recap_url"`) {
		t.Error("before the close the payload carries no closing view")
	}

	closeEvent(t, deps)
	p := closingGraphPayload(t, srv)

	// The evening's stats.
	want := EventStats{Participants: 3, Ready: 3, Matched: 2, Meetings: 1, ContactExchanges: 3, Questions: 2, QuestionsAnswered: 1}
	if p.Stats != want {
		t.Errorf("the closing stats: want %+v, got %+v", want, p.Stats)
	}
	// The recap QR's target.
	if p.RecapURL != "/recap" {
		t.Errorf("the recap QR target: got %q", p.RecapURL)
	}
	// The champion, named on the final leaderboard.
	if p.Champion == nil || p.Champion.ID != "aaa" || p.Champion.Connections != 1 {
		t.Errorf("the champion: got %+v", p.Champion)
	}
	// The final graph: the completed meeting is a meeting edge; the pending
	// one is not.
	if between := edgesBetween(p.screenPayload, "aaa", "ccc"); len(between) != 1 || !between[0].Meeting {
		t.Errorf("the completed meeting is a meeting edge: %+v", between)
	}
	for _, e := range edgesBetween(p.screenPayload, "aaa", "bbb") {
		if e.Meeting {
			t.Errorf("a pending meeting is not a meeting edge: %+v", e)
		}
	}

	// The screen carries the closing view's panels.
	body := readBody(t, get(t, srv, "/bigscreen"))
	for _, marker := range []string{`id="stats"`, `id="champion"`, "recap_url"} {
		if !strings.Contains(body, marker) {
			t.Errorf("the big screen should carry %s", marker)
		}
	}
}

// ── meeting confirmation is disabled when closed ───────────────────────────

func TestClosing_MeetingConfirmationIsDisabledWhenClosed(t *testing.T) {
	srv, deps := closingSetup(t)
	closeEvent(t, deps)

	// The confirm POST is rejected.
	bbb := reload(t, deps.db, "bbb")
	req, _ := http.NewRequest("POST", srv.URL+"/user/met/aaa", strings.NewReader(url.Values{"code": {bbb.MeetCode}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: cookieName, Value: "aaa"})
	client := srv.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("confirming a meeting when closed: want 400, got %d", resp.StatusCode)
	}
	if body := readBody(t, resp); !strings.Contains(body, "only open during the social and show & tell") {
		t.Errorf("the rejection should say why, got %q", body)
	}

	// The meet page shows the closed state, no confirm form.
	body := readBody(t, withCookie(t, srv, "/user/m/"+bbb.ID, "aaa"))
	if !strings.Contains(body, "Meeting confirmation is closed") {
		t.Error("the meet page should say confirmation is closed")
	}
	if strings.Contains(body, `hx-post="/user/met/`) {
		t.Error("the meet page must not offer the confirm form when closed")
	}

	// The personal page's meet section renders no confirmation UI at all.
	body = readBody(t, get(t, srv, "/user/meet-status/aaa"))
	if strings.Contains(body, "Your meet code") || strings.Contains(body, `name="code"`) || strings.Contains(body, "I met someone") {
		t.Error("the meet section must not offer meeting confirmation when closed")
	}
}
