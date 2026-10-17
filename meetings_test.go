package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// meetingSetup seeds two ready Participants and advances the event to the
// social, so meeting confirmation is open.
func meetingSetup(t *testing.T) (*DB, *EventStore, *Meetings, *Participant, *Participant) {
	t.Helper()
	db := newTestDB(t)
	events := NewEventStore(db)
	seed(t, db, "a", "The Gopher", "ready")
	seed(t, db, "b", "The Crab", "ready")
	if err := events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	return db, events, NewMeetings(db, events), reload(t, db, "a"), reload(t, db, "b")
}

func TestCanonicalPair_OrdersLikeTheMatchersPairKey(t *testing.T) {
	for _, tc := range []struct{ a, b, wantA, wantB string }{
		{"a", "b", "a", "b"},
		{"b", "a", "a", "b"},
		{"550e8400-e29b-41d4-a716-446655440001", "550e8400-e29b-41d4-a716-446655440000", "550e8400-e29b-41d4-a716-446655440000", "550e8400-e29b-41d4-a716-446655440001"},
	} {
		if gotA, gotB := canonicalPair(tc.a, tc.b); gotA != tc.wantA || gotB != tc.wantB {
			t.Errorf("canonicalPair(%q, %q): want (%q, %q), got (%q, %q)", tc.a, tc.b, tc.wantA, tc.wantB, gotA, gotB)
		}
	}
}

// TestPhase_MeetingConfirmationWindow pins when confirmation is open: during
// the social and show & tell phases, and not before them or once closed.
func TestPhase_MeetingConfirmationWindow(t *testing.T) {
	for _, tc := range []struct {
		phase Phase
		want  bool
	}{
		{PhaseOnboarding, false},
		{PhaseSocial, true},
		{PhaseShowTell, true},
		{PhaseClosed, false},
	} {
		if got := tc.phase.AllowsMeetings(); got != tc.want {
			t.Errorf("%s.AllowsMeetings(): want %v, got %v", tc.phase, tc.want, got)
		}
	}
}

// TestMeetings_ConfirmCompletesTheHandshake covers the two-sided confirm:
// the first side records one-sided (pending), the second side completes the
// meeting, the ticker announces it and both Participants earn a connection.
func TestMeetings_ConfirmCompletesTheHandshake(t *testing.T) {
	db, _, ms, a, b := meetingSetup(t)

	m, already, err := ms.Confirm(a, b, OriginOther)
	if err != nil {
		t.Fatal(err)
	}
	if already {
		t.Error("the first confirm is not a no-op")
	}
	if m.Completed() {
		t.Error("one side confirming is not a completed meeting")
	}
	if !m.ConfirmedBy("a") || m.ConfirmedBy("b") {
		t.Errorf("want only a's side confirmed: %+v", m)
	}
	if got := m.StatusFor("a"); got != MeetingPending {
		t.Errorf("a's status: want pending, got %s", got)
	}
	if got := m.StatusFor("b"); got != MeetingAwaiting {
		t.Errorf("b's status: want awaiting, got %s", got)
	}
	if m.A != "a" || m.B != "b" {
		t.Errorf("the canonical row: want (a, b), got (%s, %s)", m.A, m.B)
	}

	m2, already2, err := ms.Confirm(b, a, OriginOther)
	if err != nil {
		t.Fatal(err)
	}
	if already2 {
		t.Error("the second side's confirm is not a no-op")
	}
	if !m2.Completed() {
		t.Error("both sides confirming completes the meeting")
	}
	if got := m2.StatusFor("a"); got != MeetingCompleted {
		t.Errorf("a's status after completion: want completed, got %s", got)
	}

	// The ticker announced the completion.
	msgs, _ := db.GetRecentActivity(5)
	if want := "🤝 The Gopher and The Crab met!"; !strings.Contains(strings.Join(msgs, "\n"), want) {
		t.Errorf("the ticker should announce the meeting: want %q in %v", want, msgs)
	}
	// Connections derive from the completed meeting.
	if got := ms.Connections("a"); got != 1 {
		t.Errorf("a's connections: want 1, got %d", got)
	}
	if got := ms.Connections("b"); got != 1 {
		t.Errorf("b's connections: want 1, got %d", got)
	}
}

// TestMeetings_ConfirmIsIdempotentPerSide covers the friendly no-op: a second
// confirm from the same side changes nothing.
func TestMeetings_ConfirmIsIdempotentPerSide(t *testing.T) {
	_, _, ms, a, b := meetingSetup(t)

	m1, already1, err := ms.Confirm(a, b, OriginOther)
	if err != nil || already1 {
		t.Fatalf("first confirm: already=%v err=%v", already1, err)
	}
	m2, already2, err := ms.Confirm(a, b, OriginOther)
	if err != nil {
		t.Fatal(err)
	}
	if !already2 {
		t.Error("a second confirm from the same side is a no-op")
	}
	if !m2.AConfirmedAt.Equal(*m1.AConfirmedAt) {
		t.Errorf("the no-op changed the timestamp: %v vs %v", m2.AConfirmedAt, m1.AConfirmedAt)
	}
	if m2.Completed() {
		t.Error("the no-op completed the meeting")
	}

	// Completing after the no-op still works, and a confirm past completion
	// is a no-op too.
	m3, _, err := ms.Confirm(b, a, OriginOther)
	if err != nil || !m3.Completed() {
		t.Fatalf("the second side completes the meeting: err=%v", err)
	}
	if _, already4, err := ms.Confirm(a, b, OriginOther); err != nil || !already4 {
		t.Errorf("a confirm past completion is a no-op: already=%v err=%v", already4, err)
	}

	all, _ := ms.All()
	if len(all) != 1 {
		t.Errorf("want one canonical row per pair, got %d", len(all))
	}
}

func TestMeetings_SelfMeetingIsRejected(t *testing.T) {
	_, _, ms, a, _ := meetingSetup(t)

	if _, _, err := ms.Confirm(a, a, OriginOther); !errors.Is(err, ErrSelfMeeting) {
		t.Errorf("want ErrSelfMeeting, got %v", err)
	}
}

// TestMeetings_ConfirmationIsPhaseGated covers the phase gate: confirmation
// is rejected outside the social and show & tell phases.
func TestMeetings_ConfirmationIsPhaseGated(t *testing.T) {
	for _, phase := range phases {
		t.Run(string(phase), func(t *testing.T) {
			db := newTestDB(t)
			events := NewEventStore(db)
			seed(t, db, "a", "A", "ready")
			seed(t, db, "b", "B", "ready")
			if err := events.Advance(phase); err != nil {
				t.Fatal(err)
			}
			ms := NewMeetings(db, events)
			a, b := reload(t, db, "a"), reload(t, db, "b")

			_, _, err := ms.Confirm(a, b, OriginOther)
			if phase.AllowsMeetings() {
				if err != nil {
					t.Errorf("confirmation should be open in %s, got %v", phase, err)
				}
			} else if !errors.Is(err, ErrMeetingsClosed) {
				t.Errorf("confirmation should be rejected in %s with ErrMeetingsClosed, got %v", phase, err)
			}
		})
	}
}

// TestMeetings_OriginIsTaggedAutomatically covers the origin tagging from
// the initiator's perspective: a pair that are each other's current Match
// meets as match (symmetric); otherwise the initiator's origin stands
// (mission is not symmetric).
func TestMeetings_OriginIsTaggedAutomatically(t *testing.T) {
	db, _, ms, _, _ := meetingSetup(t)
	seed(t, db, "c", "The YAML Wrangler", "ready")
	seed(t, db, "d", "The Kernel Panicker", "ready")

	// A current Match meets as match, whoever initiates and whatever the
	// initiator's context says.
	pair(t, db, "a", "b")
	a, b := reload(t, db, "a"), reload(t, db, "b")
	if m, _, err := ms.Confirm(a, b, OriginOther); err != nil || m.Origin != OriginMatch {
		t.Errorf("a matched pair meets as match: origin=%s err=%v", m.Origin, err)
	}
	pair(t, db, "c", "d")
	c, d := reload(t, db, "c"), reload(t, db, "d")
	if m, _, err := ms.Confirm(d, c, OriginMission); err != nil || m.Origin != OriginMatch {
		t.Errorf("match is symmetric: origin=%s err=%v", m.Origin, err)
	}

	// Anyone else: the initiator's origin stands.
	seed(t, db, "e", "The Script Kiddie", "ready")
	e := reload(t, db, "e")
	if m, _, err := ms.Confirm(c, e, OriginMission); err != nil || m.Origin != OriginMission {
		t.Errorf("a mission meeting keeps its origin: origin=%s err=%v", m.Origin, err)
	}
	if m, _, err := ms.Confirm(e, a, OriginOther); err != nil || m.Origin != OriginOther {
		t.Errorf("anyone else meets as other: origin=%s err=%v", m.Origin, err)
	}
}

// TestMeetings_OneRowPerPairRegardlessOfWhoConfirmsFirst covers the canonical
// pair ordering: one row per pair, with the lexicographically lower id as
// participant_a, whichever side confirms first.
func TestMeetings_OneRowPerPairRegardlessOfWhoConfirmsFirst(t *testing.T) {
	_, _, ms, a, b := meetingSetup(t)

	if _, _, err := ms.Confirm(b, a, OriginOther); err != nil {
		t.Fatal(err)
	}
	all, err := ms.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].A != "a" || all[0].B != "b" {
		t.Fatalf("want one canonical row (a, b), got %+v", all)
	}
	if !all[0].ConfirmedBy("b") || all[0].ConfirmedBy("a") {
		t.Errorf("want only b's side confirmed: %+v", all[0])
	}

	if _, _, err := ms.Confirm(a, b, OriginOther); err != nil {
		t.Fatal(err)
	}
	all, _ = ms.All()
	if len(all) != 1 {
		t.Fatalf("the second side confirms the same row, got %d rows", len(all))
	}
	if !all[0].Completed() {
		t.Error("the meeting should be completed")
	}
}

func TestMeeting_Other(t *testing.T) {
	m := &Meeting{A: "a", B: "b"}
	if got := m.Other("a"); got != "b" {
		t.Errorf("Other(%q): want %q, got %q", "a", "b", got)
	}
	if got := m.Other("b"); got != "a" {
		t.Errorf("Other(%q): want %q, got %q", "b", "a", got)
	}
}

func TestMeetings_Between(t *testing.T) {
	_, _, ms, a, b := meetingSetup(t)

	if m, err := ms.Between("a", "b"); err != nil || m != nil {
		t.Errorf("no meeting yet: want (nil, nil), got (%v, %v)", m, err)
	}
	if _, _, err := ms.Confirm(a, b, OriginOther); err != nil {
		t.Fatal(err)
	}
	// The argument order does not matter.
	for _, pair := range [][2]string{{"a", "b"}, {"b", "a"}} {
		m, err := ms.Between(pair[0], pair[1])
		if err != nil || m == nil {
			t.Fatalf("Between(%q, %q): want the meeting, got (%v, %v)", pair[0], pair[1], m, err)
		}
		if m.A != "a" || m.B != "b" {
			t.Errorf("Between(%q, %q): want the canonical row (a, b), got (%s, %s)", pair[0], pair[1], m.A, m.B)
		}
	}
}

// TestMeetings_DossierUnlocked pins the dossier-unlock rule (ADR-0012): the
// pair's dossier unlocks once their Meeting is completed, or once the event
// is closed, after which the recap takes over. Every other state stays
// locked — including an unreadable database, which reads as locked, the
// safe direction.
func TestMeetings_DossierUnlocked(t *testing.T) {
	for _, tc := range []struct {
		name string
		// mate: whether the Participant is matched, so there is a
		// partner id to consult.
		mate  bool
		setup func(t *testing.T, db *DB, events *EventStore, ms *Meetings, a, b *Participant)
		want  bool
	}{
		{"a pending meeting stays locked", true, func(t *testing.T, db *DB, events *EventStore, ms *Meetings, a, b *Participant) {
			if _, _, err := ms.Confirm(a, b, OriginOther); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"a completed meeting unlocks", true, func(t *testing.T, db *DB, events *EventStore, ms *Meetings, a, b *Participant) {
			if _, _, err := ms.Confirm(a, b, OriginOther); err != nil {
				t.Fatal(err)
			}
			if _, _, err := ms.Confirm(b, a, OriginOther); err != nil {
				t.Fatal(err)
			}
		}, true},
		{"the closed event unlocks without a meeting", true, func(t *testing.T, db *DB, events *EventStore, ms *Meetings, a, b *Participant) {
			if err := events.Advance(PhaseClosed); err != nil {
				t.Fatal(err)
			}
		}, true},
		{"an unmatched participant stays locked", false, nil, false},
		{"an unreadable database reads as locked", true, func(t *testing.T, db *DB, events *EventStore, ms *Meetings, a, b *Participant) {
			breakDB(db)
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, events, ms, a, b := meetingSetup(t)
			if tc.setup != nil {
				tc.setup(t, db, events, ms, a, b)
			}
			partner := b.ID
			if !tc.mate {
				partner = ""
			}
			if got := ms.DossierUnlocked(a.ID, partner); got != tc.want {
				t.Errorf("DossierUnlocked(%q, %q): want %v, got %v", a.ID, partner, tc.want, got)
			}
		})
	}
}

func TestMeetings_ForParticipantAndConnections(t *testing.T) {
	db, _, ms, a, b := meetingSetup(t)
	seed(t, db, "c", "The YAML Wrangler", "ready")
	c := reload(t, db, "c")

	if _, _, err := ms.Confirm(a, b, OriginOther); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ms.Confirm(a, c, OriginOther); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ms.Confirm(b, c, OriginOther); err != nil {
		t.Fatal(err)
	}

	for id, want := range map[string]int{"a": 2, "b": 2, "c": 2} {
		ms, err := ms.ForParticipant(id)
		if err != nil {
			t.Fatal(err)
		}
		if len(ms) != want {
			t.Errorf("%s's meetings: want %d, got %d", id, want, len(ms))
		}
	}
	if ms, _ := ms.ForParticipant("nobody"); len(ms) != 0 {
		t.Errorf("an unknown Participant has no meetings, got %d", len(ms))
	}

	// Connections count only completed meetings.
	if got := ms.Connections("a"); got != 0 {
		t.Errorf("a's connections before any completion: want 0, got %d", got)
	}
	if _, _, err := ms.Confirm(b, a, OriginOther); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ms.Confirm(c, a, OriginOther); err != nil {
		t.Fatal(err)
	}
	if got := ms.Connections("a"); got != 2 {
		t.Errorf("a's connections after two completions: want 2, got %d", got)
	}
	if got := ms.Connections("b"); got != 1 {
		t.Errorf("b's connections: want 1, got %d", got)
	}
}

func TestMeetings_RemoveParticipant(t *testing.T) {
	_, _, ms, a, b := meetingSetup(t)
	if _, _, err := ms.Confirm(a, b, OriginOther); err != nil {
		t.Fatal(err)
	}

	if err := ms.RemoveParticipant("a"); err != nil {
		t.Fatal(err)
	}
	if all, _ := ms.All(); len(all) != 0 {
		t.Errorf("a Participant's meetings are removed with them, got %d", len(all))
	}
	if ms, _ := ms.ForParticipant("b"); len(ms) != 0 {
		t.Errorf("b's meetings with a are gone, got %d", len(ms))
	}
}

// TestMeetings_RemoveParticipantAnnouncesOnTheChangeFeed covers the push: a
// Participant leaving takes their meetings out of every live view, so the
// removal announces on the change feed like any other write.
func TestMeetings_RemoveParticipantAnnouncesOnTheChangeFeed(t *testing.T) {
	_, _, ms, a, b := meetingSetup(t)
	if _, _, err := ms.Confirm(a, b, OriginOther); err != nil {
		t.Fatal(err)
	}
	changes, stop := ms.db.Subscribe()
	defer stop()

	if err := ms.RemoveParticipant(a.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changes:
	case <-time.After(time.Second):
		t.Error("removing a Participant should announce on the change feed")
	}
}

// TestMeetings_ResetClearsMeetingsSilently covers the reset: one-sided
// confirmations expire silently — no message, no nag.
func TestMeetings_ResetClearsMeetingsSilently(t *testing.T) {
	db, events, ms, a, b := meetingSetup(t)
	if _, _, err := ms.Confirm(a, b, OriginOther); err != nil {
		t.Fatal(err)
	}

	if err := events.Reset(); err != nil {
		t.Fatal(err)
	}
	if all, _ := ms.All(); len(all) != 0 {
		t.Errorf("a reset clears the meetings, got %d", len(all))
	}
	msgs, _ := db.GetRecentActivity(50)
	for _, m := range msgs {
		if strings.Contains(m, "met") {
			t.Errorf("the reset should clear meetings silently, logged %q", m)
		}
	}
}

// TestMeetings_ConfirmAnnouncesOnTheChangeFeed covers the push: a confirm
// announces on the change feed, so phones and the big screen update via the
// existing SSE streams.
func TestMeetings_ConfirmAnnouncesOnTheChangeFeed(t *testing.T) {
	_, _, ms, a, b := meetingSetup(t)
	changes, stop := ms.db.Subscribe()
	defer stop()

	if _, _, err := ms.Confirm(a, b, OriginOther); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changes:
	case <-time.After(time.Second):
		t.Error("a confirm should announce on the change feed")
	}
}

// TestMeetings_ConfirmReportsWriteFailures covers the handshake's write
// failures: each write fails as a genuine database error, and a failure rolls
// the whole confirm back.
func TestMeetings_ConfirmReportsWriteFailures(t *testing.T) {
	t.Run("creating the row", func(t *testing.T) {
		db, _, ms, a, b := meetingSetup(t)
		failWrites(t, db, "meet_insert")
		if _, _, err := ms.Confirm(a, b, OriginOther); err == nil {
			t.Error("want a failure")
		}
		if all, _ := ms.All(); len(all) != 0 {
			t.Errorf("the failed confirm wrote a row: %d", len(all))
		}
	})
	t.Run("the second side", func(t *testing.T) {
		db, _, ms, a, b := meetingSetup(t)
		if _, _, err := ms.Confirm(a, b, OriginOther); err != nil {
			t.Fatal(err)
		}
		failWrites(t, db, "meet_update")
		if _, _, err := ms.Confirm(b, a, OriginOther); err == nil {
			t.Error("want a failure")
		}
		if m, _ := ms.Between("a", "b"); m.Completed() || m.ConfirmedBy("b") {
			t.Errorf("the failed confirm was not rolled back: %+v", m)
		}
	})
	t.Run("the completion", func(t *testing.T) {
		db, _, ms, a, b := meetingSetup(t)
		if _, _, err := ms.Confirm(a, b, OriginOther); err != nil {
			t.Fatal(err)
		}
		failWrites(t, db, "meet_complete")
		if _, _, err := ms.Confirm(b, a, OriginOther); err == nil {
			t.Error("want a failure")
		}
		if m, _ := ms.Between("a", "b"); m.Completed() {
			t.Error("the failed completion was not rolled back")
		}
		// The ticker stays silent for a meeting that did not complete.
		msgs, _ := db.GetRecentActivity(50)
		if strings.Contains(strings.Join(msgs, "\n"), "met!") {
			t.Errorf("a failed completion should not be announced: %v", msgs)
		}
	})
}

// TestMeetings_ConfirmReportsAReadFailure covers the handshake's read: a
// failure reading the pair's row fails the confirm.
func TestMeetings_ConfirmReportsAReadFailure(t *testing.T) {
	db, _, ms, a, b := meetingSetup(t)
	if _, _, err := ms.Confirm(a, b, OriginOther); err != nil {
		t.Fatal(err)
	}
	breakOnFault(t, db, "meet_read")

	if _, _, err := ms.Confirm(b, a, OriginOther); err == nil {
		t.Error("want a failure when the meeting cannot be read")
	}
}

// TestMeetings_ReadsReportAScanFailure covers the meetings reads against
// rows that cannot be scanned.
func TestMeetings_ReadsReportAScanFailure(t *testing.T) {
	db, _, ms, a, b := meetingSetup(t)
	if _, _, err := ms.Confirm(a, b, OriginOther); err != nil {
		t.Fatal(err)
	}
	breakOnFault(t, db, "meet_read")

	if _, err := ms.All(); err == nil {
		t.Error("All: want an error")
	}
	if _, err := ms.ForParticipant("a"); err == nil {
		t.Error("ForParticipant: want an error")
	}
	if _, err := ms.Between("a", "b"); err == nil {
		t.Error("Between: want an error")
	}
}

// TestMeetings_ReadsReportABrokenDatabase covers the meetings reads against
// a closed database.
func TestMeetings_ReadsReportABrokenDatabase(t *testing.T) {
	db := newTestDB(t)
	breakDB(db)
	ms := NewMeetings(db, NewEventStore(db))

	if _, err := ms.All(); err == nil {
		t.Error("All: want an error")
	}
	if _, err := ms.ForParticipant("a"); err == nil {
		t.Error("ForParticipant: want an error")
	}
	if _, err := ms.Between("a", "b"); err == nil {
		t.Error("Between: want an error")
	}
	if got := ms.Connections("a"); got != 0 {
		t.Errorf("Connections against a broken database counts as 0, got %d", got)
	}
}

// postWithCookie posts a form with the Participant cookie set.
// postWithCookie posts the form with the cookie set; the body is closed through the t.Cleanup below.
//
//bodyclose:handled
func postWithCookie(t *testing.T, srv *testSrv, path string, form url.Values, id string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: cookieName, Value: id}) //nolint:gosec // G124: an outgoing test request, no browser and no attributes to set
	client := srv.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// waitForCount waits until the stream contains substr at least n times.
func waitForCount(t *testing.T, s *liveStream, substr string, n int) {
	t.Helper()
	eventually(t, fmt.Sprintf("stream to contain %d × %q", n, substr), func() bool {
		return strings.Count(s.String(), substr) >= n
	})
}

func TestMeetPage_ShowsTheTargetPersonaCard(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The YAML Wrangler", "ready")
	must(t, deps.events.Reveal())

	resp := get(t, srv, "/user/m/b")
	if resp.StatusCode != 200 {
		t.Fatalf("GET /user/m/b: want 200, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	symbol := reload(t, deps.db, "b").PersonaSymbol
	for _, want := range []string{"You found", "The YAML Wrangler", symbol} {
		if !strings.Contains(body, want) {
			t.Errorf("the meet page should show %q", want)
		}
	}
	// Without a cookie, the page offers entering one's own meet code.
	if !strings.Contains(body, `name="me"`) {
		t.Error("without a cookie the meet page should offer entering one's own meet code")
	}
}

func TestMeetPage_UnknownTargetIs404(t *testing.T) {
	srv, _ := newTestServer(t, nil, nil)
	if resp := get(t, srv, "/user/m/nobody"); resp.StatusCode != 404 {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

// TestMeetPage_MeParameterResolvesTheConfirmer covers the cookie-less flow:
// entering one's own meet code resolves the Participant and sets the cookie.
func TestMeetPage_MeParameterResolvesTheConfirmer(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The YAML Wrangler", "ready")
	must(t, deps.events.Reveal())
	code := reload(t, deps.db, "a").MeetCode

	resp := get(t, srv, "/user/m/b?me="+strings.ToLower(code))
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("want 303, got %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/user/m/b" {
		t.Errorf("Location: want /user/m/b, got %q", loc)
	}
	cookies := resp.Cookies()
	if len(cookies) != 1 || cookies[0].Name != cookieName || cookies[0].Value != "a" {
		t.Errorf("the participant cookie should be set to the resolved Participant, got %v", cookies)
	}
}

func TestMeetPage_MeParameterWithUnknownCodeIsRejected(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "b", "The YAML Wrangler", "ready")

	if resp := get(t, srv, "/user/m/b?me=ZZZZZZ"); resp.StatusCode != 400 {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

func TestMeetPage_WithCookieShowsTheConfirmButton(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The YAML Wrangler", "ready")
	must(t, deps.events.Reveal())
	targetCode := reload(t, deps.db, "b").MeetCode

	resp := withCookie(t, srv, "/user/m/b", "a")
	body := readBody(t, resp)
	if !strings.Contains(body, `hx-post="/user/met/a"`) {
		t.Error("with a cookie the meet page should show the confirm button")
	}
	if !strings.Contains(body, `value="`+targetCode+`"`) {
		t.Error("the confirm form should carry the target's meet code")
	}
	if !strings.Contains(body, "You are") {
		t.Error("the meet page should say who you are")
	}
}

// TestMeetPage_ConfirmDialogDemandsAttention guards the confirm dialog's
// treatment: it pops continuously, stands out in emerald against the app's
// indigo actions, and respects reduced motion.
func TestMeetPage_ConfirmDialogDemandsAttention(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The YAML Wrangler", "ready")
	must(t, deps.events.Reveal())

	body := readBody(t, withCookie(t, srv, "/user/m/b", "a"))
	for _, want := range []string{
		"meet-pulse",             // the continuous pop
		"prefers-reduced-motion", // motion is opt-out
		"bg-emerald-600",         // the confirm action's own color
		"border-emerald-600/50",  // the dialog's color difference
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the confirm dialog should carry %q", want)
		}
	}
	if strings.Contains(body, "bg-indigo-600") {
		t.Error("the confirm dialog should not use the app's default indigo button")
	}
}

func TestMeetPage_SelfMeetIsFriendly(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	must(t, deps.events.Reveal())

	body := readBody(t, withCookie(t, srv, "/user/m/a", "a"))
	if !strings.Contains(body, "That's you") {
		t.Error("meeting yourself should be a friendly message")
	}
	if strings.Contains(body, `hx-post="/user/met/a"`) {
		t.Error("meeting yourself should not show the confirm button")
	}
}

func TestMeetPage_AlreadyConfirmedShowsTheNoOp(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The YAML Wrangler", "ready")
	must(t, deps.events.Reveal())
	ms := NewMeetings(deps.db, deps.events)
	if _, _, err := ms.Confirm(reload(t, deps.db, "a"), reload(t, deps.db, "b"), OriginOther); err != nil {
		t.Fatal(err)
	}

	body := readBody(t, withCookie(t, srv, "/user/m/b", "a"))
	if !strings.Contains(body, "already confirmed") {
		t.Error("a confirmed meeting should show its state")
	}
	if strings.Contains(body, `hx-post="/user/met/a"`) {
		t.Error("an already-confirmed meeting should not show the confirm button again")
	}
}

func TestMeetPage_WhenClosedShowsNoConfirmButton(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The YAML Wrangler", "ready")
	if err := deps.events.Advance(PhaseClosed); err != nil {
		t.Fatal(err)
	}

	body := readBody(t, withCookie(t, srv, "/user/m/b", "a"))
	if !strings.Contains(body, "closed for now") {
		t.Error("a closed event should say confirmation is closed")
	}
	if strings.Contains(body, `hx-post="/user/met/a"`) {
		t.Error("a closed event should not show the confirm button")
	}
}

// TestConfirmMeeting_TwoPhonesCompleteTheHandshake is #114's acceptance test:
// two phones complete a handshake end-to-end through the routes — one scans
// the other's QR (the meet page), the other types their 6-character code.
func TestConfirmMeeting_TwoPhonesCompleteTheHandshake(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The YAML Wrangler", "ready")
	must(t, deps.events.Reveal())
	aCode := reload(t, deps.db, "a").MeetCode
	bCode := reload(t, deps.db, "b").MeetCode

	// Phone A scans phone B's QR: the native camera opens B's meet link.
	if resp := withCookie(t, srv, "/user/m/b", "a"); resp.StatusCode != 200 {
		t.Fatalf("A opens B's meet page: want 200, got %d", resp.StatusCode)
	}
	// A confirms: their side of the meeting is recorded.
	resp := postWithCookie(t, srv, "/user/met/a", url.Values{"code": {bCode}}, "a")
	if resp.StatusCode != 200 {
		t.Fatalf("A confirms: want 200, got %d", resp.StatusCode)
	}
	if body := readBody(t, resp); !strings.Contains(body, "still has to confirm") {
		t.Errorf("A's confirm should show the one-sided state, got: %s", body)
	}

	// Phone B opens "I met someone" on their personal page and types A's code.
	resp = postWithCookie(t, srv, "/user/met/b", url.Values{"code": {strings.ToLower(aCode)}}, "b")
	if resp.StatusCode != 200 {
		t.Fatalf("B confirms: want 200, got %d", resp.StatusCode)
	}
	if body := readBody(t, resp); !strings.Contains(body, "You both met") {
		t.Errorf("B's confirm should complete the meeting, got: %s", body)
	}

	// The meeting is completed, the ticker announced it, both earned a connection.
	ms := NewMeetings(deps.db, deps.events)
	m, err := ms.Between("a", "b")
	if err != nil || m == nil || !m.Completed() {
		t.Fatalf("the handshake completed: %v %v", m, err)
	}
	msgs, _ := deps.db.GetRecentActivity(5)
	if !strings.Contains(strings.Join(msgs, "\n"), "🤝 The Gopher and The YAML Wrangler met!") {
		t.Errorf("the ticker should announce the completed handshake, got %v", msgs)
	}
	if got := ms.Connections("a"); got != 1 {
		t.Errorf("a's connections: want 1, got %d", got)
	}
	if got := ms.Connections("b"); got != 1 {
		t.Errorf("b's connections: want 1, got %d", got)
	}

	// And /data/meetings shows it.
	body := readBody(t, get(t, srv, "/data/meetings"))
	var meetings []map[string]any
	if err := json.Unmarshal([]byte(body), &meetings); err != nil {
		t.Fatalf("GET /data/meetings: %v — body: %s", err, body)
	}
	if len(meetings) != 1 || meetings[0]["participant_a"] != "a" || meetings[0]["participant_b"] != "b" {
		t.Errorf("/data/meetings should show the canonical row, got %s", body)
	}
	if meetings[0]["completed_at"] == nil {
		t.Errorf("the completed meeting should carry completed_at, got %s", body)
	}
}

func TestConfirmMeeting_RejectsUnknownCodes(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The YAML Wrangler", "ready")
	must(t, deps.events.Reveal())

	resp := postWithCookie(t, srv, "/user/met/a", url.Values{"code": {"ZZZZZZ"}}, "a")
	if resp.StatusCode != 400 {
		t.Errorf("an unknown code: want 400, got %d", resp.StatusCode)
	}
	if body := readBody(t, resp); !strings.Contains(body, "doesn't belong to anyone") {
		t.Errorf("an unknown code should be rejected with a clear message, got: %s", body)
	}
}

func TestConfirmMeeting_RejectsSelfEntries(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	must(t, deps.events.Reveal())
	code := reload(t, deps.db, "a").MeetCode

	resp := postWithCookie(t, srv, "/user/met/a", url.Values{"code": {code}}, "a")
	if resp.StatusCode != 400 {
		t.Errorf("a self-entry: want 400, got %d", resp.StatusCode)
	}
	if body := readBody(t, resp); !strings.Contains(body, "can't meet yourself") {
		t.Errorf("a self-entry should be rejected with a clear message, got: %s", body)
	}
}

// TestConfirmMeeting_TheConfirmerIsIdentifiedByTheCookie covers the identity
// rule: without the participant cookie, the confirm is rejected.
func TestConfirmMeeting_TheConfirmerIsIdentifiedByTheCookie(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The YAML Wrangler", "ready")
	must(t, deps.events.Reveal())
	bCode := reload(t, deps.db, "b").MeetCode

	// No cookie at all.
	if resp := post(t, srv, "/user/met/a", url.Values{"code": {bCode}}); resp.StatusCode != 400 {
		t.Errorf("without a cookie: want 400, got %d", resp.StatusCode)
	}
	// A cookie naming someone else.
	if resp := postWithCookie(t, srv, "/user/met/a", url.Values{"code": {bCode}}, "b"); resp.StatusCode != 400 {
		t.Errorf("with a mismatched cookie: want 400, got %d", resp.StatusCode)
	}
	// Nothing was recorded.
	if all, _ := NewMeetings(deps.db, deps.events).All(); len(all) != 0 {
		t.Errorf("a rejected confirm records nothing, got %d meetings", len(all))
	}
}

func TestConfirmMeeting_UnknownConfirmerIs404(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "b", "The YAML Wrangler", "ready")
	must(t, deps.events.Reveal())
	bCode := reload(t, deps.db, "b").MeetCode

	if resp := postWithCookie(t, srv, "/user/met/nobody", url.Values{"code": {bCode}}, "nobody"); resp.StatusCode != 404 {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

// TestConfirmMeeting_RejectedOutsideSocialAndShowTell covers the phase gate
// through the route.
func TestConfirmMeeting_RejectedOutsideSocialAndShowTell(t *testing.T) {
	for _, tc := range []struct {
		name  string
		phase Phase
	}{
		{"onboarding", PhaseOnboarding},
		{"closed", PhaseClosed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, deps := newTestServer(t, nil, nil)
			seed(t, deps.db, "a", "The Gopher", "ready")
			seed(t, deps.db, "b", "The YAML Wrangler", "ready")
			if err := deps.events.Advance(tc.phase); err != nil {
				t.Fatal(err)
			}
			bCode := reload(t, deps.db, "b").MeetCode

			resp := postWithCookie(t, srv, "/user/met/a", url.Values{"code": {bCode}}, "a")
			if resp.StatusCode != 400 {
				t.Errorf("confirmation in %s: want 400, got %d", tc.phase, resp.StatusCode)
			}
			if body := readBody(t, resp); !strings.Contains(body, "only open during the social") {
				t.Errorf("want a clear message, got: %s", body)
			}
		})
	}
}

// TestConfirmMeeting_SecondConfirmIsAFriendlyNoOp covers the friendly no-op
// through the route: "You already met 🐙!".
func TestConfirmMeeting_SecondConfirmIsAFriendlyNoOp(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The YAML Wrangler", "ready")
	must(t, deps.events.Reveal())
	bCode := reload(t, deps.db, "b").MeetCode

	if resp := postWithCookie(t, srv, "/user/met/a", url.Values{"code": {bCode}}, "a"); resp.StatusCode != 200 {
		t.Fatalf("first confirm: got %d", resp.StatusCode)
	}
	resp := postWithCookie(t, srv, "/user/met/a", url.Values{"code": {bCode}}, "a")
	if resp.StatusCode != 200 {
		t.Fatalf("second confirm: want 200, got %d", resp.StatusCode)
	}
	if body := readBody(t, resp); !strings.Contains(body, "You already met") {
		t.Errorf("a second confirm is a friendly no-op, got: %s", body)
	}
}

func TestConfirmMeeting_ReportsAWriteFailure(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The YAML Wrangler", "ready")
	must(t, deps.events.Reveal())
	failWrites(t, deps.db, "meet_insert")
	bCode := reload(t, deps.db, "b").MeetCode

	if resp := postWithCookie(t, srv, "/user/met/a", url.Values{"code": {bCode}}, "a"); resp.StatusCode != 500 {
		t.Errorf("want 500, got %d", resp.StatusCode)
	}
}

// TestMeetStatus_IsPhaseGatedAndShowsTheSection covers the personal page's
// meet section: the meet code, the meet-link QR and the "I met someone" entry
// point during the social and show & tell phases — and hidden otherwise.
func TestMeetStatus_IsPhaseGatedAndShowsTheSection(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	code := reload(t, deps.db, "a").MeetCode

	// Onboarding: the section is gated off, but the refresh hook stays.
	body := readBody(t, get(t, srv, "/user/meet-status/a"))
	if !strings.Contains(body, `id="meet-status"`) || !strings.Contains(body, `hx-get="/user/meet-status/a"`) {
		t.Errorf("the gated-off section should keep its refresh hook, got: %s", body)
	}
	if strings.Contains(body, code) {
		t.Error("during onboarding the meet section is hidden")
	}

	must(t, deps.events.Reveal())
	body = readBody(t, get(t, srv, "/user/meet-status/a"))
	for _, want := range []string{code, `src="/qr?data=/user/m/a&size=150"`, `hx-post="/user/met/a"`, "I met someone", "0 connection"} {
		if !strings.Contains(body, want) {
			t.Errorf("the meet section should show %q", want)
		}
	}

	// Show & tell keeps it open; closed hides it again.
	if err := deps.events.Advance(PhaseShowTell); err != nil {
		t.Fatal(err)
	}
	if body := readBody(t, get(t, srv, "/user/meet-status/a")); !strings.Contains(body, code) {
		t.Error("during show & tell the meet section stays open")
	}
	if err := deps.events.Advance(PhaseClosed); err != nil {
		t.Fatal(err)
	}
	if body := readBody(t, get(t, srv, "/user/meet-status/a")); strings.Contains(body, code) {
		t.Error("when closed the meet section is hidden")
	}
}

func TestMeetStatus_UnknownParticipantIs404(t *testing.T) {
	srv, _ := newTestServer(t, nil, nil)
	if resp := get(t, srv, "/user/meet-status/nobody"); resp.StatusCode != 404 {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

// TestMeetStatus_ListsMeetingsWithTheirStatus covers the personal page's
// meetings list: one-sided confirmations show as pending to the initiator,
// and the other side is offered a confirm-back.
func TestMeetStatus_ListsMeetingsWithTheirStatus(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The YAML Wrangler", "ready")
	seed(t, deps.db, "c", "The Kernel Panicker", "ready")
	must(t, deps.events.Reveal())
	ms := NewMeetings(deps.db, deps.events)
	// a confirmed meeting b (pending for a), c confirmed meeting a (awaiting for a).
	if _, _, err := ms.Confirm(reload(t, deps.db, "a"), reload(t, deps.db, "b"), OriginOther); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ms.Confirm(reload(t, deps.db, "c"), reload(t, deps.db, "a"), OriginOther); err != nil {
		t.Fatal(err)
	}

	body := readBody(t, get(t, srv, "/user/meet-status/a"))
	for _, want := range []string{"The YAML Wrangler", "Pending", "The Kernel Panicker", "confirm back"} {
		if !strings.Contains(body, want) {
			t.Errorf("a's meet section should show %q, got: %s", want, body)
		}
	}
	// The confirm-back form carries the other side's meet code.
	cCode := reload(t, deps.db, "c").MeetCode
	if !strings.Contains(body, `value="`+cCode+`"`) {
		t.Error("the confirm-back form should carry the other side's meet code")
	}
	// Completing the handshake updates the list through the same fragment.
	if _, _, err := ms.Confirm(reload(t, deps.db, "b"), reload(t, deps.db, "a"), OriginOther); err != nil {
		t.Fatal(err)
	}
	body = readBody(t, get(t, srv, "/user/meet-status/a"))
	if !strings.Contains(body, "You met!") {
		t.Errorf("a completed meeting shows as met, got: %s", body)
	}
	if !strings.Contains(body, "1 connection") {
		t.Errorf("the derived connection count shows, got: %s", body)
	}
	// The other side sees the same meeting from their perspective.
	body = readBody(t, get(t, srv, "/user/meet-status/b"))
	if !strings.Contains(body, "The Gopher") || !strings.Contains(body, "You met!") {
		t.Errorf("b's meet section should show the completed meeting with a, got: %s", body)
	}
}

// TestMeetStatus_SkipsMeetingsWhoseCounterpartLeft covers a meeting whose
// other side was deleted: the row stays (it is debug data) but the list skips it.
func TestMeetStatus_SkipsMeetingsWhoseCounterpartLeft(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The YAML Wrangler", "ready")
	must(t, deps.events.Reveal())
	ms := NewMeetings(deps.db, deps.events)
	if _, _, err := ms.Confirm(reload(t, deps.db, "a"), reload(t, deps.db, "b"), OriginOther); err != nil {
		t.Fatal(err)
	}
	// The Relationships module deletes b without touching the meetings —
	// deliberately invalid state, written through the module the handler uses
	// for a delete that does not go through this ticket's route.
	if err := NewRelationships(deps.db).Remove("b"); err != nil {
		t.Fatal(err)
	}

	body := readBody(t, get(t, srv, "/user/meet-status/a"))
	if strings.Contains(body, "The YAML Wrangler") {
		t.Errorf("a meeting whose counterpart left is skipped, got: %s", body)
	}
	if all, _ := ms.All(); len(all) != 1 {
		t.Errorf("the row itself stays for the debug endpoint, got %d", len(all))
	}
}

// TestMatchPage_IncludesTheMeetSection covers the personal page's section on
// the match page.
func TestMatchPage_IncludesTheMeetSection(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The YAML Wrangler", "ready")
	pair(t, deps.db, "a", "b")
	must(t, deps.events.Reveal())
	code := reload(t, deps.db, "a").MeetCode

	body := readBody(t, get(t, srv, "/user/match/a"))
	if !strings.Contains(body, `id="meet-status"`) || !strings.Contains(body, code) {
		t.Error("the match page should include the meet section")
	}
	if !strings.Contains(body, "/user/match-stream/a") {
		t.Error("the match page should listen on its stream")
	}
}

// TestWaitPage_IncludesTheMeetSection covers the section on the wait page, so
// Participants who are not matched yet can meet people too.
func TestWaitPage_IncludesTheMeetSection(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	must(t, deps.events.Reveal())
	code := reload(t, deps.db, "a").MeetCode

	body := readBody(t, get(t, srv, "/user/wait/a"))
	if !strings.Contains(body, `id="meet-status"`) || !strings.Contains(body, code) {
		t.Error("the wait page should include the meet section during the social")
	}
}

// TestMatchStream_RefreshesWhenTheMeetingsChange covers the push to the
// personal page: the meet section refreshes when the Participant's meetings
// or the phase change — and stays quiet otherwise.
func TestMatchStream_RefreshesWhenTheMeetingsChange(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The YAML Wrangler", "ready")
	must(t, deps.events.Reveal())
	ms := NewMeetings(deps.db, deps.events)

	s := open(t, srv, "/user/match-stream/a")
	waitFor(t, s, "event: refresh") // the sync on connect
	before := s.String()
	sentNothingNew(t, s, 1, before, "nothing changed")

	// A meeting appears: the initiator's phone refreshes.
	if _, _, err := ms.Confirm(reload(t, deps.db, "a"), reload(t, deps.db, "b"), OriginOther); err != nil {
		t.Fatal(err)
	}
	waitForCount(t, s, "event: refresh", strings.Count(before, "event: refresh")+1)

	// The completion: both phones celebrate.
	before = s.String()
	if _, _, err := ms.Confirm(reload(t, deps.db, "b"), reload(t, deps.db, "a"), OriginOther); err != nil {
		t.Fatal(err)
	}
	waitForCount(t, s, "event: refresh", strings.Count(before, "event: refresh")+1)

	// A change that does not concern this Participant's meetings: quiet.
	before = s.String()
	deps.db.LogActivity("someone else did something")
	sentNothingNew(t, s, 3, before, "a change that does not concern this Participant")

	// The phase closing hides the section: refresh again.
	if err := deps.events.Advance(PhaseClosed); err != nil {
		t.Fatal(err)
	}
	waitForCount(t, s, "event: refresh", strings.Count(before, "event: refresh")+1)
}

// TestWaitStream_RefreshesTheMeetSection covers the wait page's push: the
// section appears when the social begins.
func TestWaitStream_RefreshesTheMeetSection(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")

	s := open(t, srv, "/user/wait-stream/a")
	waitFor(t, s, "event: refresh") // the sync on connect
	before := s.String()
	sentNothingNew(t, s, 1, before, "during onboarding")

	post(t, srv, "/admin/reveal", nil)
	waitForCount(t, s, "event: refresh", strings.Count(before, "event: refresh")+1)
}

func TestMatchStream_UnknownParticipantIs404AndEndsWhenRemoved(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")

	s := open(t, srv, "/user/match-stream/nobody")
	eventually(t, "the stream to end", s.ended)
	if s.status != 404 {
		t.Errorf("want 404, got %d", s.status)
	}

	s = open(t, srv, "/user/match-stream/a")
	// Wait until the stream is established — its connect sync proves the
	// first check ran — only then can the removal be observed by an
	// in-watch check (and not by the handler's initial lookup).
	waitFor(t, s, "event: refresh") // the sync on connect
	before := s.String()
	sentNothingNew(t, s, 1, before, "while the stream settles")
	if err := NewRelationships(deps.db).Remove("a"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the stream to end when the Participant is removed", s.ended)
}

func TestMatchStream_NeedsAWriterThatCanStream(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")

	rec := httptest.NewRecorder()
	srv.h.ServeHTTP(plainWriter{rec}, httptest.NewRequest("GET", "/user/match-stream/a", nil))
	if rec.Code != 500 {
		t.Errorf("want 500 without streaming support, got %d", rec.Code)
	}
}

func TestDataMeetings_EmptyAndBrokenDatabase(t *testing.T) {
	srv, _ := newTestServer(t, nil, nil)

	body := readBody(t, get(t, srv, "/data/meetings"))
	if trimmed := strings.TrimSpace(body); trimmed != "null" && trimmed != "[]" {
		t.Errorf("expected an empty JSON list, got %s", trimmed)
	}

	srv, deps := newTestServer(t, nil, nil)
	breakDB(deps.db)
	if resp := get(t, srv, "/data/meetings"); resp.StatusCode != 500 {
		t.Errorf("a broken database: want 500, got %d", resp.StatusCode)
	}
}

// TestReset_ClearsTheMeetings covers the reset through the route.
func TestReset_ClearsTheMeetings(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The YAML Wrangler", "ready")
	must(t, deps.events.Reveal())
	ms := NewMeetings(deps.db, deps.events)
	if _, _, err := ms.Confirm(reload(t, deps.db, "a"), reload(t, deps.db, "b"), OriginOther); err != nil {
		t.Fatal(err)
	}

	if resp := post(t, srv, "/admin/reset", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /admin/reset: want 303, got %d", resp.StatusCode)
	}
	if all, _ := ms.All(); len(all) != 0 {
		t.Errorf("a reset clears the meetings, got %d", len(all))
	}
}

func TestReset_ReportsAMeetingsFailure(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The YAML Wrangler", "ready")
	must(t, deps.events.Reveal())
	ms := NewMeetings(deps.db, deps.events)
	// The DELETE fault trigger fires per row: a meeting must exist.
	if _, _, err := ms.Confirm(reload(t, deps.db, "a"), reload(t, deps.db, "b"), OriginOther); err != nil {
		t.Fatal(err)
	}
	failWrites(t, deps.db, "reset_meetings")

	if resp := post(t, srv, "/admin/reset", nil); resp.StatusCode != 500 {
		t.Errorf("want 500, got %d", resp.StatusCode)
	}
}

// TestDeleteParticipant_RemovesTheirMeetings covers the delete through the
// route: a Participant's meetings leave with them.
func TestDeleteParticipant_RemovesTheirMeetings(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The YAML Wrangler", "ready")
	must(t, deps.events.Reveal())
	ms := NewMeetings(deps.db, deps.events)
	if _, _, err := ms.Confirm(reload(t, deps.db, "a"), reload(t, deps.db, "b"), OriginOther); err != nil {
		t.Fatal(err)
	}

	if resp := del(t, srv, "/data/participant/b"); resp.StatusCode != 200 {
		t.Fatalf("DELETE: want 200, got %d", resp.StatusCode)
	}
	if all, _ := ms.All(); len(all) != 0 {
		t.Errorf("a deleted Participant's meetings are removed, got %d", len(all))
	}
}

func TestDeleteParticipant_ReportsAMeetingsFailure(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The YAML Wrangler", "ready")
	must(t, deps.events.Reveal())
	ms := NewMeetings(deps.db, deps.events)
	// The DELETE fault trigger fires per row: a meeting must exist.
	if _, _, err := ms.Confirm(reload(t, deps.db, "a"), reload(t, deps.db, "b"), OriginOther); err != nil {
		t.Fatal(err)
	}
	failWrites(t, deps.db, "meet_delete")

	if resp := del(t, srv, "/data/participant/b"); resp.StatusCode != 500 {
		t.Errorf("want 500, got %d", resp.StatusCode)
	}
}
