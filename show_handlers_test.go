package main

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestHostConsoleState_RendersTheFragment(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	if err := deps.db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}

	body := readBody(t, get(t, srv, "/admin/host/state"))

	if !strings.Contains(body, "Lobby") || !strings.Contains(body, "Start the show") {
		t.Errorf("want the Lobby state fragment, got:\n%s", body)
	}
}

func TestShowStatus_UnknownParticipantIs404(t *testing.T) {
	srv, _ := newTestServer(t, nil, nil)

	if resp := get(t, srv, "/user/show/nobody"); resp.StatusCode != 404 {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

func TestShowStatusStream_UnknownParticipantIs404(t *testing.T) {
	srv, _ := newTestServer(t, nil, nil)

	if resp := get(t, srv, "/user/show-stream/nobody"); resp.StatusCode != 404 {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

func TestShowStatusStream_RedirectsImmediatelyWhenAlreadyElsewhere(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	seed(t, deps.db, "a", "A", "ready") // Classic flow: belongs on the wait page, not the show status page

	s := open(t, srv, "/user/show-stream/a")

	waitFor(t, s, "data: /user/wait/a")
}

func TestHostConsole_ShowsTheSegmentAndTheNextAction(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	if err := deps.db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}

	body := readBody(t, get(t, srv, "/admin/host"))

	if !strings.Contains(body, "Lobby") {
		t.Errorf("want the Lobby shown, got:\n%s", body)
	}
	if !strings.Contains(body, "Start the show") {
		t.Errorf("want the next action \"Start the show\", got:\n%s", body)
	}
}

func TestHostConsole_InactiveInTheClassicFlow(t *testing.T) {
	srv, _ := newTestServer(t, nil, nil)

	body := readBody(t, get(t, srv, "/admin/host"))

	if !strings.Contains(body, "inactive") {
		t.Errorf("want the console to say the Show is inactive, got:\n%s", body)
	}
}

func TestAdvanceShow_MovesToTheNextSegment(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	if err := deps.db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}

	if loc := post(t, srv, "/admin/host/advance", nil).Header.Get("Location"); loc != "/admin/host" {
		t.Errorf("want redirect to the host console, got %q", loc)
	}

	body := readBody(t, get(t, srv, "/admin/host"))
	if !strings.Contains(body, "Round 1 of 6") {
		t.Errorf("want Round 1 shown after advancing once, got:\n%s", body)
	}
}

func TestAdvanceShow_RejectedInTheClassicFlow(t *testing.T) {
	srv, _ := newTestServer(t, nil, nil)

	resp := post(t, srv, "/admin/host/advance", nil)

	if resp.StatusCode != 400 {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

// TestHostConsole_ShowsTheBackgroundAssessmentProgress covers #81: the host
// console shows how many candidate Pairs have been assessed so far, before
// the matchmaking segment.
func TestHostConsole_ShowsTheBackgroundAssessmentProgress(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	if err := deps.db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	seed(t, deps.db, "a", "The Gopher", "ready")
	seed(t, deps.db, "b", "The Crab", "ready")

	body := readBody(t, get(t, srv, "/admin/host"))

	if !strings.Contains(body, "0 of 1 Pairs assessed") {
		t.Errorf("want the assessment progress shown, got:\n%s", body)
	}
}

// TestHostConsole_ShowsTheRoundCountdownAndAnswerCount covers #79: while a
// Round is open, the host console shows the countdown and answer count
// instead of the next-action button, plus a way to close the Round early.
func TestHostConsole_ShowsTheRoundCountdownAndAnswerCount(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	if err := deps.db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	deps.db.CreateParticipant("p", "p", "P", true)
	post(t, srv, "/admin/host/advance", nil) // -> Round 0

	body := readBody(t, get(t, srv, "/admin/host"))
	if !strings.Contains(body, "15s") {
		t.Errorf("want the countdown shown, got:\n%s", body)
	}
	if !strings.Contains(body, "0 answered") {
		t.Errorf("want the answer count shown, got:\n%s", body)
	}
	if !strings.Contains(body, "Close Round early") {
		t.Errorf("want a way to close the Round early, got:\n%s", body)
	}

	post(t, srv, "/user/round-answer/p", url.Values{"answer": {"Tabs"}})

	body = readBody(t, get(t, srv, "/admin/host"))
	if !strings.Contains(body, "1 answered") {
		t.Errorf("want the answer count updated, got:\n%s", body)
	}
}

func TestCloseRoundEarly_ClosesTheOpenRound(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	if err := deps.db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	post(t, srv, "/admin/host/advance", nil) // -> Round 0

	if loc := post(t, srv, "/admin/host/close-round", nil).Header.Get("Location"); loc != "/admin/host" {
		t.Errorf("want redirect to the host console, got %q", loc)
	}

	body := readBody(t, get(t, srv, "/admin/host"))
	if strings.Contains(body, "Close Round early") {
		t.Errorf("want the Round no longer open, got:\n%s", body)
	}
	if !strings.Contains(body, "Open Round 2") {
		t.Errorf("want the next action offered again, got:\n%s", body)
	}
}

func TestCloseRoundEarly_RejectedInTheClassicFlow(t *testing.T) {
	srv, _ := newTestServer(t, nil, nil)

	if resp := post(t, srv, "/admin/host/close-round", nil); resp.StatusCode != 400 {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

// TestShowStatus_ShowsTheAnswerPadWhileARoundIsOpen and
// TestSubmitRoundAnswer_RecordsTheAnswerAndReRendersThePad cover #79: a
// Participant answers the current Round's Fixed Question from the show
// status page, and can change their answer while the Round stays open.
func TestShowStatus_ShowsTheAnswerPadWhileARoundIsOpen(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	if err := deps.db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	seed(t, deps.db, "p", "P", "ready")
	post(t, srv, "/admin/host/advance", nil) // -> Round 0

	body := readBody(t, get(t, srv, "/user/show/p"))

	if !strings.Contains(body, FixedQuestions[0].Text) {
		t.Errorf("want the current Round's Question shown, got:\n%s", body)
	}
	if !strings.Contains(body, "15") {
		t.Errorf("want the countdown shown, got:\n%s", body)
	}
}

func TestSubmitRoundAnswer_RecordsTheAnswerAndReRendersThePad(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	if err := deps.db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	deps.db.CreateParticipant("p", "p", "P", true)
	post(t, srv, "/admin/host/advance", nil) // -> Round 0

	body := readBody(t, post(t, srv, "/user/round-answer/p", url.Values{"answer": {"Tabs"}}))

	if !strings.Contains(body, "id=\"round-answer\"") {
		t.Errorf("want the answer pad re-rendered, got:\n%s", body)
	}
	p, err := NewParticipantStore(deps.db).Get("p")
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Answers[FixedQuestions[0].ID]; got != "Tabs" {
		t.Errorf("want the answer recorded, got %q", got)
	}
}

// TestShowStatus_RendersSwipeCardsForATwoOptionRound and
// TestShowStatus_RendersAnswerPadsForOtherRounds cover #80: a two-option
// Round renders as a swipe card (with tap buttons as a fallback), while
// every other Round keeps the colour answer pads, tested through the mux.
func TestShowStatus_RendersSwipeCardsForATwoOptionRound(t *testing.T) {
	original := FixedQuestions
	FixedQuestions = []Question{{ID: "fixed_0", Text: "Tabs or spaces?", Options: []string{"Tabs", "Spaces"}}}
	t.Cleanup(func() { FixedQuestions = original })

	srv, deps := newTestServer(t, nil, nil)
	if err := deps.db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	seed(t, deps.db, "p", "P", "ready")
	post(t, srv, "/admin/host/advance", nil) // -> Round 0

	body := readBody(t, get(t, srv, "/user/show/p"))

	if !strings.Contains(body, `id="swipe-card"`) {
		t.Errorf("want a swipe card for a two-option Round, got:\n%s", body)
	}
	if !strings.Contains(body, "Tabs") || !strings.Contains(body, "Spaces") {
		t.Errorf("want both options offered as tap fallbacks, got:\n%s", body)
	}
}

func TestShowStatus_RendersAnswerPadsForOtherRounds(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	if err := deps.db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	seed(t, deps.db, "p", "P", "ready")
	post(t, srv, "/admin/host/advance", nil) // -> Round 0: three options, not two

	body := readBody(t, get(t, srv, "/user/show/p"))

	if strings.Contains(body, `id="swipe-card"`) {
		t.Errorf("want answer pads, not a swipe card, for a three-option Round, got:\n%s", body)
	}
}

// TestSwipeRoundAnswer_RecordsTheAnswerLikeATap covers #80: swiping a card
// posts the same form as tapping — the mux can't tell them apart, and
// shouldn't need to.
func TestSwipeRoundAnswer_RecordsTheAnswerLikeATap(t *testing.T) {
	original := FixedQuestions
	FixedQuestions = []Question{{ID: "fixed_0", Text: "Tabs or spaces?", Options: []string{"Tabs", "Spaces"}}}
	t.Cleanup(func() { FixedQuestions = original })

	srv, deps := newTestServer(t, nil, nil)
	if err := deps.db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	deps.db.CreateParticipant("p", "p", "P", true)
	post(t, srv, "/admin/host/advance", nil) // -> Round 0

	post(t, srv, "/user/round-answer/p", url.Values{"answer": {"Spaces"}}) // what a right-swipe posts

	p, err := NewParticipantStore(deps.db).Get("p")
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Answers["fixed_0"]; got != "Spaces" {
		t.Errorf("want the swiped answer recorded, got %q", got)
	}
}

func TestSubmitRoundAnswer_UnknownParticipantIs404(t *testing.T) {
	srv, _ := newTestServer(t, nil, nil)

	if resp := post(t, srv, "/user/round-answer/nobody", url.Values{"answer": {"Tabs"}}); resp.StatusCode != 404 {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

func TestSubmitRoundAnswer_RejectedOutsideAnOpenRound(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	deps.db.CreateParticipant("p", "p", "P", true) // Classic flow: the Show is inactive

	if resp := post(t, srv, "/user/round-answer/p", url.Values{"answer": {"Tabs"}}); resp.StatusCode != 400 {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

// TestAdvanceShow_EnteringMatchmakingPairsEveryoneThroughTheMux covers #82:
// advancing all the way into the matchmaking segment pairs every ready
// Participant, through the mux.
func TestAdvanceShow_EnteringMatchmakingPairsEveryoneThroughTheMux(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	if err := deps.db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	seed(t, deps.db, "a", "A", "ready")
	seed(t, deps.db, "b", "B", "ready")

	for i := 0; i < TotalFixedQuestions; i++ {
		post(t, srv, "/admin/host/advance", nil)     // open Round i
		post(t, srv, "/admin/host/close-round", nil) // close it early
	}
	post(t, srv, "/admin/host/advance", nil) // -> matchmaking

	a, b := reload(t, deps.db, "a"), reload(t, deps.db, "b")
	if a.MatchedWith != "b" || b.MatchedWith != "a" {
		t.Errorf("want a and b paired, got a.MatchedWith=%q b.MatchedWith=%q", a.MatchedWith, b.MatchedWith)
	}

	body := readBody(t, get(t, srv, "/admin/host"))
	if !strings.Contains(body, "Matchmaking") {
		t.Errorf("want the Matchmaking segment shown, got:\n%s", body)
	}
}

// TestReveal_HidesIdentitiesUntilEachPairsTurn covers #83: the stage output
// (the Big Screen) never names a Participant before their pair's turn, and
// names them (and the pair stays named) from their turn on.
func TestReveal_HidesIdentitiesUntilEachPairsTurn(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	if err := deps.db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"avery", "blair", "cody", "drew"} {
		seed(t, deps.db, id, id, "ready")
	}

	for i := 0; i < TotalFixedQuestions; i++ {
		post(t, srv, "/admin/host/advance", nil)
		post(t, srv, "/admin/host/close-round", nil)
	}
	post(t, srv, "/admin/host/advance", nil) // -> matchmaking: pairs everyone
	post(t, srv, "/admin/host/advance", nil) // -> reveal

	graphData := func() map[string]any {
		var v map[string]any
		if err := json.Unmarshal([]byte(readBody(t, get(t, srv, "/bigscreen/graph-data"))), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	revealPairIDs := func(v map[string]any) []string {
		raw, _ := v["reveal_pair_ids"].([]any)
		var ids []string
		for _, id := range raw {
			ids = append(ids, id.(string))
		}
		return ids
	}
	all := []string{"avery", "blair", "cody", "drew"}
	revealedNames := func(v map[string]any) map[string]bool {
		names := map[string]bool{}
		for _, n := range v["nodes"].([]any) {
			node := n.(map[string]any)
			if h, ok := node["handle"].(string); ok && h != "" {
				names[node["id"].(string)] = true
			}
		}
		return names
	}

	first := revealPairIDs(graphData())
	if len(first) == 0 {
		t.Fatal("want a pair on stage")
	}
	revealed := revealedNames(graphData())
	for _, id := range all {
		want := false
		for _, r := range first {
			if r == id {
				want = true
			}
		}
		if revealed[id] != want {
			t.Errorf("id %s: want revealed=%v before the second pair's turn, got %v", id, want, revealed[id])
		}
	}

	post(t, srv, "/admin/host/skip-reveal", nil)

	revealed = revealedNames(graphData())
	for _, id := range all {
		if !revealed[id] {
			t.Errorf("id %s: want revealed once every pair has had its turn", id)
		}
	}
}

// TestSkipReveal_MovesToTheNextPair and TestSkipReveal_RejectedOutsideTheReveal
// cover #83: the Host can skip the Reveal ahead, but only while it's playing.
// TestHostConsoleStream_RefreshesWhenTheRevealAdvancesOnItsOwnClock covers
// #83's "tests drive the Reveal with the injected clock... and the
// streams": the stream notices a pair change the injected clock alone
// produced, with no Host action, once the heartbeat re-checks it.
func TestHostConsoleStream_RefreshesWhenTheRevealAdvancesOnItsOwnClock(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	if err := deps.db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "c", "d"} {
		seed(t, deps.db, id, id, "ready")
	}
	for i := 0; i < TotalFixedQuestions; i++ {
		post(t, srv, "/admin/host/advance", nil)
		post(t, srv, "/admin/host/close-round", nil)
	}
	post(t, srv, "/admin/host/advance", nil) // -> matchmaking
	post(t, srv, "/admin/host/advance", nil) // -> reveal: pair 1 of 2

	s := open(t, srv, "/admin/host/stream")
	sentNothingNew(t, s, 1, "", "nothing changed")

	deps.showClock.Advance(DefaultRevealPairDuration) // the clock alone: no Host click, no skip
	srv.clock.fire()                                  // the heartbeat re-checks Show.State, which observes it

	waitFor(t, s, "event: refresh")
}

func TestSkipReveal_MovesToTheNextPair(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	if err := deps.db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "c", "d"} {
		seed(t, deps.db, id, id, "ready")
	}
	for i := 0; i < TotalFixedQuestions; i++ {
		post(t, srv, "/admin/host/advance", nil)
		post(t, srv, "/admin/host/close-round", nil)
	}
	post(t, srv, "/admin/host/advance", nil) // -> matchmaking
	post(t, srv, "/admin/host/advance", nil) // -> reveal, pair 1 of 2

	if loc := post(t, srv, "/admin/host/skip-reveal", nil).Header.Get("Location"); loc != "/admin/host" {
		t.Errorf("want redirect to the host console, got %q", loc)
	}

	body := readBody(t, get(t, srv, "/admin/host"))
	if !strings.Contains(body, "Pair 2 of 2") {
		t.Errorf("want pair 2 of 2 shown, got:\n%s", body)
	}
}

func TestSkipReveal_RejectedOutsideTheReveal(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	if err := deps.db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}

	if resp := post(t, srv, "/admin/host/skip-reveal", nil); resp.StatusCode != 400 {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

// TestMatch_ShowsStandUpInAGameShowOnceThePairsTurnComes and
// TestShowStatus_ShowsTheWildcardMessageAtTheirTurn cover #83: both phones
// pop at their pair's turn, and the wildcard sees their own message.
func TestMatch_ShowsStandUpInAGameShowOnceThePairsTurnComes(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	if err := deps.db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		seed(t, deps.db, id, id, "ready")
	}
	for i := 0; i < TotalFixedQuestions; i++ {
		post(t, srv, "/admin/host/advance", nil)
		post(t, srv, "/admin/host/close-round", nil)
	}
	post(t, srv, "/admin/host/advance", nil) // -> matchmaking: a-b paired
	post(t, srv, "/admin/host/advance", nil) // -> reveal: the only pair is on stage immediately

	if loc := get(t, srv, "/user/show/a").Header.Get("Location"); loc != "/user/match/a" {
		t.Errorf("want a redirected to the match page at their turn, got %q", loc)
	}
	body := readBody(t, get(t, srv, "/user/match/a"))
	if !strings.Contains(body, "Stand up!") {
		t.Errorf("want \"Stand up!\" shown, got:\n%s", body)
	}
}

// TestMatch_OthersListWaitsForTheAfterpartyInAGameShow covers #83: the
// Others-at-the-event list would leak not-yet-revealed identities, so it's
// withheld for the whole Reveal segment, even on a page whose own pair is
// already revealed.
func TestMatch_OthersListWaitsForTheAfterpartyInAGameShow(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	if err := deps.db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "c", "d"} {
		seed(t, deps.db, id, id, "ready")
	}
	for i := 0; i < TotalFixedQuestions; i++ {
		post(t, srv, "/admin/host/advance", nil)
		post(t, srv, "/admin/host/close-round", nil)
	}
	post(t, srv, "/admin/host/advance", nil) // -> matchmaking
	post(t, srv, "/admin/host/advance", nil) // -> reveal

	var revealedID string
	for _, id := range []string{"a", "b", "c", "d"} {
		if loc := get(t, srv, "/user/show/"+id).Header.Get("Location"); loc == "/user/match/"+id {
			revealedID = id
			break
		}
	}
	if revealedID == "" {
		t.Fatal("want somebody's pair revealed immediately")
	}

	body := readBody(t, get(t, srv, "/user/match/"+revealedID))
	if strings.Contains(body, "Others at the event") {
		t.Errorf("want the Others list withheld during the Reveal, got:\n%s", body)
	}
}

func TestShowStatus_ShowsTheWildcardMessageAtTheirTurn(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	if err := deps.db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "c"} { // odd: leaves a wildcard
		seed(t, deps.db, id, id, "ready")
	}
	for i := 0; i < TotalFixedQuestions; i++ {
		post(t, srv, "/admin/host/advance", nil)
		post(t, srv, "/admin/host/close-round", nil)
	}
	post(t, srv, "/admin/host/advance", nil) // -> matchmaking: one pair + a wildcard
	post(t, srv, "/admin/host/advance", nil) // -> reveal

	var wildcard string
	for _, id := range []string{"a", "b", "c"} {
		if reload(t, deps.db, id).Wildcard {
			wildcard = id
		}
	}
	if wildcard == "" {
		t.Fatal("want a wildcard among an odd number of Participants")
	}

	// Skip until the wildcard's own slot comes up (at most one skip needed: 2 slots total).
	for i := 0; i < 2; i++ {
		if strings.Contains(readBody(t, get(t, srv, "/user/show/"+wildcard)), "wildcard") {
			break
		}
		post(t, srv, "/admin/host/skip-reveal", nil)
	}

	body := readBody(t, get(t, srv, "/user/show/"+wildcard))
	if !strings.Contains(body, "wildcard") {
		t.Errorf("want the wildcard's own message shown, got:\n%s", body)
	}

	// The stage lights up the wildcard alone: one ID, not a pair.
	var snap map[string]any
	if err := json.Unmarshal([]byte(readBody(t, get(t, srv, "/bigscreen/graph-data"))), &snap); err != nil {
		t.Fatal(err)
	}
	ids, _ := snap["reveal_pair_ids"].([]any)
	if len(ids) != 1 || ids[0].(string) != wildcard {
		t.Errorf("want the wildcard alone lit up on stage, got %v", ids)
	}
}

// TestLateArrival_JoinsTheCurrentRoundThroughTheMux covers #84: a
// Participant registering mid-show sees the Round already in progress once
// they're ready.
func TestLateArrival_JoinsTheCurrentRoundThroughTheMux(t *testing.T) {
	llm := newFakeLLM().
		on("interviewer", `{"questions": ["Why?", "How?", "When?", "Where?", "Who?"]}`).
		on("personality generator", `{"name": "The Gopher", "tagline": "Ships"}`)
	gh := newFakeGitHub().withProfile(&GitHubProfile{Login: "late", Languages: []string{"Go"}})
	srv, deps := newTestServer(t, llm, gh)
	if err := deps.db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	post(t, srv, "/admin/host/advance", nil) // -> Round 0: the show has already started

	id, err := deps.onboarding.Register("Late", "late", true)
	if err != nil {
		t.Fatal(err)
	}
	answerEverything(t, deps.onboarding, deps.db, id)
	eventually(t, "ready", func() bool { return reload(t, deps.db, id).PipelineStep == StepReady })

	body := readBody(t, get(t, srv, "/user/show/"+id))
	if !strings.Contains(body, FixedQuestions[0].Text) {
		t.Errorf("want the late arrival to see the current Round's Question, got:\n%s", body)
	}
}

// TestLateArrival_MatchedImmediatelyInTheAfterpartyThroughTheMux covers
// #84: once the Afterparty starts, a new arrival is paired with the
// wildcard as soon as they're ready, and sees their Match straight away.
func TestLateArrival_MatchedImmediatelyInTheAfterpartyThroughTheMux(t *testing.T) {
	llm := newFakeLLM().
		on("interviewer", `{"questions": ["Why?", "How?", "When?", "Where?", "Who?"]}`).
		on("personality generator", `{"name": "The Gopher", "tagline": "Ships"}`).
		on("matchmaker", matchReply)
	gh := newFakeGitHub().withProfile(&GitHubProfile{Login: "late", Languages: []string{"Go"}})
	srv, deps := newTestServer(t, llm, gh)
	if err := deps.db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	seed(t, deps.db, "wild", "The Wildcard", "ready") // the only ready Participant: becomes the wildcard

	for i := 0; i < TotalFixedQuestions; i++ {
		post(t, srv, "/admin/host/advance", nil)
		post(t, srv, "/admin/host/close-round", nil)
	}
	post(t, srv, "/admin/host/advance", nil)     // -> matchmaking: wild stays the wildcard
	post(t, srv, "/admin/host/advance", nil)     // -> reveal: the wildcard's own slot
	post(t, srv, "/admin/host/skip-reveal", nil) // -> afterparty

	id, err := deps.onboarding.Register("Late", "late", true)
	if err != nil {
		t.Fatal(err)
	}
	answerEverything(t, deps.onboarding, deps.db, id)

	eventually(t, "matched with the wildcard", func() bool {
		return reload(t, deps.db, id).MatchedWith == "wild"
	})
	if loc := get(t, srv, "/user/show/"+id).Header.Get("Location"); loc != "/user/match/"+id {
		t.Errorf("want the late arrival redirected to their match page, got %q", loc)
	}
}

func TestBigScreen_SnapshotCarriesTheShowSegmentInAGameShow(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	show := NewShow(db, time.Now)
	if err := show.Advance(); err != nil { // -> Round 0
		t.Fatal(err)
	}
	bs := NewBigScreen(db, NewMatcher(db, newFakeGitHub(), newFakeLLM()), show)

	snap := bs.Snapshot()

	if snap.Format != GameShow {
		t.Errorf("want Format GameShow, got %q", snap.Format)
	}
	if snap.Segment != SegmentRound || snap.Round != 1 {
		t.Errorf("want Round 1, got segment %q round %d", snap.Segment, snap.Round)
	}
	if !snap.RoundOpen {
		t.Error("want RoundOpen true right after opening the Round")
	}
	if snap.RoundCallout != nil {
		t.Errorf("want no callout while the Round is open, got %+v", snap.RoundCallout)
	}
}

// TestBigScreen_SnapshotCarriesTheRoundCalloutOnceClosed covers #80: the
// stage view carries the majority/minority callout once the Round closes.
func TestBigScreen_SnapshotCarriesTheRoundCalloutOnceClosed(t *testing.T) {
	db := newTestDB(t)
	if err := db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	show := NewShow(db, time.Now)
	db.CreateParticipant("a", "a", "A", true)
	db.CreateParticipant("b", "b", "B", true)
	if err := show.Advance(); err != nil { // -> Round 0
		t.Fatal(err)
	}
	show.SubmitRoundAnswer("a", "Tabs")
	show.SubmitRoundAnswer("b", "Spaces")
	if err := show.CloseRound(); err != nil {
		t.Fatal(err)
	}
	bs := NewBigScreen(db, NewMatcher(db, newFakeGitHub(), newFakeLLM()), show)

	snap := bs.Snapshot()

	if snap.RoundOpen {
		t.Error("want RoundOpen false once closed")
	}
	if snap.RoundCallout == nil {
		t.Fatal("want a callout once the Round has closed")
	}
	if snap.RoundCallout.MinorityCount != 1 {
		t.Errorf("want a minority of 1, got %+v", snap.RoundCallout)
	}
}

// TestShowStatus_ReflectsTheCurrentSegmentAndRedirectsOnceMatched covers
// #78: a ready Game Show Participant sees the show status page, which
// redirects to the match page once they're matched and revealed.
func TestShowStatus_ReflectsTheCurrentSegmentAndRedirectsOnceMatched(t *testing.T) {
	llm := newFakeLLM().
		on("interviewer", `{"questions": ["Why?", "How?", "When?", "Where?", "Who?"]}`).
		on("personality generator", `{"name": "The Gopher", "tagline": "Ships"}`)
	gh := newFakeGitHub().withProfile(&GitHubProfile{Login: "octo", Languages: []string{"Go"}})
	srv, deps := newTestServer(t, llm, gh)
	if err := deps.db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	id, err := deps.onboarding.Register("Octo", "octo", true)
	if err != nil {
		t.Fatal(err)
	}
	answerEverything(t, deps.onboarding, deps.db, id)
	eventually(t, "ready", func() bool { return reload(t, deps.db, id).PipelineStep == StepReady })

	if loc := get(t, srv, "/user/wait/"+id).Header.Get("Location"); loc != "/user/show/"+id {
		t.Errorf("a ready Game Show Participant belongs on the show status page, got redirect %q", loc)
	}

	body := readBody(t, get(t, srv, "/user/show/"+id))
	if !strings.Contains(body, "Waiting for the Host") {
		t.Errorf("want the Lobby status shown, got:\n%s", body)
	}

	seed(t, deps.db, "partner", "The Crab", "ready")
	pair(t, deps.db, id, "partner")
	deps.db.Reveal()

	if loc := get(t, srv, "/user/show/"+id).Header.Get("Location"); loc != "/user/match/"+id {
		t.Errorf("once matched and revealed, want redirect to the match page, got %q", loc)
	}
}

// TestHostConsoleStream_RefreshesWhenTheSegmentChanges and
// TestShowStatusStream_RefreshesWhenTheSegmentChanges cover #78: the host
// console and a Participant's show status page are both pushed the current
// segment through the change feed, not polled.
func TestHostConsoleStream_RefreshesWhenTheSegmentChanges(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	if err := deps.db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}

	s := open(t, srv, "/admin/host/stream")
	sentNothingNew(t, s, 1, "", "nothing changed")

	post(t, srv, "/admin/host/advance", nil)
	waitFor(t, s, "event: refresh")
}

func TestShowStatusStream_RefreshesWhenTheSegmentChanges(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	if err := deps.db.SetEventFormat(GameShow); err != nil {
		t.Fatal(err)
	}
	seed(t, deps.db, "p", "The Gopher", "ready")

	s := open(t, srv, "/user/show-stream/p")
	sentNothingNew(t, s, 1, "", "nothing changed")

	post(t, srv, "/admin/host/advance", nil)
	waitFor(t, s, "event: refresh")
}
