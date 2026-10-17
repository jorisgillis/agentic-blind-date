package main

// Journey bindings J9–J13 (ADR-0025's domain split over the one shared
// world): the slide decks, the board, the big screen, the host console and
// the debug data — the five gap groups ADR-0022's journey column assigns
// them. The steps register through the gherkinSteps seam of
// gherkin_test.go; the world helpers (w.post/w.get/w.person/w.eventually,
// newTestServer behind "a fresh event") and the fixtures' idioms (the
// multipart slide upload, the board posts, the phase changes) are reused,
// not restated. The prose is these journeys' own — J1, J3 and the J4–J8
// slices own theirs.

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/cucumber/godog"
)

func init() {
	gherkinSteps = append(gherkinSteps, registerJourneySteps9to13)
}

// journeyState is what the steps of the scenario in flight last observed:
// the response the last When fetched, and the big screen's stream where a
// scenario opened one. godog runs one scenario at a time, so one slot each
// is enough; every consumer follows its producer within the same scenario.
var (
	journeyResp   *httptest.ResponseRecorder
	journeyStream *liveStream
)

func registerJourneySteps9to13(ctx *godog.ScenarioContext, w *gherkinWorld) {
	// J9 — the host shares the speakers' slide decks.
	ctx.Given(`^the host has uploaded a slide deck titled "([^"]*)" for the speaker "([^"]*)"$`, w.hostUploadsSlides)
	ctx.When(`^the host uploads a slide deck titled "([^"]*)" for the speaker "([^"]*)"$`, w.hostUploadsSlides)
	ctx.Then(`^the host's slides page lists "([^"]*)"$`, w.hostSlidesPageLists)
	ctx.Then(`^(\w+)'s personal page offers the slides "([^"]*)" by "([^"]*)"$`, w.personalPageOffersSlides)
	ctx.When(`^(\w+) opens the slide deck$`, w.opensSlideDeck)
	ctx.Then(`^(\w+)'s browser shows the slides as a PDF$`, w.slideDeckShownInline)
	ctx.When(`^(\w+) downloads the slide deck$`, w.downloadsSlideDeck)
	ctx.Then(`^the download arrives as an attachment$`, w.slideDeckDownloaded)
	ctx.When(`^the host removes the slide deck$`, w.hostRemovesSlideDeck)
	ctx.Then(`^the host's slides page lists no slide decks$`, w.hostSlidesPageEmpty)
	ctx.Then(`^(\w+)'s personal page offers no slide decks$`, w.personalPageOffersNoSlides)

	// J10 — the board carries the meet-up's shared posts.
	ctx.Given(`^(\w+) has posted "([^"]*)" on the board$`, w.postsOnBoard)
	ctx.When(`^(\w+) posts "([^"]*)" on the board$`, w.postsOnBoard)
	ctx.Then(`^(\w+)'s board shows the post "([^"]*)" waiting for the host$`, w.boardShowsPendingPost)
	ctx.Then(`^(\w+)'s board shows no posts$`, w.boardShowsNoPosts)
	ctx.Given(`^the host has approved (\w+)'s post$`, w.hostApprovesPost)
	ctx.When(`^the host approves (\w+)'s post$`, w.hostApprovesPost)
	ctx.Then(`^(\w+)'s board shows the post "([^"]*)" under (\w+)'s persona$`, w.boardShowsPostUnderPersona)
	ctx.Then(`^the big screen rotates the post on its board card$`, w.bigScreenRotatesThePost)
	ctx.When(`^the host takes (\w+)'s post down$`, w.hostTakesPostDown)
	ctx.When(`^(\w+) deletes their post$`, w.deletesOwnPost)

	// J11 — the big screen shows the evening to the room.
	ctx.When(`^the big screen loads$`, w.bigScreenLoads)
	ctx.Then(`^the big screen renders the graph$`, w.bigScreenRendersGraph)
	ctx.Then(`^the graph names every participant as their persona, never their identity$`, w.graphNamesPersonasOnly)
	ctx.Then(`^the big screen's connection leaderboard shows (\w+) with one connection$`, w.screenLeaderboardShowsConnection)
	ctx.Given(`^the big screen is watching for changes$`, w.bigScreenWatches)
	ctx.Then(`^the big screen's stream announces the Social$`, w.screenStreamAnnouncesSocial)

	// J12 — the host runs the evening from the console.
	ctx.When(`^the host opens the console$`, w.hostOpensConsole)
	ctx.Then(`^the console shows the onboarding phase$`, w.consoleShowsOnboarding)
	ctx.Then(`^the console counts two participants$`, w.consoleCountsTwo)
	ctx.When(`^the host starts a rematch$`, w.hostStartsRematch)
	ctx.Then(`^the console says the matchmaker agents are at work$`, w.consoleSaysRematchStarted)
	ctx.Then(`^the console says matching is frozen, pairs are final$`, w.consoleSaysRematchFrozen)
	ctx.Then(`^the big screen's graph still shows (\w+) and (\w+) matched$`, w.graphStillShowsPair)
	ctx.When(`^the host resets the event$`, w.hostResetsEvent)
	ctx.Then(`^the connection leaderboard is empty$`, w.leaderboardIsEmpty)
	ctx.Then(`^the big screen shows the onboarding phase again$`, w.screenBackOnboarding)
	ctx.When(`^the host closes the event$`, w.hostClosesEvent)
	ctx.When(`^the host downloads the export pack$`, w.hostDownloadsExport)
	ctx.Then(`^the pack names the champion with one connection$`, w.exportNamesChampion)
	ctx.Then(`^the pack carries the personas, never the identities$`, w.exportPersonasOnly)

	// J13 — the debug data behind the event.
	ctx.When(`^the debug index is opened$`, w.debugIndexOpened)
	ctx.Then(`^it shows the event's phase$`, w.debugIndexShowsPhase)
	ctx.Then(`^it lists the one participant$`, w.debugIndexListsOne)
	ctx.When(`^the debug data is read for (\w+)$`, w.debugDataReadFor)
	ctx.Then(`^it shows (\w+)'s persona$`, w.debugDataShowsPersona)
	ctx.Then(`^the match dossier stays scrubbed$`, w.debugDossierScrubbed)
	ctx.Then(`^the debug data for (\w+) carries the unlocked dossier$`, w.debugDossierUnlocked)
	ctx.When(`^the debug meetings are read$`, w.debugMeetingsRead)
	ctx.Then(`^they show the completed meeting between (\w+) and (\w+)$`, w.debugMeetingsShowPair)
	ctx.Then(`^the activity log records the meeting$`, w.debugActivityRecordsMeeting)
	ctx.Given(`^(\w+) has opted in to their email channel$`, w.optsIntoEmail)
	ctx.When(`^the debug channel counts are read$`, w.debugChannelsRead)
	ctx.Then(`^they show (\w+) with one channel$`, w.debugChannelsShowOne)
	ctx.Then(`^they never show (\w+)'s email address$`, w.debugChannelsHideValue)
	ctx.When(`^the host removes (\w+) from the event$`, w.hostRemovesParticipant)
	ctx.Then(`^(\w+)'s debug record is gone$`, w.debugRecordGone)
	ctx.Then(`^the debug data shows two participants$`, w.debugShowsTwo)
}

// ── J9: slide decks ─────────────────────────────────────────────────────────

// hostUploadsSlides is the Host's upload: one PDF with a title and the
// speaker's name, exactly like the census plan stages it.
func (w *gherkinWorld) hostUploadsSlides(title, speaker string) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for field, value := range map[string]string{"title": title, "speaker": speaker} {
		if err := mw.WriteField(field, value); err != nil {
			w.t.Fatal(err)
		}
	}
	part, err := mw.CreateFormFile("file", "deck.pdf")
	if err != nil {
		w.t.Fatal(err)
	}
	if _, err := part.Write(samplePDF); err != nil {
		w.t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		w.t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/slides", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	w.srv.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		w.t.Fatalf("uploading the slide deck: status %d, body %s", rec.Code, rec.Body.String())
	}
}

// onlyDeck is the scenario's one slide deck, found by the module the way
// the fixtures do — the upload steps stage exactly one.
func (w *gherkinWorld) onlyDeck() SlideDeck {
	decks, err := w.deps.slides.List()
	if err != nil {
		w.t.Fatal(err)
	}
	if len(decks) != 1 {
		w.t.Fatalf("want exactly one slide deck, got %d", len(decks))
	}
	return decks[0]
}

func (w *gherkinWorld) hostSlidesPageLists(title string) {
	rec := w.get("/admin/slides", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), title) {
		w.t.Fatalf("the host's slides page does not list %q: status %d", title, rec.Code)
	}
}

func (w *gherkinWorld) personalPageOffersSlides(name, title, speaker string) {
	rec := w.get("/user/slides/"+w.people[name], w.people[name])
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, title) || !strings.Contains(body, speaker) {
		w.t.Fatalf("%s's personal page does not offer %q by %q: status %d", name, title, speaker, rec.Code)
	}
}

func (w *gherkinWorld) opensSlideDeck(name string) {
	journeyResp = w.get("/slides/"+strconv.FormatInt(w.onlyDeck().ID, 10), w.people[name])
	if journeyResp.Code != http.StatusOK {
		w.t.Fatalf("%s opening the slide deck: status %d", name, journeyResp.Code)
	}
}

func (w *gherkinWorld) slideDeckShownInline(name string) {
	if ct := journeyResp.Header().Get("Content-Type"); ct != "application/pdf" {
		w.t.Fatalf("%s's slide deck: want a PDF, got %q", name, ct)
	}
	if disp := journeyResp.Header().Get("Content-Disposition"); !strings.Contains(disp, "inline") {
		w.t.Fatalf("%s's slide deck: want it shown inline, got %q", name, disp)
	}
	if !bytes.HasPrefix(journeyResp.Body.Bytes(), []byte("%PDF-")) {
		w.t.Fatalf("%s's slide deck: the body is not a PDF", name)
	}
}

func (w *gherkinWorld) downloadsSlideDeck(name string) {
	journeyResp = w.get("/slides/"+strconv.FormatInt(w.onlyDeck().ID, 10)+"/download", w.people[name])
	if journeyResp.Code != http.StatusOK {
		w.t.Fatalf("%s downloading the slide deck: status %d", name, journeyResp.Code)
	}
}

func (w *gherkinWorld) slideDeckDownloaded() {
	if ct := journeyResp.Header().Get("Content-Type"); ct != "application/pdf" {
		w.t.Fatalf("the download: want a PDF, got %q", ct)
	}
	if disp := journeyResp.Header().Get("Content-Disposition"); !strings.Contains(disp, "attachment") || !strings.Contains(disp, "deck.pdf") {
		w.t.Fatalf("the download: want an attachment named deck.pdf, got %q", disp)
	}
}

func (w *gherkinWorld) hostRemovesSlideDeck() {
	rec := w.post("/admin/slides/remove", url.Values{"id": {strconv.FormatInt(w.onlyDeck().ID, 10)}}, "")
	if rec.Code != http.StatusSeeOther {
		w.t.Fatalf("removing the slide deck: status %d, body %s", rec.Code, rec.Body.String())
	}
}

func (w *gherkinWorld) hostSlidesPageEmpty() {
	rec := w.get("/admin/slides", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Nothing uploaded yet") {
		w.t.Fatalf("the host's slides page still lists decks: status %d", rec.Code)
	}
}

func (w *gherkinWorld) personalPageOffersNoSlides(name string) {
	rec := w.get("/user/slides/"+w.people[name], w.people[name])
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "📑 Slides") {
		w.t.Fatalf("%s's personal page still offers slide decks: status %d", name, rec.Code)
	}
}

// ── J10: the board ──────────────────────────────────────────────────────────

func (w *gherkinWorld) postsOnBoard(name, text string) {
	rec := w.post("/user/board/"+w.people[name], url.Values{"text": {text}}, w.people[name])
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), text) {
		w.t.Fatalf("%s posting %q: status %d, body %s", name, text, rec.Code, rec.Body.String())
	}
}

func (w *gherkinWorld) boardShowsPendingPost(name, text string) {
	rec := w.get("/user/board/"+w.people[name], w.people[name])
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, text) || !strings.Contains(body, "Waiting for the Host") {
		w.t.Fatalf("%s's board does not show %q waiting: status %d", name, text, rec.Code)
	}
}

// boardShowsNoPosts: the section renders only its form when a ready
// Participant holds no posts — the post rows are the observable.
func (w *gherkinWorld) boardShowsNoPosts(name string) {
	rec := w.get("/user/board/"+w.people[name], w.people[name])
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `flex gap-3`) {
		w.t.Fatalf("%s's board still shows posts: status %d", name, rec.Code)
	}
}

// hostDecidesOnPost is the Host's call on a Post: approve a pending one or
// take an approved one down. The Post id is found through the Board module,
// the way the census fixture finds its posts.
func (w *gherkinWorld) hostDecidesOnPost(poster string, status PostStatus, action string) {
	posts, err := w.deps.handler.board.Moderation()
	if err != nil {
		w.t.Fatal(err)
	}
	for _, post := range posts {
		if post.ParticipantID == w.people[poster] && post.Status == status {
			rec := w.post("/admin/board", url.Values{
				"id":     {strconv.FormatInt(post.ID, 10)},
				"action": {action},
			}, "")
			if rec.Code != http.StatusSeeOther {
				w.t.Fatalf("the host's %s on %s's post: status %d, body %s", action, poster, rec.Code, rec.Body.String())
			}
			return
		}
	}
	w.t.Fatalf("no %s post by %s for the host to decide on", status, poster)
}

func (w *gherkinWorld) hostApprovesPost(poster string) {
	w.hostDecidesOnPost(poster, PostPending, "approved")
}

func (w *gherkinWorld) hostTakesPostDown(poster string) {
	w.hostDecidesOnPost(poster, PostApproved, "rejected")
}

func (w *gherkinWorld) boardShowsPostUnderPersona(reader, text, poster string) {
	rec := w.get("/user/board/"+w.people[reader], w.people[reader])
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, text) || !strings.Contains(body, w.person(poster).PersonaName) {
		w.t.Fatalf("%s's board does not show %q under %s's persona: status %d", reader, text, poster, rec.Code)
	}
}

func (w *gherkinWorld) bigScreenRotatesThePost() {
	approved, err := w.deps.handler.board.Approved(-1)
	if err != nil {
		w.t.Fatal(err)
	}
	if len(approved) == 0 {
		w.t.Fatal("no approved post for the big screen to rotate")
	}
	found := false
	for _, card := range w.screenPayload().Board {
		if card.Text == approved[0].Text {
			found = true
		}
	}
	if !found {
		w.t.Fatalf("the big screen does not rotate the approved post %q", approved[0].Text)
	}
}

func (w *gherkinWorld) deletesOwnPost(name string) {
	posts, err := w.deps.handler.board.Visible(w.people[name])
	if err != nil {
		w.t.Fatal(err)
	}
	if len(posts) == 0 {
		w.t.Fatalf("%s has no post to delete", name)
	}
	rec := w.post("/user/board/"+w.people[name]+"/delete",
		url.Values{"post": {strconv.FormatInt(posts[0].ID, 10)}}, w.people[name])
	if rec.Code != http.StatusOK {
		w.t.Fatalf("%s deleting their post: status %d, body %s", name, rec.Code, rec.Body.String())
	}
}

// ── J11: the big screen ─────────────────────────────────────────────────────

func (w *gherkinWorld) screenPayload() ScreenPayload {
	rec := w.get("/bigscreen/graph-data", "")
	if rec.Code != http.StatusOK {
		w.t.Fatalf("the big screen's graph data: status %d", rec.Code)
	}
	var payload ScreenPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		w.t.Fatalf("the big screen's graph data is not JSON: %v", err)
	}
	return payload
}

func (w *gherkinWorld) bigScreenLoads() {
	journeyResp = w.get("/bigscreen", "")
	if journeyResp.Code != http.StatusOK {
		w.t.Fatalf("the big screen: status %d", journeyResp.Code)
	}
}

func (w *gherkinWorld) bigScreenRendersGraph() {
	body := journeyResp.Body.String()
	if !strings.Contains(body, `id="graph"`) || !strings.Contains(body, `id="phase-label"`) {
		w.t.Fatal("the big screen does not render the graph")
	}
}

func (w *gherkinWorld) graphNamesPersonasOnly() {
	payload := w.screenPayload()
	byID := map[string]graphNode{}
	for _, node := range payload.Nodes {
		byID[node.ID] = node
	}
	for name, id := range w.people {
		node, ok := byID[id]
		if !ok || node.PersonaName != w.person(name).PersonaName {
			w.t.Fatalf("the graph does not show %s as their persona: %+v", name, node)
		}
	}
	// Identities never leave the server in the graph payload.
	rec := w.get("/bigscreen/graph-data", "")
	for name := range w.people {
		if strings.Contains(rec.Body.String(), name) || strings.Contains(rec.Body.String(), w.person(name).GitHubHandle) {
			w.t.Fatalf("the graph payload leaks %s's identity", name)
		}
	}
}

func (w *gherkinWorld) screenLeaderboardShowsConnection(name string) {
	payload := w.screenPayload()
	if payload.Leaderboard == nil {
		w.t.Fatal("the big screen shows no connection leaderboard")
	}
	for _, standing := range *payload.Leaderboard {
		if standing.ID == w.people[name] && standing.Connections == 1 {
			return
		}
	}
	w.t.Fatalf("the big screen's leaderboard does not show %s with one connection: %+v", name, *payload.Leaderboard)
}

func (w *gherkinWorld) bigScreenWatches() {
	journeyStream = open(w.t, w.srv, "/bigscreen/stream")
}

func (w *gherkinWorld) screenStreamAnnouncesSocial() {
	w.eventually("the big screen's stream announces the Social", func() bool {
		return strings.Contains(journeyStream.String(), `"phase":"social"`)
	})
}

// ── J12: the host console ───────────────────────────────────────────────────

func (w *gherkinWorld) hostOpensConsole() {
	journeyResp = w.get("/admin", "")
	if journeyResp.Code != http.StatusOK {
		w.t.Fatalf("the host console: status %d", journeyResp.Code)
	}
}

func (w *gherkinWorld) consoleShowsOnboarding() {
	if !strings.Contains(journeyResp.Body.String(), "onboarding") {
		w.t.Fatal("the host console does not show the onboarding phase")
	}
}

// consoleCount is the console's "Registered" head count.
func (w *gherkinWorld) consoleCount() int {
	m := regexp.MustCompile(`Registered</span>\s*<span class="font-bold">(\d+)</span>`).
		FindStringSubmatch(journeyResp.Body.String())
	if m == nil {
		w.t.Fatal("the host console does not show its participant count")
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		w.t.Fatal(err)
	}
	return n
}

func (w *gherkinWorld) consoleCountsTwo() {
	if n := w.consoleCount(); n != 2 {
		w.t.Fatalf("the host console counts %d participants, want 2", n)
	}
}

// hostActionAndLands is the console's post-and-land: the action posts, the
// 303's destination is fetched, and the console that comes back — notice
// included — becomes the scenario's observed response.
func (w *gherkinWorld) hostActionAndLands(path string, vals url.Values) {
	rec := w.post(path, vals, "")
	if rec.Code != http.StatusSeeOther {
		w.t.Fatalf("%s: status %d, body %s", path, rec.Code, rec.Body.String())
	}
	journeyResp = w.get(rec.Header().Get("Location"), "")
	if journeyResp.Code != http.StatusOK {
		w.t.Fatalf("the console after %s: status %d", path, journeyResp.Code)
	}
}

func (w *gherkinWorld) hostStartsRematch() {
	w.hostActionAndLands("/admin/rematch", url.Values{})
}

func (w *gherkinWorld) consoleSaysRematchStarted() {
	if !strings.Contains(journeyResp.Body.String(), "the matchmaker agents are at work") {
		w.t.Fatal("the console does not say the matchmaker agents are at work")
	}
}

func (w *gherkinWorld) consoleSaysRematchFrozen() {
	body := journeyResp.Body.String()
	if !strings.Contains(body, "Matching is frozen") || !strings.Contains(body, "pairs are final") {
		w.t.Fatal("the console does not say matching is frozen, pairs are final")
	}
}

func (w *gherkinWorld) graphStillShowsPair(a, b string) {
	w.eventually("the graph still shows "+a+" and "+b+" matched", func() bool {
		for _, edge := range w.screenPayload().Edges {
			if edge.Matched && edge.Source == w.people[a] && edge.Target == w.people[b] {
				return true
			}
			if edge.Matched && edge.Source == w.people[b] && edge.Target == w.people[a] {
				return true
			}
		}
		return false
	})
}

func (w *gherkinWorld) hostResetsEvent() {
	w.hostActionAndLands("/admin/reset", url.Values{})
}

func (w *gherkinWorld) leaderboardIsEmpty() {
	rec := w.get("/data/leaderboard", "")
	var board []ConnectionStanding
	if err := json.Unmarshal(rec.Body.Bytes(), &board); err != nil {
		w.t.Fatalf("the connection leaderboard is not a list: %v", err)
	}
	if len(board) != 0 {
		w.t.Fatalf("the connection leaderboard still shows %+v", board)
	}
}

func (w *gherkinWorld) screenBackOnboarding() {
	if phase := w.screenPayload().Phase; phase != PhaseOnboarding {
		w.t.Fatalf("the big screen shows the %s phase, want onboarding", phase)
	}
}

func (w *gherkinWorld) hostClosesEvent() {
	w.hostActionAndLands("/admin/phase", url.Values{"to": {string(PhaseClosed)}})
}

func (w *gherkinWorld) hostDownloadsExport() {
	journeyResp = w.get("/admin/export?format=md", "")
	if ct := journeyResp.Header().Get("Content-Type"); journeyResp.Code != http.StatusOK || !strings.HasPrefix(ct, "text/markdown") {
		w.t.Fatalf("the export pack: status %d, content type %q", journeyResp.Code, ct)
	}
}

func (w *gherkinWorld) exportNamesChampion() {
	body := journeyResp.Body.String()
	if !strings.Contains(body, "👑") || !strings.Contains(body, "1 connection") {
		w.t.Fatal("the export pack does not name the champion with one connection")
	}
}

func (w *gherkinWorld) exportPersonasOnly() {
	body := journeyResp.Body.String()
	for name := range w.people {
		if strings.Contains(body, name) {
			w.t.Fatalf("the export pack carries %s's identity", name)
		}
		if !strings.Contains(body, w.person(name).PersonaName) {
			w.t.Fatalf("the export pack does not carry %s's persona", name)
		}
	}
	if strings.Contains(body, "no-github-") {
		w.t.Fatal("the export pack carries a generated handle")
	}
}

// ── J13: the debug data ──────────────────────────────────────────────────────

func (w *gherkinWorld) debugIndexOpened() {
	journeyResp = w.get("/data", "")
	if journeyResp.Code != http.StatusOK {
		w.t.Fatalf("the debug index: status %d", journeyResp.Code)
	}
}

func (w *gherkinWorld) debugIndexShowsPhase() {
	if !strings.Contains(journeyResp.Body.String(), "Waiting for participants") {
		w.t.Fatal("the debug index does not show the event's phase")
	}
}

func (w *gherkinWorld) debugIndexListsOne() {
	if !strings.Contains(journeyResp.Body.String(), "Participants (1)") {
		w.t.Fatal("the debug index does not list the one participant")
	}
}

func (w *gherkinWorld) debugDataReadFor(name string) {
	journeyResp = w.get("/data/participant/"+w.people[name], "")
	if journeyResp.Code != http.StatusOK {
		w.t.Fatalf("the debug data for %s: status %d", name, journeyResp.Code)
	}
}

func (w *gherkinWorld) debugParticipantJSON() map[string]any {
	var p map[string]any
	if err := json.Unmarshal(journeyResp.Body.Bytes(), &p); err != nil {
		w.t.Fatalf("the debug data is not one participant's JSON: %v", err)
	}
	return p
}

func (w *gherkinWorld) debugDataShowsPersona(name string) {
	if got := w.debugParticipantJSON()["PersonaName"]; got != w.person(name).PersonaName {
		w.t.Fatalf("the debug data shows persona %v, want %s's", got, name)
	}
}

func (w *gherkinWorld) debugDossierScrubbed() {
	if assessment := w.debugParticipantJSON()["Assessment"]; assessment != nil {
		w.t.Fatalf("the locked dossier is scrubbed, but the debug data carries %v", assessment)
	}
}

func (w *gherkinWorld) debugDossierUnlocked(name string) {
	journeyResp = w.get("/data/participant/"+w.people[name], "")
	if assessment := w.debugParticipantJSON()["Assessment"]; assessment == nil {
		w.t.Fatalf("the debug data for %s does not carry the unlocked dossier", name)
	}
}

func (w *gherkinWorld) debugMeetingsRead() {
	journeyResp = w.get("/data/meetings", "")
	if journeyResp.Code != http.StatusOK {
		w.t.Fatalf("the debug meetings: status %d", journeyResp.Code)
	}
}

func (w *gherkinWorld) debugMeetingsShowPair(a, b string) {
	var meetings []Meeting
	if err := json.Unmarshal(journeyResp.Body.Bytes(), &meetings); err != nil {
		w.t.Fatalf("the debug meetings are not a list: %v", err)
	}
	for _, m := range meetings {
		pair := map[string]bool{m.A: true, m.B: true}
		if pair[w.people[a]] && pair[w.people[b]] && m.CompletedAt != nil {
			return
		}
	}
	w.t.Fatalf("the debug meetings do not show the completed meeting between %s and %s: %+v", a, b, meetings)
}

func (w *gherkinWorld) debugActivityRecordsMeeting() {
	rec := w.get("/data/activity", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "met!") {
		w.t.Fatalf("the activity log does not record the meeting: status %d", rec.Code)
	}
}

func (w *gherkinWorld) optsIntoEmail(name string) {
	rec := w.post("/user/settings/"+w.people[name],
		url.Values{"email": {journeyEmail(name)}, "email_on": {"on"}}, w.people[name])
	if rec.Code != http.StatusOK {
		w.t.Fatalf("%s opting in to their email channel: status %d", name, rec.Code)
	}
}

// journeyEmail is the email address the opt-in steps share, so the
// never-shows step knows exactly what must not appear.
func journeyEmail(name string) string {
	return strings.ToLower(name) + "@example.net"
}

func (w *gherkinWorld) debugChannelsRead() {
	journeyResp = w.get("/data/channels", "")
	if journeyResp.Code != http.StatusOK {
		w.t.Fatalf("the debug channel counts: status %d", journeyResp.Code)
	}
}

func (w *gherkinWorld) debugChannelsShowOne(name string) {
	var counts []ChannelCount
	if err := json.Unmarshal(journeyResp.Body.Bytes(), &counts); err != nil {
		w.t.Fatalf("the debug channel counts are not a list: %v", err)
	}
	for _, count := range counts {
		if count.ParticipantID == w.people[name] && count.Enabled == 1 {
			return
		}
	}
	w.t.Fatalf("the debug channel counts do not show %s with one channel: %+v", name, counts)
}

func (w *gherkinWorld) debugChannelsHideValue(name string) {
	if strings.Contains(journeyResp.Body.String(), journeyEmail(name)) {
		w.t.Fatalf("the debug channel counts show %s's email address", name)
	}
}

func (w *gherkinWorld) hostRemovesParticipant(name string) {
	rec := w.do(http.MethodDelete, "/data/participant/"+w.people[name], "", nil)
	if rec.Code != http.StatusOK {
		w.t.Fatalf("the host removing %s from the event: status %d, body %s", name, rec.Code, rec.Body.String())
	}
}

func (w *gherkinWorld) debugRecordGone(name string) {
	rec := w.get("/data/participant/"+w.people[name], "")
	if rec.Code != http.StatusNotFound {
		w.t.Fatalf("%s's debug record is still there: status %d", name, rec.Code)
	}
}

func (w *gherkinWorld) debugShowsTwo() {
	rec := w.get("/data/participants", "")
	var participants []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &participants); err != nil {
		w.t.Fatalf("the debug participants are not a list: %v", err)
	}
	if len(participants) != 2 {
		w.t.Fatalf("the debug data shows %d participants, want 2", len(participants))
	}
}
