package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// seed creates a Participant with a persona, profile and answers in the given pipeline step.
func seed(t *testing.T, db *DB, id, persona string, step Step) *Participant {
	t.Helper()
	if err := db.CreateParticipant(id, id, id, true); err != nil {
		t.Fatal(err)
	}
	questions := []Question{{ID: "fixed_0", Text: "Tabs or spaces?"}}
	setProfile(t, db, id, &GitHubProfile{Login: id, Languages: []string{"Go"}})
	setPersona(t, db, id, persona, "Ships things")
	forceQuestions(db, id, questions)
	if step != "interviewing" && step != "fetching_github" {
		updateAnswers(t, db, id, map[string]string{"fixed_0": "Tabs"})
	}
	forceStep(db, id, step)
	return reload(t, db, id)
}

// awaitingPersona puts a Participant at the persona step, as if their Interview just completed.
func awaitingPersona(t *testing.T, db *DB, id string) {
	t.Helper()
	must(t, db.CreateParticipant(id, id, id, true))
	setProfile(t, db, id, &GitHubProfile{Login: id, Languages: []string{"Go"}})
	forceStep(db, id, StepCreatingPersona)
}

func pair(t *testing.T, db *DB, a, b string) {
	t.Helper()
	result := &matchResult{Score: 91, Reason: "Both love tabs", RedFlags: []string{"hogs the whiteboard"}, GreenFlags: []string{"tabs"}, Icebreakers: []string{"Why tabs?"}}
	if _, err := NewRelationships(db).Pair(Match{A: reload(t, db, a), B: reload(t, db, b), Result: result}); err != nil {
		t.Fatal(err)
	}
}

// withCookie fetches path with the cookie set; the body is closed through the t.Cleanup below.
//
//bodyclose:handled
func withCookie(t *testing.T, srv *testSrv, path, id string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", srv.URL+path, nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: id}) //nolint:gosec // G124: an outgoing test request, no browser and no attributes to set
	client := srv.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestLanding_ReturningParticipantIsSentToOnboarding(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "p1", "The Gopher", "interviewing")

	resp := withCookie(t, srv, "/user", "p1")

	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/user/onboard/p1" {
		t.Errorf("want redirect to onboarding, got %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestLanding_UnknownCookieIsClearedAndTheFormShown(t *testing.T) {
	srv, _ := newTestServer(t, nil, nil)

	resp := withCookie(t, srv, "/user", "gone")

	if resp.StatusCode != 200 {
		t.Errorf("want the landing page, got %d", resp.StatusCode)
	}
	if c := resp.Header.Get("Set-Cookie"); !strings.Contains(c, cookieName+"=;") {
		t.Errorf("want the stale cookie cleared, got %q", c)
	}
}

func TestLanding_RendersTheAnimatedWalkthrough(t *testing.T) {
	srv, _ := newTestServer(t, nil, nil)

	body := readBody(t, get(t, srv, "/user"))

	// The walkthrough: the heading, all six steps, one scene per step, and the
	// reduced-motion static fallback.
	for _, want := range []string{
		"How the evening flows",
		"1 · Join", "2 · Interview", "3 · Match",
		"4 · Social", "5 · Show &amp; tell", "6 · Recap",
		"prefers-reduced-motion",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("landing page: want %q in the walkthrough", want)
		}
	}
	if n := strings.Count(body, `class="lw-scene"`); n != 6 {
		t.Errorf("want 6 walkthrough scenes, got %d", n)
	}

	// The join form stays immediately usable, and no prototype chrome ships.
	for _, want := range []string{`id="joinForm"`, `action="/user/join"`} {
		if !strings.Contains(body, want) {
			t.Errorf("landing page: want %q", want)
		}
	}
	for _, unwanted := range []string{"lw-switcher", "PROTOTYPE", "variant="} {
		if strings.Contains(body, unwanted) {
			t.Errorf("landing page: prototype chrome %q must not ship", unwanted)
		}
	}
}

// TestLanding_WalkthroughStyleReadsThemeVariables covers the walkthrough's
// converted style block (#173): its dedicated colors are --color-walk-*
// references, the glows compose from variables via color-mix, and the page's
// head injects the walkthrough values for the Public theme.
func TestLanding_WalkthroughStyleReadsThemeVariables(t *testing.T) {
	srv, _ := newTestServer(t, nil, nil)

	body := readBody(t, get(t, srv, "/user"))

	// Dedicated walkthrough variables, referenced in the style block.
	for _, want := range []string{
		"var(--color-walk-gray-600)",
		"var(--color-walk-gray-500)",
		"var(--color-walk-indigo-300)",
		"var(--color-walk-indigo-500)",
		"var(--color-walk-indigo-900)",
		"var(--color-walk-indigo-950)",
		"var(--color-walk-fuchsia-400)",
		"var(--color-walk-emerald-300)",
		"var(--color-walk-amber-400)",
		"var(--color-walk-green-500)",
		"var(--color-walk-green-950)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("landing page: want %q in the walkthrough style block", want)
		}
	}

	// The glows compose from the variables; the injected head carries the
	// default Public theme's walk values alongside its role variables.
	for _, want := range []string{
		"color-mix(in srgb, var(--color-accent-muted) 22%, transparent)",
		"color-mix(in srgb, var(--color-walk-fuchsia-400) 22%, transparent)",
		"color-mix(in srgb, var(--color-walk-indigo-500) 35%, transparent)",
		"--color-walk-indigo-900:#312e81", // Midnight: the Public theme default
	} {
		if !strings.Contains(body, want) {
			t.Errorf("landing page: want %q", want)
		}
	}

	// The two black rgba() shadows stay hardcoded (spec §7, exception 4).
	for _, want := range []string{"rgba(0, 0, 0, 0.5)", "rgba(0, 0, 0, 0.45)"} {
		if !strings.Contains(body, want) {
			t.Errorf("landing page: want the hardcoded shadow %q", want)
		}
	}
}

func TestOnboard_SendsEachPipelineStepToItsPage(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	must(t, deps.events.Reveal()) // after the Reveal, matched Participants see their Match
	seed(t, deps.db, "i", "I", "interviewing")
	seed(t, deps.db, "r", "R", "ready")
	seed(t, deps.db, "m", "M", "ready")
	seed(t, deps.db, "m2", "M2", "ready")
	pair(t, deps.db, "m", "m2")

	if resp := get(t, srv, "/user/onboard/i"); resp.StatusCode != 200 {
		t.Errorf("interviewing: want 200, got %d", resp.StatusCode)
	}
	if loc := get(t, srv, "/user/onboard/r").Header.Get("Location"); loc != "/user/wait/r" {
		t.Errorf("ready: want wait page, got %q", loc)
	}
	if loc := get(t, srv, "/user/onboard/m").Header.Get("Location"); loc != "/user/match/m" {
		t.Errorf("matched: want match page, got %q", loc)
	}
	if resp := get(t, srv, "/user/onboard/nobody"); resp.StatusCode != 404 {
		t.Errorf("unknown: want 404, got %d", resp.StatusCode)
	}
}

func TestOnboard_ShowsTheQuestionWhenTheInterviewAlreadyStarted(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "i", "I", "interviewing") // one unanswered question, as after a reload

	body := readBody(t, get(t, srv, "/user/onboard/i"))

	if strings.Contains(body, "Agent pipeline running") {
		t.Errorf("want the interview, got the preparing placeholder")
	}
	if !strings.Contains(body, "Tabs or spaces?") {
		t.Errorf("want the first question on a settled interview")
	}
}

func TestPipelineStatus_RedirectsOrRendersByStep(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	must(t, deps.events.Reveal()) // after the Reveal, matched Participants see their Match
	seed(t, deps.db, "f", "F", "fetching_github")
	seed(t, deps.db, "r", "R", "ready")
	seed(t, deps.db, "m", "M", "ready")
	seed(t, deps.db, "m2", "M2", "ready")
	pair(t, deps.db, "m", "m2")
	seed(t, deps.db, "done", "D", "interviewing")
	updateAnswers(t, deps.db, "done", map[string]string{"fixed_0": "Tabs"}) // every question answered

	if body := readBody(t, get(t, srv, "/user/pipeline/f")); !strings.Contains(body, "Preparing your interview questions") {
		t.Errorf("fetching_github: want the preparing state")
	}
	if loc := get(t, srv, "/user/pipeline/r").Header.Get("HX-Redirect"); loc != "/user/wait/r" {
		t.Errorf("ready: want HX-Redirect to wait, got %q", loc)
	}
	if loc := get(t, srv, "/user/pipeline/m").Header.Get("HX-Redirect"); loc != "/user/match/m" {
		t.Errorf("matched: want HX-Redirect to match, got %q", loc)
	}
	if loc := get(t, srv, "/user/pipeline/done").Header.Get("HX-Redirect"); loc != "/user/wait/done" {
		t.Errorf("all answered: wait while the persona is crafted, got %q", loc)
	}
	if resp := get(t, srv, "/user/pipeline/nobody"); resp.StatusCode != 404 {
		t.Errorf("unknown: want 404, got %d", resp.StatusCode)
	}
}

func TestWait_ShowsTheAnswersUntilMatched(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	must(t, deps.events.Reveal()) // after the Reveal, matched Participants see their Match
	seed(t, deps.db, "r", "The Gopher", "ready")
	setProfile(t, deps.db, "r", &GitHubProfile{Login: "r", Languages: []string{"Go"}, AccountAgeDays: 730})
	seed(t, deps.db, "m", "M", "ready")
	seed(t, deps.db, "m2", "M2", "ready")
	pair(t, deps.db, "m", "m2")

	body := readBody(t, get(t, srv, "/user/wait/r"))
	if !strings.Contains(body, "Tabs or spaces?") || !strings.Contains(body, "The Gopher") {
		t.Errorf("wait page should show the persona and answers")
	}
	if !strings.Contains(body, "2 yrs") {
		t.Errorf("wait page should show the GitHub account age")
	}
	if loc := get(t, srv, "/user/wait/m").Header.Get("Location"); loc != "/user/match/m" {
		t.Errorf("matched: want match page, got %q", loc)
	}
	if resp := get(t, srv, "/user/wait/nobody"); resp.StatusCode != 404 {
		t.Errorf("unknown: want 404, got %d", resp.StatusCode)
	}
}

// TestWaitPage_RendersThePhaseHeader covers #136: the wait page carries the
// persistent phase header (ADR 0019) as its first card — the progress strip
// of the evening's four phases, the one-line "Tonight: …" summary and the
// Participant's own status — and the wait stream refreshes it on 'refresh'
// like every other fragment, and on the 'phase' event when the host advances
// the evening.
func TestWaitPage_RendersThePhaseHeader(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "r", "The Gopher", "ready")

	body := readBody(t, get(t, srv, "/user/wait/r"))

	// The header is the page's first card, wired for a stream refresh.
	if idx, persona := strings.Index(body, `id="phase-header"`), strings.Index(body, `id="persona-card"`); idx < 0 || idx > persona {
		t.Errorf("the phase header should be the wait page's first card, got header at %d, persona card at %d", idx, persona)
	}
	for _, want := range []string{
		`hx-get="/user/phase-header/r"`,
		`hx-trigger="refresh"`,
		`data-phase="onboarding"`, `data-phase="social"`, `data-phase="showtell"`, `data-phase="closed"`,
		"Onboarding", "Social", "Show &amp; tell", "Closed",
		`aria-current="step"`,
		"Tonight: register, answer the interview, and wait for your match",
		"Interview: 1 of 1 answered",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the wait page's phase header should contain %q", want)
		}
	}

	// The stream refreshes the header on 'refresh' and on the 'phase' event.
	if n := strings.Count(body, `htmx.trigger('#phase-header', 'refresh')`); n != 2 {
		t.Errorf("want the header refreshed by both stream events, got %d triggers", n)
	}
	if !strings.Contains(body, `_es.addEventListener('phase', () => htmx.trigger('#phase-header', 'refresh'));`) {
		t.Errorf("the wait page should listen for the 'phase' event")
	}
}

func TestWaitStatus_returnsCorrectStatus(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	must(t, deps.events.Reveal()) // after the Reveal, matched Participants see their Match
	seed(t, deps.db, "r", "R", "ready")
	seed(t, deps.db, "m", "M", "ready")
	seed(t, deps.db, "m2", "M2", "ready")
	pair(t, deps.db, "m", "m2")

	if resp := get(t, srv, "/user/wait-status/r"); resp.StatusCode != 200 || resp.Header.Get("HX-Redirect") != "" {
		t.Errorf("ready: want the status fragment, got %d", resp.StatusCode)
	}
	if loc := get(t, srv, "/user/wait-status/m").Header.Get("HX-Redirect"); loc != "/user/match/m" {
		t.Errorf("matched: want HX-Redirect to match, got %q", loc)
	}
	if resp := get(t, srv, "/user/wait-status/nobody"); resp.StatusCode != 404 {
		t.Errorf("unknown: want 404, got %d", resp.StatusCode)
	}
}

// TestPersonaCard covers #125: the wait page's persona card is a refreshable
// fragment — the persona is crafted in the background after the last
// interview answer, so the fragment serves a placeholder until it exists.
func TestPersonaCard(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "r", "The Bard", "ready")
	if err := deps.db.CreateParticipant("crafting", "crafting", "Crafting", true); err != nil {
		t.Fatal(err)
	}
	forceStep(deps.db, "crafting", StepCreatingPersona)
	seed(t, deps.db, "m", "M", "ready")
	seed(t, deps.db, "m2", "M2", "ready")
	pair(t, deps.db, "m", "m2")

	// The wait page loads the card as a fragment and refreshes it by push.
	body := readBody(t, get(t, srv, "/user/wait/r"))
	for _, want := range []string{`hx-get="/user/persona/r"`, `htmx.trigger('#persona-card', 'refresh')`} {
		if !strings.Contains(body, want) {
			t.Errorf("the wait page should contain %q", want)
		}
	}

	// A ready Participant sees their persona card.
	body = readBody(t, get(t, srv, "/user/persona/r"))
	for _, want := range []string{`id="persona-card"`, `hx-get="/user/persona/r"`, "The Bard", "Ships things", `src="/qr?data=/user/wait/r&size=120"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the persona card should show %q, got: %s", want, body)
		}
	}

	// A Participant whose persona is still being crafted sees a placeholder.
	body = readBody(t, get(t, srv, "/user/persona/crafting"))
	if !strings.Contains(body, "Crafting your persona") {
		t.Errorf("want the crafting placeholder, got: %s", body)
	}
	if strings.Contains(body, "/qr?data=") {
		t.Error("no profile QR before the persona exists")
	}

	// A matched Participant belongs on the match page once the social begins.
	must(t, deps.events.Reveal())
	if loc := get(t, srv, "/user/persona/m").Header.Get("HX-Redirect"); loc != "/user/match/m" {
		t.Errorf("matched: want HX-Redirect to match, got %q", loc)
	}
	if resp := get(t, srv, "/user/persona/nobody"); resp.StatusCode != 404 {
		t.Errorf("unknown: want 404, got %d", resp.StatusCode)
	}
}

// TestMatchPage_SectionOrder covers #137 and #138: the match page is the
// phase header, then the owner's identity line and the slides — kept high so
// the room finds them — then the phase-content region (the mission card
// first, the meet section next, then the match reveal), then the bottom
// "More" drawer holding the evergreen cards: the dossier, the settings, the
// share-profile QR and the recap link.
func TestMatchPage_SectionOrder(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	pair(t, deps.db, "a", "b")
	must(t, deps.events.Reveal())

	body := readBody(t, get(t, srv, "/user/match/a"))

	markers := []string{
		`id="phase-header"`,
		"This is you:",
		`id="slides"`,
		`id="phase-content"`,
		`id="mission-card"`,
		`id="meet-status"`,
		"It's a match!",
		"Find them in the room",
		`<details`,
		"More",
		`id="dossier"`,
		`id="settings"`,
		`id="qr"`,
		`id="recap"`,
	}
	last := -1
	for _, m := range markers {
		idx := strings.Index(body, m)
		if idx < 0 {
			t.Errorf("the match page should contain %q", m)
			continue
		}
		if idx < last {
			t.Errorf("%q appears out of order on the match page", m)
		}
		last = idx
	}
	// The show & tell sign-up is open from the social on, and the matched
	// Participants on this page must reach it — the sign-up form rides the
	// region in the social and the show & tell alike.
	if !strings.Contains(body, `id="showtell"`) || !strings.Contains(body, "Sign me up") {
		t.Error("the social's match page should carry the show & tell sign-up form")
	}
	// Exactly one anchor per fragment id: each fragment lives in exactly one
	// place — the phase cards in the region, the evergreen cards in the
	// drawer.
	for _, id := range []string{"phase-header", "phase-content", "mission-card", "meet-status", "showtell", "slides", "dossier", "settings", "qr", "recap"} {
		if n := strings.Count(body, `id="`+id+`"`); n != 1 {
			t.Errorf("want exactly one %q anchor on the match page, got %d", id, n)
		}
	}
}

// TestMatchPage_RendersThePhaseHeader covers #136: the match page carries the
// same persistent phase header (ADR 0019) as its first card — a matched
// Participant in the social sees the evening's progress strip, the social's
// summary and their mission status — and the match stream refreshes it on
// 'refresh' like every other fragment, and on the 'phase' event when the host
// advances the evening.
func TestMatchPage_RendersThePhaseHeader(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	pair(t, deps.db, "a", "b")
	must(t, deps.events.Reveal()) // the social begins: matched Participants see their Match

	body := readBody(t, get(t, srv, "/user/match/a"))

	// The header is the page's first card, above the match hero.
	if idx, hero := strings.Index(body, `id="phase-header"`), strings.Index(body, "It's a match!"); idx < 0 || idx > hero {
		t.Errorf("the phase header should be the match page's first card, got header at %d, hero at %d", idx, hero)
	}
	for _, want := range []string{
		`hx-get="/user/phase-header/a"`,
		`hx-trigger="refresh"`,
		`data-phase="onboarding"`, `data-phase="social"`, `data-phase="showtell"`, `data-phase="closed"`,
		"Onboarding", "Social", "Show &amp; tell", "Closed",
		`aria-current="step"`,
		"Tonight: the matches are revealed — meet your match, hunt your mission card, sign up for show &amp; tell",
		"Mission: your card is being prepared",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the match page's phase header should contain %q", want)
		}
	}

	// The stream refreshes the header on 'refresh' and on the 'phase' event,
	// which additionally swaps the phase-content region.
	if n := strings.Count(body, `htmx.trigger('#phase-header', 'refresh')`); n != 2 {
		t.Errorf("want the header refreshed by both stream events, got %d triggers", n)
	}
	phase := body[strings.Index(body, `_mes.addEventListener('phase'`):]
	for _, want := range []string{
		`htmx.trigger('#phase-header', 'refresh')`,
		`htmx.trigger('#phase-content', 'phase')`,
	} {
		if !strings.Contains(phase, want) {
			t.Errorf("the 'phase' event should refresh the header and swap the region with %q, got %q", want, phase)
		}
	}
}

// TestMatchPage_ShowsThePartnerAndTheLockedDossier covers the Reveal: the
// Match page shows the partner's full persona card and the "find them in the
// room" panel — personas only, never the partner's handle — inside the
// phase-content region, while the dossier (reason, flags, icebreakers) starts
// out locked, and Explore's "Others at the event" section is gone.
func TestMatchPage_ShowsThePartnerAndTheLockedDossier(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	must(t, deps.events.Reveal()) // after the Reveal, matched Participants see their Match
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	seed(t, deps.db, "c", "The Snake", "ready")
	pair(t, deps.db, "a", "b")

	body := readBody(t, get(t, srv, "/user/match/a"))
	for _, want := range []string{"The Crab", "Ships things", "91%", "Find them in the room", "🔒", "to unlock"} {
		if !strings.Contains(body, want) {
			t.Errorf("match page should contain %q", want)
		}
	}
	// The reveal renders inside the phase-content region.
	if region, hero := strings.Index(body, `id="phase-content"`), strings.Index(body, "It's a match!"); region < 0 || region > hero {
		t.Errorf("the match reveal should render inside the phase-content region, got region at %d, hero at %d", region, hero)
	}
	for _, absent := range []string{"@b", "Both love tabs", "hogs the whiteboard", "Why tabs?", "The Snake", "Others at the event", "/user/explore"} {
		if strings.Contains(body, absent) {
			t.Errorf("match page must not contain %q while the dossier is locked", absent)
		}
	}
	if loc := get(t, srv, "/user/match/c").Header.Get("Location"); loc != "/user/wait/c" {
		t.Errorf("unmatched: want wait page, got %q", loc)
	}
	if resp := get(t, srv, "/user/match/nobody"); resp.StatusCode != 404 {
		t.Errorf("unknown: want 404, got %d", resp.StatusCode)
	}
}

func TestBigScreen_GraphHasMatchedAndPotentialEdges(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")
	seed(t, deps.db, "b", "B", "ready")
	seed(t, deps.db, "c", "C", "ready")
	pair(t, deps.db, "a", "b")

	var graph struct {
		Nodes []graphNode `json:"nodes"`
		Edges []graphEdge `json:"edges"`
	}
	if err := json.Unmarshal([]byte(readBody(t, get(t, srv, "/bigscreen/graph-data"))), &graph); err != nil {
		t.Fatal(err)
	}

	if len(graph.Nodes) != 3 {
		t.Errorf("nodes: want 3, got %d", len(graph.Nodes))
	}
	var matched, potential int
	for _, e := range graph.Edges {
		if e.Matched {
			matched++
			if e.Score != 91 {
				t.Errorf("matched edge score: want 91, got %d", e.Score)
			}
		} else {
			potential++
		}
	}
	if matched != 1 || potential != 2 {
		t.Errorf("want 1 matched edge and 2 potential edges (C to A and B), got %d and %d", matched, potential)
	}
	if resp := get(t, srv, "/bigscreen"); resp.StatusCode != 200 {
		t.Errorf("big screen: want 200, got %d", resp.StatusCode)
	}
}

func TestAdminRematch_PairsTheReadyParticipants(t *testing.T) {
	llm := newFakeLLM().on("matchmaker", `{"score": 70, "reason": "fine"}`)
	srv, deps := newTestServer(t, llm, nil)
	seed(t, deps.db, "a", "A", "ready")
	seed(t, deps.db, "b", "B", "ready")

	resp := post(t, srv, "/admin/rematch", nil)

	// The host lands back on the console with a confirmation notice (#127).
	loc := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(loc, "/admin?notice=") {
		t.Errorf("want a redirect to the admin console with a notice, got %d %q", resp.StatusCode, loc)
	}
	eventually(t, "both matched", func() bool {
		return reload(t, deps.db, "a").MatchedWith == "b" && reload(t, deps.db, "b").MatchedWith == "a"
	})
}

// TestAdminRematch_IsRejectedOnceMatchingIsFrozen covers #115 and #127: from
// the social onward the endpoint rejects the rematch — the host lands back on
// the console with a notice explaining why, no matching runs and the frozen
// pairs are left exactly as they were.
func TestAdminRematch_IsRejectedOnceMatchingIsFrozen(t *testing.T) {
	llm := newFakeLLM().on("matchmaker", `{"score": 70, "reason": "fine"}`)
	srv, deps := newTestServer(t, llm, nil)
	seed(t, deps.db, "a", "A", "ready")
	seed(t, deps.db, "b", "B", "ready")
	seed(t, deps.db, "c", "C", "ready")
	seed(t, deps.db, "d", "D", "ready")
	pair(t, deps.db, "a", "b")
	pair(t, deps.db, "c", "d")
	if err := deps.events.Advance(PhaseSocial); err != nil {
		t.Fatal(err)
	}

	resp := post(t, srv, "/admin/rematch", nil)

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("from the social onward: want a redirect, got %d", resp.StatusCode)
	}
	if loc, _ := url.QueryUnescape(resp.Header.Get("Location")); !strings.Contains(loc, "Matching is frozen") {
		t.Errorf("the rejection should say why, got %q", loc)
	}
	// No matching ran: the pairs are final and nothing was logged.
	if n := llm.callsMatching("matchmaker"); n != 0 {
		t.Errorf("a rejected rematch must not assess any pair, got %d calls", n)
	}
	for id, want := range map[string]string{"a": "b", "b": "a", "c": "d", "d": "c"} {
		if got := reload(t, deps.db, id).MatchedWith; got != want {
			t.Errorf("%s matched with %q, want %q", id, got, want)
		}
	}
	if msgs, _ := deps.db.GetRecentActivity(10); strings.Contains(strings.Join(msgs, "\n"), "ematch") {
		t.Errorf("a rejected rematch should log nothing about rematching, got %v", msgs)
	}
}

// TestAdmin_NoticeIsRendered covers #127: a notice query parameter (set by
// host actions like the rematch) is shown on the console, HTML-escaped.
func TestAdmin_NoticeIsRendered(t *testing.T) {
	srv, _ := newTestServer(t, nil, nil)

	if body := readBody(t, get(t, srv, "/admin")); strings.Contains(body, "border-indigo-700") {
		t.Error("no notice box without the query parameter")
	}

	notice := "🔄 Rematch started — <b>the matchmaker</b>"
	body := readBody(t, get(t, srv, "/admin?notice="+url.QueryEscape(notice)))
	if !strings.Contains(body, "🔄 Rematch started — &lt;b&gt;the matchmaker&lt;/b&gt;") {
		t.Errorf("the notice should be rendered HTML-escaped, got: %s", body)
	}
}

func TestRematch_NeedsTwoReadyParticipants(t *testing.T) {
	_, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")

	if err := deps.matchmaking.Rematch(); err == nil {
		t.Error("want error with a single ready participant")
	}
}

func TestAdminRevealAndDelete(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready")

	if loc := post(t, srv, "/admin/reveal", nil).Header.Get("Location"); loc != "/admin" {
		t.Errorf("reveal: want redirect to admin, got %q", loc)
	}

	req, _ := http.NewRequest("DELETE", srv.URL+"/data/participant/a", nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != 200 {
		t.Fatalf("delete: want 200, got %d", resp.StatusCode)
	}
	if _, err := deps.db.GetParticipant("a"); err == nil {
		t.Error("participant should be gone after delete")
	}
}

func TestDeletingAMatchedParticipant_LeavesNoTraceOnTheirPartnerOrTheBigScreen(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	must(t, deps.events.Reveal())
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")
	pair(t, deps.db, "a", "b")

	req, _ := http.NewRequest("DELETE", srv.URL+"/data/participant/a", nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != 200 {
		t.Fatalf("delete: want 200, got %d", resp.StatusCode)
	}

	if loc := get(t, srv, "/user/match/b").Header.Get("Location"); loc != "/user/wait/b" {
		t.Errorf("the partner's Match page should be gone, got redirect %q", loc)
	}
	var graph struct {
		Edges []graphEdge `json:"edges"`
	}
	must(t, json.Unmarshal([]byte(readBody(t, get(t, srv, "/bigscreen/graph-data"))), &graph))
	for _, e := range graph.Edges {
		if e.Source == "a" || e.Target == "a" {
			t.Errorf("Big Screen still has an edge to the deleted Participant: %+v", e)
		}
	}
}

func TestNonGitHubUser_NoScreenShowsTheGeneratedHandle(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	must(t, deps.events.Reveal())
	must(t, deps.db.CreateParticipant("ada", "no-github-1234abcd", "Ada Lovelace", false))
	setProfile(t, deps.db, "ada", &GitHubProfile{})
	setPersona(t, deps.db, "ada", "The Analyst", "Computes")
	forceStep(deps.db, "ada", "ready")
	seed(t, deps.db, "octo", "The Gopher", "ready")
	pair(t, deps.db, "ada", "octo")
	must(t, deps.db.CreateParticipant("zed", "no-github-5678efgh", "Zed", false))
	forceQuestions(deps.db, "zed", ExtraQuestions)
	forceStep(deps.db, "zed", "interviewing")

	for _, path := range []string{"/user/onboard/zed", "/user/match/ada", "/user/match/octo", "/data", "/bigscreen/graph-data"} {
		resp := get(t, srv, path)
		if resp.StatusCode != 200 {
			t.Errorf("%s: status %d", path, resp.StatusCode)
			continue
		}
		if body := readBody(t, resp); strings.Contains(body, "no-github-") {
			t.Errorf("%s shows the generated handle", path)
		}
	}
	// The Big Screen shows persona names only, in every phase — never the
	// Non-GitHub User's real name or generated handle (#113's privacy baseline).
	if body := readBody(t, get(t, srv, "/bigscreen/graph-data")); strings.Contains(body, "Ada Lovelace") || !strings.Contains(body, "The Analyst") {
		t.Errorf("the Big Screen should show the persona, not the name:\n%s", body)
	}
}

// TestLanding_ExplainsWhatTheAppIsAndWhyTheQuestions covers the landing
// page's one-paragraph answer to "what is this and why the questions": what
// happens tonight, what the answers are for, and the anonymity promise.
func TestLanding_ExplainsWhatTheAppIsAndWhyTheQuestions(t *testing.T) {
	srv, _ := newTestServer(t, nil, nil)

	body := readBody(t, get(t, srv, "/user"))
	for _, want := range []string{
		"anonymous persona",
		"technical soulmate",
		"Why the questions?",
		"never appears on the big screen",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the landing page should contain %q", want)
		}
	}
}

// TestOnboardPage_ExplainsItselfWhileWaitingAndInterviewing covers the onboarding page's header:
// while the agents prepare the interview it says so and counts the wait; once
// the interview runs it says why the questions are asked.
func TestOnboardPage_ExplainsItselfWhileWaitingAndInterviewing(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	must(t, deps.db.CreateParticipant("prep", "prep", "Prep", true))
	forceStep(deps.db, "prep", StepFetchingGitHub)
	seed(t, deps.db, "iv", "The Bard", "interviewing")

	// The assertions target the rendered header elements — the page's script
	// carries both variants' strings as literals.
	body := readBody(t, get(t, srv, "/user/onboard/prep"))
	for _, want := range []string{
		`<h1 class="text-2xl font-black" id="onboard-title">The agents are on it</h1>`,
		`id="onboard-sub">Fetching your profile and preparing your questions`,
		`id="wait-elapsed"`,
		"Agent pipeline running...",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the preparing onboard page should contain %q", want)
		}
	}
	if strings.Contains(body, `id="onboard-title">Quick interview<`) {
		t.Error("the preparing onboard page should not claim the interview has started")
	}

	body = readBody(t, get(t, srv, "/user/onboard/iv"))
	for _, want := range []string{
		`<h1 class="text-2xl font-black" id="onboard-title">Quick interview</h1>`,
		`id="onboard-sub">A few quick questions — your answers decide who you meet tonight`,
		"data-question",
		"Tabs or spaces?",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the interviewing onboard page should contain %q", want)
		}
	}
	if strings.Contains(body, `id="onboard-title">The agents are on it<`) {
		t.Error("the interviewing onboard page should not claim the agents are still preparing")
	}
}

// TestAgentQueue_ShowsThePositionWhileWaiting covers the wait feedback: while
// Participants wait on the agents, the pipeline-step fragment and the wait
// status name how many registered before them are still waiting too — a busy
// night reads as a queue, not a hang.
func TestAgentQueue_ShowsThePositionWhileWaiting(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	must(t, deps.db.CreateParticipant("first", "first", "First", true))
	forceStep(deps.db, "first", StepFetchingGitHub)
	must(t, deps.db.CreateParticipant("second", "second", "Second", true))
	forceStep(deps.db, "second", StepFetchingGitHub)

	if body := readBody(t, get(t, srv, "/user/pipeline/second")); !strings.Contains(body, "1 person ahead of you in the agent queue") {
		t.Errorf("the pipeline fragment should show the queue position, got:\n%s", body)
	}
	if body := readBody(t, get(t, srv, "/user/pipeline/first")); strings.Contains(body, "ahead of you in the agent queue") {
		t.Errorf("the first in line should see nobody ahead, got:\n%s", body)
	}

	// The wait page's status carries the queue line too, while the persona is
	// being crafted.
	forceStep(deps.db, "second", StepCreatingPersona)
	if body := readBody(t, get(t, srv, "/user/wait-status/second")); !strings.Contains(body, "1 person ahead of you in the agent queue") {
		t.Errorf("the wait status should show the queue position, got:\n%s", body)
	}
}

// TestPersonalPages_SayThisIsYou covers the identity line: the personal pages
// are the one place a Participant finds their real name again — the persona
// card on the wait page, a line under the header on the Match page — while
// the meet page shows the target's persona only, never their real name.
func TestPersonalPages_SayThisIsYou(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	must(t, deps.db.CreateParticipant("ada", "ada", "Ada Lovelace", true))
	setProfile(t, deps.db, "ada", &GitHubProfile{Login: "ada", Languages: []string{"Go"}})
	setPersona(t, deps.db, "ada", "The Analyst", "Computes")
	forceStep(deps.db, "ada", StepReady)
	must(t, deps.db.CreateParticipant("octo", "octo", "Octavia", true))
	setProfile(t, deps.db, "octo", &GitHubProfile{Login: "octo", Languages: []string{"Go"}})
	setPersona(t, deps.db, "octo", "The Gopher", "Ships things")
	forceStep(deps.db, "octo", StepReady)
	pair(t, deps.db, "ada", "octo")
	must(t, deps.events.Reveal())

	// The Match page names the owner — the persona on the big screen maps
	// back to a person.
	body := readBody(t, get(t, srv, "/user/match/ada"))
	for _, want := range []string{"This is you:", "Ada Lovelace", "@ada"} {
		if !strings.Contains(body, want) {
			t.Errorf("the match page should contain %q", want)
		}
	}
	if strings.Contains(body, "Octavia") {
		t.Error("the match page must not show the partner's real name")
	}

	// The wait page's persona card names the owner too — an unmatched
	// Participant belongs there once the social begins.
	must(t, deps.db.CreateParticipant("zed", "zed", "Zed", true))
	setProfile(t, deps.db, "zed", &GitHubProfile{Login: "zed", Languages: []string{"Go"}})
	setPersona(t, deps.db, "zed", "The Lone Wolf", "Lurks")
	forceStep(deps.db, "zed", StepReady)
	if body := readBody(t, get(t, srv, "/user/wait/zed")); !strings.Contains(body, "This is you:") || !strings.Contains(body, "Zed") {
		t.Errorf("the wait page's persona card should say this is you, got:\n%s", body)
	}

	// The meet page shows personas only — neither side's real name leaves
	// their own page.
	if body := readBody(t, withCookie(t, srv, "/user/m/octo", "ada")); strings.Contains(body, "Ada Lovelace") || strings.Contains(body, "Octavia") {
		t.Errorf("the meet page must not show real names, got:\n%s", body)
	}

	// A Non-GitHub User sees their name only — the generated handle never shows.
	must(t, deps.db.CreateParticipant("grace", "no-github-1234abcd", "Grace Hopper", false))
	setProfile(t, deps.db, "grace", &GitHubProfile{})
	setPersona(t, deps.db, "grace", "The Compiler", "Compiles")
	forceStep(deps.db, "grace", StepReady)
	body = readBody(t, get(t, srv, "/user/wait/grace"))
	if !strings.Contains(body, "This is you:") || !strings.Contains(body, "Grace Hopper") {
		t.Errorf("the wait page should say this is you for a Non-GitHub User, got:\n%s", body)
	}
	if strings.Contains(body, "no-github-") {
		t.Error("the generated handle must never show")
	}
}

// TestAdmin_ShowsTheAgentQueueLength covers the host console's view of the
// agent queue: how many Participants are waiting on the agents tonight, so
// the host can explain a busy pipeline to the room.
func TestAdmin_ShowsTheAgentQueueLength(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	must(t, deps.db.CreateParticipant("a", "a", "A", true))
	forceStep(deps.db, "a", StepFetchingGitHub)
	seed(t, deps.db, "b", "The Gopher", "ready")

	if body := readBody(t, get(t, srv, "/admin")); !strings.Contains(body, "Waiting for the agents") {
		t.Errorf("the admin console should show the agent queue length, got:\n%s", body)
	}
}
