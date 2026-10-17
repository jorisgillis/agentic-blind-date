package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// This file covers ticket #117's contact exchange: the contact_channels
// store, the settings section (channels off by default, the github channel
// pre-filled from the profile, values entered once), the post-meeting
// exchange prompt with the mutual reveal rule, and the /data/channels debug
// endpoint showing counts only, never values.

// contactSetup seeds two ready Participants and advances the event to the
// social, so meeting confirmation is open.
func contactSetup(t *testing.T) (*testSrv, *testDeps) {
	t.Helper()
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The YAML Wrangler", "ready")
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}
	return srv, deps
}

// saveChannels posts the settings form for the Participant, the way the
// personal page does.
func saveChannels(t *testing.T, srv *testSrv, id string, form url.Values) {
	t.Helper()
	if resp := post(t, srv, "/user/settings/"+id, form); resp.StatusCode != http.StatusOK {
		t.Fatalf("saving %s's channels: want 200, got %d", id, resp.StatusCode)
	}
}

// completeAB confirms the meeting between a and b from both sides.
func completeAB(t *testing.T, deps *testDeps) {
	t.Helper()
	ms := NewMeetings(deps.db, deps.events)
	x, y := reload(t, deps.db, "a"), reload(t, deps.db, "b")
	if _, _, err := ms.Confirm(x, y, OriginOther); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ms.Confirm(y, x, OriginOther); err != nil {
		t.Fatal(err)
	}
}

func TestContactChannels_AreOffByDefault(t *testing.T) {
	db := newTestDB(t)
	seed(t, db, "a", "The Gopher", "ready")
	channels := NewContactChannels(db)

	list, err := channels.Channels("a")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != len(ContactChannelNames) {
		t.Fatalf("want %d channels, got %d", len(ContactChannelNames), len(list))
	}
	for i, name := range ContactChannelNames {
		if list[i].Channel != name {
			t.Errorf("channel %d: want %q, got %q", i, name, list[i].Channel)
		}
		if list[i].Label == "" {
			t.Errorf("channel %q has no label", name)
		}
		if list[i].Enabled {
			t.Errorf("channel %q is on by default", name)
		}
		if list[i].Value != "" {
			t.Errorf("channel %q has a value by default: %q", name, list[i].Value)
		}
	}
}

// TestContactChannels_SaveIsAnUpsert covers the store's save: an upsert per
// channel, all in one transaction. The settings form always saves every
// channel, so a save rewrites them all — turning a channel off keeps its
// value stored, entered once.
func TestContactChannels_SaveIsAnUpsert(t *testing.T) {
	db := newTestDB(t)
	seed(t, db, "a", "The Gopher", "ready")
	channels := NewContactChannels(db)

	err := channels.Save("a", []ContactChannel{
		{Channel: "email", Value: "a@example.com", Enabled: true},
		{Channel: "other", Value: "carrier pigeon", Enabled: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	list, err := channels.Channels("a")
	if err != nil {
		t.Fatal(err)
	}
	if got := list[1]; !got.Enabled || got.Value != "a@example.com" {
		t.Errorf("email: want on with the saved value, got %+v", got)
	}
	if got := list[5]; !got.Enabled || got.Value != "carrier pigeon" {
		t.Errorf("other: want on with the saved value, got %+v", got)
	}

	// Saving again rewrites the channels it carries: the email value
	// changes, and the channel the form leaves unchecked comes back off —
	// its value stays stored.
	err = channels.Save("a", []ContactChannel{
		{Channel: "email", Value: "new@example.com", Enabled: true},
		{Channel: "other", Value: "carrier pigeon", Enabled: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	list, err = channels.Channels("a")
	if err != nil {
		t.Fatal(err)
	}
	if got := list[1]; !got.Enabled || got.Value != "new@example.com" {
		t.Errorf("email after the second save: want on with the new value, got %+v", got)
	}
	if got := list[5]; got.Enabled || got.Value != "carrier pigeon" {
		t.Errorf("other after the second save: want off with the value kept, got %+v", got)
	}
}

// TestContactChannels_RevealedIsTheMutualRule covers the mutual rule in the
// one place it lives: a channel is revealed only if the owner opted in to it
// AND the pair's meeting is completed.
func TestContactChannels_RevealedIsTheMutualRule(t *testing.T) {
	newPair := func(t *testing.T) (*DB, *EventStore, *ContactChannels) {
		db := newTestDB(t)
		events := NewEventStore(db)
		seed(t, db, "a", "The Gopher", "ready")
		seed(t, db, "b", "The YAML Wrangler", "ready")
		if err := events.Advance(PhaseSocial); err != nil {
			t.Fatal(err)
		}
		return db, events, NewContactChannels(db)
	}
	optIn := func(t *testing.T, channels *ContactChannels, id, value string, enabled bool) {
		t.Helper()
		if err := channels.Save(id, []ContactChannel{{Channel: "email", Value: value, Enabled: enabled}}); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("no meeting at all", func(t *testing.T) {
		_, _, channels := newPair(t)
		optIn(t, channels, "a", "a@example.com", true)
		if got, err := channels.Revealed("a", "b"); err != nil || len(got) != 0 {
			t.Errorf("without a meeting nothing is revealed, got %v (err %v)", got, err)
		}
	})

	t.Run("a pending meeting reveals nothing", func(t *testing.T) {
		db, events, channels := newPair(t)
		optIn(t, channels, "a", "a@example.com", true)
		ms := NewMeetings(db, events)
		// One side only: the meeting is pending, not completed.
		if _, _, err := ms.Confirm(reload(t, db, "a"), reload(t, db, "b"), OriginOther); err != nil {
			t.Fatal(err)
		}
		if got, err := channels.Revealed("a", "b"); err != nil || len(got) != 0 {
			t.Errorf("a pending meeting reveals nothing, got %v (err %v)", got, err)
		}
		if got, err := channels.Revealed("b", "a"); err != nil || len(got) != 0 {
			t.Errorf("a pending meeting reveals nothing to the other side either, got %v (err %v)", got, err)
		}
	})

	t.Run("the one-sided case: the owner did not opt in", func(t *testing.T) {
		db, events, channels := newPair(t)
		// a entered an email but never turned the channel on.
		optIn(t, channels, "a", "a@example.com", false)
		ms := NewMeetings(db, events)
		if _, _, err := ms.Confirm(reload(t, db, "a"), reload(t, db, "b"), OriginOther); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ms.Confirm(reload(t, db, "b"), reload(t, db, "a"), OriginOther); err != nil {
			t.Fatal(err)
		}
		if got, err := channels.Revealed("a", "b"); err != nil || len(got) != 0 {
			t.Errorf("an opted-out channel is never revealed, got %v (err %v)", got, err)
		}
	})

	t.Run("an opted-in channel with no value shares nothing", func(t *testing.T) {
		db, events, channels := newPair(t)
		optIn(t, channels, "a", "", true)
		ms := NewMeetings(db, events)
		if _, _, err := ms.Confirm(reload(t, db, "a"), reload(t, db, "b"), OriginOther); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ms.Confirm(reload(t, db, "b"), reload(t, db, "a"), OriginOther); err != nil {
			t.Fatal(err)
		}
		if got, err := channels.Revealed("a", "b"); err != nil || len(got) != 0 {
			t.Errorf("an empty channel shares nothing, got %v (err %v)", got, err)
		}
	})

	t.Run("both opted in and the meeting completed", func(t *testing.T) {
		db, events, channels := newPair(t)
		optIn(t, channels, "a", "a@example.com", true)
		optIn(t, channels, "b", "b@example.com", true)
		ms := NewMeetings(db, events)
		if _, _, err := ms.Confirm(reload(t, db, "a"), reload(t, db, "b"), OriginOther); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ms.Confirm(reload(t, db, "b"), reload(t, db, "a"), OriginOther); err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct{ owner, viewer, want string }{
			{"a", "b", "a@example.com"},
			{"b", "a", "b@example.com"},
		} {
			got, err := channels.Revealed(tc.owner, tc.viewer)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].Value != tc.want {
				t.Errorf("Revealed(%s, %s): want the email %q, got %+v", tc.owner, tc.viewer, tc.want, got)
			}
		}
	})
}

// TestContactChannels_ExchangedTakesTheMeetingInHand covers the exchange
// join's seam: the mutual rule applied to the Meeting the caller already
// holds — both channel sets under one operation, without the pair's row
// being re-queried.
func TestContactChannels_ExchangedTakesTheMeetingInHand(t *testing.T) {
	newPair := func(t *testing.T) (*DB, *EventStore, *ContactChannels) {
		db := newTestDB(t)
		events := NewEventStore(db)
		seed(t, db, "a", "The Gopher", "ready")
		seed(t, db, "b", "The YAML Wrangler", "ready")
		if err := events.Advance(PhaseSocial); err != nil {
			t.Fatal(err)
		}
		return db, events, NewContactChannels(db)
	}
	optIn := func(t *testing.T, channels *ContactChannels, id, value string, enabled bool) {
		t.Helper()
		if err := channels.Save(id, []ContactChannel{{Channel: "email", Value: value, Enabled: enabled}}); err != nil {
			t.Fatal(err)
		}
	}
	complete := func(t *testing.T, db *DB, events *EventStore) *Meeting {
		t.Helper()
		ms := NewMeetings(db, events)
		if _, _, err := ms.Confirm(reload(t, db, "a"), reload(t, db, "b"), OriginOther); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ms.Confirm(reload(t, db, "b"), reload(t, db, "a"), OriginOther); err != nil {
			t.Fatal(err)
		}
		m, err := ms.Between("a", "b")
		if err != nil || m == nil {
			t.Fatalf("the pair's meeting: got %v, err %v", m, err)
		}
		return m
	}

	t.Run("a missing meeting shares nothing", func(t *testing.T) {
		_, _, channels := newPair(t)
		if ex, err := channels.Exchanged(nil, "a"); err != nil || len(ex.Revealed) != 0 || len(ex.Shared) != 0 {
			t.Errorf("without a meeting nothing is shared, got %+v (err %v)", ex, err)
		}
	})

	t.Run("a pending meeting shares nothing", func(t *testing.T) {
		db, events, channels := newPair(t)
		optIn(t, channels, "a", "a@example.com", true)
		optIn(t, channels, "b", "b@example.com", true)
		// One side only: the meeting is pending, not completed.
		if _, _, err := NewMeetings(db, events).Confirm(reload(t, db, "a"), reload(t, db, "b"), OriginOther); err != nil {
			t.Fatal(err)
		}
		m, err := NewMeetings(db, events).Between("a", "b")
		if err != nil || m == nil {
			t.Fatalf("the pair's meeting: got %v, err %v", m, err)
		}
		if ex, err := channels.Exchanged(m, "a"); err != nil || len(ex.Revealed) != 0 || len(ex.Shared) != 0 {
			t.Errorf("a pending meeting shares nothing, got %+v (err %v)", ex, err)
		}
	})

	t.Run("a completed meeting returns both channel sets", func(t *testing.T) {
		db, events, channels := newPair(t)
		optIn(t, channels, "a", "a@example.com", true)
		optIn(t, channels, "b", "b@example.com", true)
		m := complete(t, db, events)
		for _, tc := range []struct{ id, revealed, shared string }{
			{"a", "b@example.com", "a@example.com"},
			{"b", "a@example.com", "b@example.com"},
		} {
			ex, err := channels.Exchanged(m, tc.id)
			if err != nil {
				t.Fatal(err)
			}
			if len(ex.Revealed) != 1 || ex.Revealed[0].Value != tc.revealed {
				t.Errorf("Exchanged(m, %s).Revealed: want the email %q, got %+v", tc.id, tc.revealed, ex.Revealed)
			}
			if len(ex.Shared) != 1 || ex.Shared[0].Value != tc.shared {
				t.Errorf("Exchanged(m, %s).Shared: want the email %q, got %+v", tc.id, tc.shared, ex.Shared)
			}
		}
	})

	t.Run("the meeting in hand is not re-queried", func(t *testing.T) {
		db, events, channels := newPair(t)
		optIn(t, channels, "a", "a@example.com", true)
		optIn(t, channels, "b", "b@example.com", true)
		m := complete(t, db, events)
		// The fault fails any read of the meetings table: the operation must
		// manage on the Meeting it was handed, not re-fetch the pair's row.
		breakOnFault(t, db, "meet_read")
		ex, err := channels.Exchanged(m, "a")
		if err != nil {
			t.Fatalf("the meeting in hand must not be re-queried: %v", err)
		}
		if len(ex.Revealed) != 1 || ex.Revealed[0].Value != "b@example.com" {
			t.Errorf("the exchange still works, got %+v", ex)
		}
	})

	t.Run("a channels read failure is reported", func(t *testing.T) {
		t.Run("the other side's channels", func(t *testing.T) {
			db, events, channels := newPair(t)
			// Only b carries a channel row: the fault fails on that read.
			optIn(t, channels, "b", "b@example.com", true)
			m := complete(t, db, events)
			breakOnFault(t, db, "channel_read")
			if _, err := channels.Exchanged(m, "a"); err == nil {
				t.Error("want an error when the other side's channels cannot be read")
			}
		})
		t.Run("the participant's own channels", func(t *testing.T) {
			db, events, channels := newPair(t)
			// Only a carries a channel row: the other side's read succeeds
			// (no rows), the participant's own fails.
			optIn(t, channels, "a", "a@example.com", true)
			m := complete(t, db, events)
			breakOnFault(t, db, "channel_read")
			if _, err := channels.Exchanged(m, "a"); err == nil {
				t.Error("want an error when the participant's own channels cannot be read")
			}
		})
	})
}

// TestSettings_ShowTheChannelsOffByDefaultWithGitHubPrefilled covers the
// settings section: every channel is listed off by default, and the github
// channel is pre-filled from the Participant's GitHub profile.
func TestSettings_ShowTheChannelsOffByDefaultWithGitHubPrefilled(t *testing.T) {
	t.Run("github pre-filled from the profile", func(t *testing.T) {
		srv, deps := newTestServer(t, nil, nil)
		seed(t, deps.db, "a", "The Gopher", "ready")
		setProfile(t, deps.db, "a", &GitHubProfile{Login: "octocat"})

		body := readBody(t, get(t, srv, "/user/settings/a"))
		for _, want := range []string{`name="github"`, `value="octocat"`, `name="email"`, `name="linkedin"`, `name="instagram"`, `name="mastodon"`, `name="other"`, `hx-post="/user/settings/a"`} {
			if !strings.Contains(body, want) {
				t.Errorf("the settings section should contain %q, got: %s", want, body)
			}
		}
		if strings.Contains(body, "checked") {
			t.Error("every channel starts off — no checkbox is checked")
		}
	})

	t.Run("github pre-filled from the handle without a profile", func(t *testing.T) {
		srv, deps := newTestServer(t, nil, nil)
		if err := deps.db.CreateParticipant("h", "handle-h", "H", true); err != nil {
			t.Fatal(err)
		}
		forceStep(deps.db, "h", StepReady)

		body := readBody(t, get(t, srv, "/user/settings/h"))
		if !strings.Contains(body, `name="github"`) || !strings.Contains(body, `value="handle-h"`) {
			t.Errorf("without a profile the github channel is pre-filled from the handle, got: %s", body)
		}
	})

	t.Run("no pre-fill for a non-github participant", func(t *testing.T) {
		srv, deps := newTestServer(t, nil, nil)
		if err := deps.db.CreateParticipant("n", "no-github-1234abcd", "N", false); err != nil {
			t.Fatal(err)
		}
		forceStep(deps.db, "n", StepReady)

		body := readBody(t, get(t, srv, "/user/settings/n"))
		if !strings.Contains(body, `name="github"`) {
			t.Errorf("the github channel is listed, got: %s", body)
		}
		if strings.Contains(body, `value="no-github-1234abcd"`) {
			t.Error("the generated handle is never pre-filled into the github channel")
		}
	})

	t.Run("unknown participant", func(t *testing.T) {
		srv, _ := newTestServer(t, nil, nil)
		if resp := get(t, srv, "/user/settings/nobody"); resp.StatusCode != 404 {
			t.Errorf("want 404, got %d", resp.StatusCode)
		}
	})
}

// TestSaveSettings_PersistsTheToggles covers the settings form's save: the
// toggles and values are stored, the re-rendered section shows them, and the
// values are entered once — they come back on the next open.
func TestSaveSettings_PersistsTheToggles(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")

	form := url.Values{
		"email":       {"a@example.com"},
		"email_on":    {"on"},
		"linkedin":    {"linkedin.com/in/a"}, // entered but not shared
		"mastodon":    {"@a@mastodon.social"},
		"mastodon_on": {"on"},
	}
	resp := post(t, srv, "/user/settings/a", form)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("saving a's channels: want 200, got %d", resp.StatusCode)
	}

	channels := NewContactChannels(deps.db)
	list, err := channels.Channels("a")
	if err != nil {
		t.Fatal(err)
	}
	if got := list[1]; !got.Enabled || got.Value != "a@example.com" {
		t.Errorf("email: want on with the value, got %+v", got)
	}
	if got := list[2]; got.Enabled || got.Value != "linkedin.com/in/a" {
		t.Errorf("linkedin: entered but off, got %+v", got)
	}
	if got := list[4]; !got.Enabled || got.Value != "@a@mastodon.social" {
		t.Errorf("mastodon: want on with the value, got %+v", got)
	}

	// The save's response confirms the save; a fresh open shows the stored
	// state — the values are entered once, they come back on the next open.
	if body := readBody(t, resp); !strings.Contains(body, "Saved") {
		t.Error("the save's response should confirm the save")
	}
	body := readBody(t, get(t, srv, "/user/settings/a"))
	if !strings.Contains(body, `value="a@example.com"`) || !strings.Contains(body, `value="@a@mastodon.social"`) {
		t.Error("the saved values are entered once — they come back on the next open")
	}
	if strings.Count(body, "checked") != 2 {
		t.Errorf("want the two opted-in channels checked, got: %s", body)
	}

	// Turning a channel off keeps it stored but unshared.
	saveChannels(t, srv, "a", url.Values{"email": {"a@example.com"}})
	list, err = channels.Channels("a")
	if err != nil {
		t.Fatal(err)
	}
	if got := list[1]; got.Enabled {
		t.Errorf("email after saving without the toggle: want off, got %+v", got)
	}
}

// TestSettings_AChannelReadFailureIsLogged covers the settings section
// against a channels read that fails: the section still renders, without the
// channels.
func TestSettings_AChannelReadFailureIsLogged(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	saveChannels(t, srv, "a", url.Values{"email": {"a@example.com"}, "email_on": {"on"}})
	breakOnFault(t, deps.db, "channel_read")

	resp := get(t, srv, "/user/settings/a")
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, `id="settings"`) {
		t.Error("the section still renders")
	}
	if strings.Contains(body, `name="email"`) || strings.Contains(body, "a@example.com") {
		t.Error("the failed read shows no channels")
	}
}

// TestSaveSettings_ReportsFailures covers the settings form's edges: an
// unknown Participant is a 404, a failed save is a 500.
func TestSaveSettings_ReportsFailures(t *testing.T) {
	t.Run("unknown participant", func(t *testing.T) {
		srv, _ := newTestServer(t, nil, nil)
		if resp := post(t, srv, "/user/settings/nobody", url.Values{"email": {"x@y.z"}}); resp.StatusCode != 404 {
			t.Errorf("want 404, got %d", resp.StatusCode)
		}
	})
	t.Run("a failed save", func(t *testing.T) {
		srv, deps := newTestServer(t, nil, nil)
		seed(t, deps.db, "a", "The Gopher", "ready")
		failWrites(t, deps.db, "channel_insert")
		if resp := post(t, srv, "/user/settings/a", url.Values{"email": {"x@y.z"}, "email_on": {"on"}}); resp.StatusCode != 500 {
			t.Errorf("want 500, got %d", resp.StatusCode)
		}
	})
}

// TestExchangePrompt_TheMutualRule covers the acceptance rule on the phones:
// after a completed meeting, both sides see an "Exchange contact details?"
// prompt — a opted in on email, b not, so a sees nothing of b, and b sees a's
// email; once both opt in, both see it.
func TestExchangePrompt_TheMutualRule(t *testing.T) {
	srv, deps := contactSetup(t)
	completeAB(t, deps)

	// a opted in on email; b entered one but never turned the channel on.
	saveChannels(t, srv, "a", url.Values{"email": {"a@example.com"}, "email_on": {"on"}})
	saveChannels(t, srv, "b", url.Values{"email": {"b@example.com"}})

	bodyA := readBody(t, get(t, srv, "/user/meet-status/a"))
	if !strings.Contains(bodyA, "Exchange contact details?") {
		t.Errorf("a completed meeting shows the exchange prompt, got: %s", bodyA)
	}
	if !strings.Contains(bodyA, "a@example.com") {
		t.Error("a sees what a shared with b")
	}
	if strings.Contains(bodyA, "b@example.com") {
		t.Error("a sees nothing of b: b never opted in")
	}

	bodyB := readBody(t, get(t, srv, "/user/meet-status/b"))
	if !strings.Contains(bodyB, "a@example.com") {
		t.Error("b sees a's email: a opted in")
	}
	if strings.Contains(bodyB, "b@example.com") {
		t.Error("b's own opted-out email is not revealed to b as shared")
	}

	// Once both opt in, both see it.
	saveChannels(t, srv, "b", url.Values{"email": {"b@example.com"}, "email_on": {"on"}})
	bodyA = readBody(t, get(t, srv, "/user/meet-status/a"))
	if !strings.Contains(bodyA, "b@example.com") {
		t.Error("with both opted in, a sees b's email")
	}
	bodyB = readBody(t, get(t, srv, "/user/meet-status/b"))
	if !strings.Contains(bodyB, "a@example.com") || !strings.Contains(bodyB, "b@example.com") {
		t.Error("with both opted in, b sees both emails")
	}
}

// TestExchangePrompt_OnlyAfterACompletedMeeting covers the rule's other half:
// while the meeting is still pending — both opted in — no contact data is
// visible on either phone.
func TestExchangePrompt_OnlyAfterACompletedMeeting(t *testing.T) {
	srv, deps := contactSetup(t)
	saveChannels(t, srv, "a", url.Values{"email": {"a@example.com"}, "email_on": {"on"}})
	saveChannels(t, srv, "b", url.Values{"email": {"b@example.com"}, "email_on": {"on"}})

	// One side confirmed: the meeting is pending.
	ms := NewMeetings(deps.db, deps.events)
	if _, _, err := ms.Confirm(reload(t, deps.db, "a"), reload(t, deps.db, "b"), OriginOther); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"a", "b"} {
		body := readBody(t, get(t, srv, "/user/meet-status/"+id))
		if strings.Contains(body, "Exchange contact details?") {
			t.Errorf("%s: a pending meeting shows no exchange prompt, got: %s", id, body)
		}
		if strings.Contains(body, "a@example.com") || strings.Contains(body, "b@example.com") {
			t.Errorf("%s: no contact data is visible without the completed meeting, got: %s", id, body)
		}
	}

	// The second side completes the meeting: the prompt appears on both.
	if _, _, err := ms.Confirm(reload(t, deps.db, "b"), reload(t, deps.db, "a"), OriginOther); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		body := readBody(t, get(t, srv, "/user/meet-status/"+id))
		if !strings.Contains(body, "Exchange contact details?") {
			t.Errorf("%s: the completed meeting shows the exchange prompt, got: %s", id, body)
		}
	}
}

// TestExchangePrompt_NobodySharedAnything covers the prompt's empty state.
func TestExchangePrompt_NobodySharedAnything(t *testing.T) {
	srv, deps := contactSetup(t)
	completeAB(t, deps)

	body := readBody(t, get(t, srv, "/user/meet-status/a"))
	if !strings.Contains(body, "Exchange contact details?") {
		t.Errorf("the prompt shows even when nobody shared anything, got: %s", body)
	}
	if !strings.Contains(body, "nothing") {
		t.Error("the empty state says so")
	}
}

// TestMeetStatus_AChannelReadFailureIsLogged covers the meet section against
// a channels read that fails — on both sides' reads: the meetings still
// list, without the channels.
func TestMeetStatus_AChannelReadFailureIsLogged(t *testing.T) {
	srv, deps := contactSetup(t)
	completeAB(t, deps)
	// Both sides carry a channel row: the fault fails while either side's
	// channels are read.
	saveChannels(t, srv, "a", url.Values{"email": {"a@example.com"}, "email_on": {"on"}})
	saveChannels(t, srv, "b", url.Values{"email": {"b@example.com"}, "email_on": {"on"}})
	breakOnFault(t, deps.db, "channel_read")

	resp := get(t, srv, "/user/meet-status/a")
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "The YAML Wrangler") {
		t.Error("the meetings still list")
	}
	if strings.Contains(body, "a@example.com") {
		t.Error("the failed read reveals nothing")
	}
}

// TestPersonalPages_IncludeTheSettingsSection covers the settings section on
// both personal pages.
func TestPersonalPages_IncludeTheSettingsSection(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The YAML Wrangler", "ready")
	seed(t, deps.db, "solo", "The Loner", "ready")
	pair(t, deps.db, "a", "b")
	must(t, deps.events.Reveal())

	for _, path := range []string{"/user/match/a", "/user/wait/solo"} {
		body := readBody(t, get(t, srv, path))
		if !strings.Contains(body, `id="settings"`) || !strings.Contains(body, `hx-post="/user/settings/`) {
			t.Errorf("%s should include the settings section, got: %s", path, body)
		}
	}
}

// TestDataChannels_ShowsCountsOnly covers the debug endpoint's rule: per
// Participant, how many channels they opted in to — counts only, never
// values, and the values stay out of every other debug endpoint too.
func TestDataChannels_ShowsCountsOnly(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The YAML Wrangler", "ready")
	seed(t, deps.db, "c", "The Kernel Panicker", "ready")
	saveChannels(t, srv, "a", url.Values{
		"email":     {"a@example.com"},
		"email_on":  {"on"},
		"github":    {"octocat-a"},
		"github_on": {"on"},
		"linkedin":  {"linkedin.com/in/a"}, // entered but not shared
	})
	saveChannels(t, srv, "c", url.Values{"other": {"carrier pigeon"}, "other_on": {"on"}})

	body := readBody(t, get(t, srv, "/data/channels"))
	var counts []ChannelCount
	if err := json.Unmarshal([]byte(body), &counts); err != nil {
		t.Fatalf("decoding the channel counts: %v", err)
	}
	byID := map[string]int{}
	for _, c := range counts {
		byID[c.ParticipantID] = c.Enabled
		if c.PersonaName == "" {
			t.Errorf("the count for %s carries no persona name", c.ParticipantID)
		}
	}
	if len(counts) != 3 {
		t.Errorf("every Participant is counted, got %d", len(counts))
	}
	if byID["a"] != 2 || byID["b"] != 0 || byID["c"] != 1 {
		t.Errorf("the opted-in counts: want a=2 b=0 c=1, got %v", byID)
	}

	// Counts only, never values — not here, not anywhere else in /data.
	for _, leak := range []string{"a@example.com", "octocat-a", "linkedin.com/in/a", "carrier pigeon", "email", "github"} {
		if strings.Contains(body, leak) {
			t.Errorf("/data/channels leaks %q", leak)
		}
	}
	for _, path := range []string{"/data/participants", "/data/participant/a", "/data/meetings", "/data/leaderboard"} {
		if body := readBody(t, get(t, srv, path)); strings.Contains(body, "a@example.com") || strings.Contains(body, "carrier pigeon") {
			t.Errorf("%s leaks contact values", path)
		}
	}

	// The debug dashboard links the endpoint.
	if body := readBody(t, get(t, srv, "/data")); !strings.Contains(body, "/data/channels") {
		t.Error("the debug dashboard should link /data/channels")
	}
}

// TestDataChannels_ReportsAReadFailure covers the debug endpoint's error path.
func TestDataChannels_ReportsAReadFailure(t *testing.T) {
	t.Run("a scan failure", func(t *testing.T) {
		srv, deps := newTestServer(t, nil, nil)
		seed(t, deps.db, "a", "The Gopher", "ready")
		breakOnFault(t, deps.db, "channel_count_read")
		if resp := get(t, srv, "/data/channels"); resp.StatusCode != 500 {
			t.Errorf("want 500, got %d", resp.StatusCode)
		}
	})
	t.Run("a broken database", func(t *testing.T) {
		srv, deps := newTestServer(t, nil, nil)
		seed(t, deps.db, "a", "The Gopher", "ready")
		breakDB(deps.db)
		if resp := get(t, srv, "/data/channels"); resp.StatusCode != 500 {
			t.Errorf("want 500, got %d", resp.StatusCode)
		}
	})
}

// TestContactChannels_ReportsFailures covers the store's error paths: each
// read and write fails as a genuine database error.
func TestContactChannels_ReportsFailures(t *testing.T) {
	t.Run("reads against a broken database", func(t *testing.T) {
		db := newTestDB(t)
		breakDB(db)
		channels := NewContactChannels(db)
		if _, err := channels.Channels("a"); err == nil {
			t.Error("Channels: want an error")
		}
		if _, err := channels.Counts(); err == nil {
			t.Error("Counts: want an error")
		}
		if _, err := channels.Revealed("a", "b"); err == nil {
			t.Error("Revealed: want an error")
		}
	})
	t.Run("a channels scan failure", func(t *testing.T) {
		db := newTestDB(t)
		seed(t, db, "a", "The Gopher", "ready")
		seed(t, db, "b", "The YAML Wrangler", "ready")
		events := NewEventStore(db)
		if err := events.Advance(PhaseSocial); err != nil {
			t.Fatal(err)
		}
		channels := NewContactChannels(db)
		if err := channels.Save("a", []ContactChannel{{Channel: "email", Value: "a@example.com", Enabled: true}}); err != nil {
			t.Fatal(err)
		}
		ms := NewMeetings(db, events)
		if _, _, err := ms.Confirm(reload(t, db, "a"), reload(t, db, "b"), OriginOther); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ms.Confirm(reload(t, db, "b"), reload(t, db, "a"), OriginOther); err != nil {
			t.Fatal(err)
		}
		breakOnFault(t, db, "channel_read")
		if _, err := channels.Channels("a"); err == nil {
			t.Error("Channels: want an error")
		}
		if _, err := channels.Revealed("a", "b"); err == nil {
			t.Error("Revealed: want an error when the owner's channels cannot be read")
		}
	})
	t.Run("a save failure", func(t *testing.T) {
		db := newTestDB(t)
		seed(t, db, "a", "The Gopher", "ready")
		channels := NewContactChannels(db)
		failWrites(t, db, "channel_insert")
		if err := channels.Save("a", []ContactChannel{{Channel: "email", Value: "a@example.com", Enabled: true}}); err == nil {
			t.Error("Save: want an error")
		}
	})
	t.Run("a delete failure", func(t *testing.T) {
		db := newTestDB(t)
		seed(t, db, "a", "The Gopher", "ready")
		channels := NewContactChannels(db)
		// A row to delete: the fault's trigger cannot fire on an empty table.
		if err := channels.Save("a", []ContactChannel{{Channel: "email", Value: "a@example.com", Enabled: true}}); err != nil {
			t.Fatal(err)
		}
		failWrites(t, db, "channel_delete")
		if err := channels.RemoveParticipant("a"); err == nil {
			t.Error("RemoveParticipant: want an error")
		}
	})
}

// TestDeleteParticipant_RemovesTheirChannels covers the cleanup when a
// Participant leaves: their channels go with them.
func TestDeleteParticipant_RemovesTheirChannels(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	saveChannels(t, srv, "a", url.Values{"email": {"a@example.com"}, "email_on": {"on"}})

	if resp := del(t, srv, "/data/participant/a"); resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	list, err := NewContactChannels(deps.db).Channels("a")
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range list {
		if ch.Enabled || ch.Value != "" {
			t.Errorf("the channels of a deleted Participant are gone, got %+v", ch)
		}
	}
}

// TestDeleteParticipant_ReportsAChannelsFailure covers the cleanup's error path.
func TestDeleteParticipant_ReportsAChannelsFailure(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	// A row to delete: the fault's trigger cannot fire on an empty table.
	saveChannels(t, srv, "a", url.Values{"email": {"a@example.com"}, "email_on": {"on"}})
	failWrites(t, deps.db, "channel_delete")

	if resp := del(t, srv, "/data/participant/a"); resp.StatusCode != 500 {
		t.Errorf("want 500, got %d", resp.StatusCode)
	}
}

// TestReset_ClearsTheChannels covers the reset: the contact channels go with
// the event.
func TestReset_ClearsTheChannels(t *testing.T) {
	db := newTestDB(t)
	events := NewEventStore(db)
	seed(t, db, "a", "The Gopher", "ready")
	channels := NewContactChannels(db)
	if err := channels.Save("a", []ContactChannel{{Channel: "email", Value: "a@example.com", Enabled: true}}); err != nil {
		t.Fatal(err)
	}

	if err := events.Reset(); err != nil {
		t.Fatal(err)
	}
	list, err := channels.Channels("a")
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range list {
		if ch.Enabled || ch.Value != "" {
			t.Errorf("after a reset the channels are cleared, got %+v", ch)
		}
	}
}

// TestReset_ReportsAChannelsOrRecapsFailure covers the reset's new deletes:
// each one failing fails the whole reset.
func TestReset_ReportsAChannelsOrRecapsFailure(t *testing.T) {
	for _, tag := range []string{"reset_contact_channels", "reset_recaps"} {
		t.Run(tag, func(t *testing.T) {
			db := newTestDB(t)
			events := NewEventStore(db)
			seed(t, db, "a", "The Gopher", "ready")
			// Rows in both tables: the faults' triggers cannot fire on an
			// empty table.
			if err := NewContactChannels(db).Save("a", []ContactChannel{{Channel: "email", Value: "a@example.com", Enabled: true}}); err != nil {
				t.Fatal(err)
			}
			if _, err := NewRecaps(db).TokenFor("a"); err != nil {
				t.Fatal(err)
			}
			breakOnFault(t, db, tag)
			if err := events.Reset(); err == nil {
				t.Error("want a failure")
			}
		})
	}
}
