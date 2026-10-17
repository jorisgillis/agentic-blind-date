package main

// Gherkin journey bindings for census journeys J4–J8 (ADR-0022's gap
// groups, run under ADR-0025's suite): settings & theming, the meet page
// & the reveal, the dossier & the mission card, the recap, and show &
// tell. The steps stay in the domain's own language — every route, form
// field and rendered marker lives here, never in the .feature prose.
// Registration goes through the suite's gherkinSteps seam: the seed
// steps and the shared world stay in gherkin_test.go, and these steps
// reuse them wherever they fit.

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/cucumber/godog"
)

func init() { gherkinSteps = append(gherkinSteps, registerJourneySteps4to8) }

func registerJourneySteps4to8(ctx *godog.ScenarioContext, w *gherkinWorld) {
	// J4 — settings and theming.
	ctx.When(`^(\w+) picks the (\w+) theme in Settings$`, w.picksTheme)
	ctx.When(`^the host picks (\w+) as the Public theme$`, w.hostPicksPublicTheme)
	ctx.When(`^(\w+) shares their email in Settings$`, w.sharesEmail)
	ctx.Given(`^(\w+) has shared their email in Settings$`, w.sharesEmail)
	ctx.Then(`^(\w+)'s personal page is painted in (\w+)$`, w.personalPagePaintedIn)
	ctx.Then(`^the landing page stays in Midnight$`, w.landingStaysMidnight)
	ctx.Then(`^the landing page is painted in (\w+)$`, w.landingPaintedIn)
	ctx.Then(`^the big screen is painted in (\w+)$`, w.bigScreenPaintedIn)
	ctx.Then(`^(\w+)'s settings show the email ready to share$`, w.settingsShowEmail)
	ctx.Then(`^(\w+)'s exchange prompt shows (\w+)'s email$`, w.exchangeShowsEmail)

	// J5 — the meet page and the reveal.
	ctx.When(`^(\w+) opens (\w+)'s meet link$`, w.opensMeetLink)
	ctx.Then(`^(\w+) sees (\w+)'s persona card$`, w.seesPersonaCard)
	ctx.Then(`^meeting (\w+) waits for the Social$`, w.meetingWaitsForSocial)
	ctx.When(`^(\w+) opens their match page$`, w.opensMatchPage)
	ctx.Then(`^(\w+) is sent back to the wait page$`, w.sentBackToWait)
	ctx.When(`^the host reveals the matches$`, w.hostReveals)
	ctx.Then(`^(\w+)'s match page shows the match and its compatibility score$`, w.matchPageShowsMatch)

	// Shared staging for the later journeys.
	ctx.Given(`^the host has advanced the event to the Social$`, w.hostAdvancesToSocial)
	ctx.Given(`^the host has advanced the event to Show & Tell$`, w.hostAdvancesToShowTell)
	ctx.Given(`^(\w+) and (\w+) have confirmed their meeting$`, w.haveConfirmedMeeting)
	ctx.Given(`^the matchmaker has written (\w+) and (\w+)'s dossier$`, w.matchmakerWroteDossier)
	ctx.When(`^the host closes the event$`, w.hostClosesEvent)

	// J6 — the dossier and the mission card.
	ctx.Then(`^(\w+)'s dossier is locked$`, w.dossierLocked)
	ctx.Then(`^(\w+)'s dossier is unlocked$`, w.dossierUnlocked)
	ctx.Given(`^(\w+)'s mission card is live$`, w.missionCardLive)
	ctx.Then(`^(\w+)'s mission card is live$`, w.missionCardLive)
	ctx.Then(`^(\w+)'s mission card is on record$`, w.missionCardOnRecord)
	ctx.Then(`^(\w+)'s mission card stamps (\w+)'s tile$`, w.missionCardStamps)

	// J7 — the recap.
	ctx.When(`^a visitor opens the public event recap$`, w.visitorOpensPublicRecap)
	ctx.Then(`^the public recap is not available yet$`, w.publicRecapNotAvailable)
	ctx.Then(`^the public event recap opens$`, w.publicRecapOpens)
	ctx.Then(`^the recap names the evening's personas and connections$`, w.publicRecapContent)
	ctx.Then(`^the recap keeps identities and contact details to itself$`, w.publicRecapPersonasOnly)
	ctx.Then(`^(\w+)'s personal page offers their recap link$`, w.recapLinkOffered)
	ctx.When(`^(\w+) opens their recap$`, w.opensRecap)
	ctx.Then(`^(\w+)'s recap shows their persona, their match and their connection$`, w.recapShowsEvening)
	ctx.Then(`^(\w+)'s recap shows the contact details they shared$`, w.recapShowsShared)

	// J8 — show & tell.
	ctx.When(`^(\w+) signs up with the talk "([^"]*)"$`, w.signsUp)
	ctx.Given(`^(\w+) has signed up with the talk "([^"]*)"$`, w.signsUp)
	ctx.Then(`^(\w+)'s show and tell section shows "([^"]*)" in the queue$`, w.showtellShowsQueued)
	ctx.Then(`^the host's queue lists "([^"]*)"$`, w.hostQueueLists)
	ctx.Then(`^the host's queue no longer lists "([^"]*)"$`, w.hostQueueOmits)
	ctx.When(`^the host puts (\w+)'s talk on stage$`, w.hostStagesTalk)
	ctx.Then(`^the host's queue shows (\w+)'s talk on stage$`, w.hostQueueShowsOnStage)
	ctx.Then(`^(\w+)'s question form is open for the presenter$`, w.questionFormOpen)
	ctx.When(`^(\w+) asks "([^"]*)"$`, w.asksQuestion)
	ctx.Then(`^the host's question queue carries (\w+)'s question$`, w.hostQuestionQueueCarries)
	ctx.When(`^the host puts (\w+)'s question on the big screen$`, w.hostAsksQuestion)
	ctx.When(`^the host marks (\w+)'s question answered$`, w.hostAnswersQuestion)
	ctx.Then(`^(\w+)'s question is answered$`, w.questionAnswered)
	ctx.When(`^the host moves (\w+)'s talk up$`, w.hostMovesTalkUp)
	ctx.Then(`^the host's queue shows (\w+)'s talk ahead of (\w+)'s$`, w.hostQueueOrder)
	ctx.When(`^the host removes (\w+)'s talk$`, w.hostRemovesTalk)
	ctx.Then(`^(\w+)'s show and tell section shows the sign-up form again$`, w.showtellFormAgain)
}

// --- J4: settings and theming -----------------------------------------

// picksTheme saves the Participant's theme choice from the settings'
// theme picker: one click, and the page reloads in its new look.
func (w *gherkinWorld) picksTheme(name, label string) {
	rec := w.post("/user/theme/"+w.people[name], url.Values{"theme": {strings.ToLower(label)}}, w.people[name])
	if rec.Code != http.StatusOK || rec.Header().Get("HX-Redirect") == "" {
		w.t.Fatalf("%s picking the %s theme: status %d, HX-Redirect %q", name, label, rec.Code, rec.Header().Get("HX-Redirect"))
	}
}

// hostPicksPublicTheme saves the host's choice of Public theme.
func (w *gherkinWorld) hostPicksPublicTheme(label string) {
	rec := w.post("/admin/public-theme", url.Values{"theme": {strings.ToLower(label)}}, "")
	if rec.Code != http.StatusSeeOther {
		w.t.Fatalf("picking the Public theme: status %d", rec.Code)
	}
}

// sharesEmail opts the Participant into one shared contact channel: their
// email, entered once and turned on. The address is derived from their name
// so the reveal assertions can check the exact value.
func (w *gherkinWorld) sharesEmail(name string) {
	rec := w.post("/user/settings/"+w.people[name],
		url.Values{"email": {emailOf(name)}, "email_on": {"on"}}, w.people[name])
	if rec.Code != http.StatusOK {
		w.t.Fatalf("%s sharing their email: status %d, body %s", name, rec.Code, rec.Body.String())
	}
}

func emailOf(name string) string { return strings.ToLower(name) + "@example.com" }

// themeHex is a Theme's page background — the observable difference
// between two painted pages.
func themeHex(label string) string { return themeByName(strings.ToLower(label)).Background }

// pagePaintedIn fetches a page and asserts the Theme that painted it, by
// the background color role the page's stylesheet carries.
func (w *gherkinWorld) pagePaintedIn(path, cookie, label string) {
	rec := w.get(path, cookie)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "--color-bg:"+themeHex(label)) {
		w.t.Fatalf("%s painted in %s: status %d, want background %s", path, label, rec.Code, themeHex(label))
	}
}

// personalPagePaintedIn asserts the Participant's personal page — wherever
// the navigation rule currently places them — carries their Theme.
func (w *gherkinWorld) personalPagePaintedIn(name, label string) {
	w.pagePaintedIn(w.deps.handler.destination(w.person(name)), w.people[name], label)
}

func (w *gherkinWorld) landingStaysMidnight() { w.pagePaintedIn("/user", "", "Midnight") }
func (w *gherkinWorld) landingPaintedIn(label string) {
	w.pagePaintedIn("/user", "", label)
}
func (w *gherkinWorld) bigScreenPaintedIn(label string) {
	w.pagePaintedIn("/bigscreen", "", label)
}

// settingsShowEmail asserts the settings section carries the saved email,
// pre-filled for the next visit.
func (w *gherkinWorld) settingsShowEmail(name string) {
	rec := w.get("/user/settings/"+w.people[name], "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `value="`+emailOf(name)+`"`) {
		w.t.Fatalf("%s's settings do not show their email: status %d", name, rec.Code)
	}
}

// exchangeShowsEmail asserts the mutual rule's payoff: after a completed
// meeting, the other side's meet section shows what this participant chose
// to share.
func (w *gherkinWorld) exchangeShowsEmail(name, owner string) {
	rec := w.get("/user/meet-status/"+w.people[name], w.people[name])
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "They shared") || !strings.Contains(body, emailOf(owner)) {
		w.t.Fatalf("%s's exchange prompt does not show %s's email: status %d", name, owner, rec.Code)
	}
}

// --- J5: the meet page and the reveal ---------------------------------

// opensMeetLink opens the target's meet page as the named participant.
func (w *gherkinWorld) opensMeetLink(name, target string) {
	if rec := w.get("/user/m/"+w.people[target], w.people[name]); rec.Code != http.StatusOK {
		w.t.Fatalf("%s opening %s's meet link: status %d", name, target, rec.Code)
	}
}

// seesPersonaCard asserts the meet page carries the target's persona card.
func (w *gherkinWorld) seesPersonaCard(name, target string) {
	rec := w.get("/user/m/"+w.people[target], w.people[name])
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "You found") || !strings.Contains(body, w.person(target).PersonaName) {
		w.t.Fatalf("%s does not see %s's persona card: status %d", name, target, rec.Code)
	}
}

// meetingWaitsForSocial asserts the meet page names the gate: confirmation
// opens at the Social.
func (w *gherkinWorld) meetingWaitsForSocial(target string) {
	rec := w.get("/user/m/"+w.people[target], "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Meeting confirmation is closed for now") {
		w.t.Fatalf("meeting %s should wait for the Social: status %d", target, rec.Code)
	}
}

// opensMatchPage opens the participant's match page, wherever it leads.
func (w *gherkinWorld) opensMatchPage(name string) {
	if rec := w.get("/user/match/"+w.people[name], w.people[name]); rec.Code >= 400 {
		w.t.Fatalf("%s opening their match page: status %d", name, rec.Code)
	}
}

// sentBackToWait asserts the navigation rule before the reveal: a matched
// participant's match page redirects to the wait page while matches stay
// hidden.
func (w *gherkinWorld) sentBackToWait(name string) {
	rec := w.get("/user/match/"+w.people[name], w.people[name])
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/user/wait/"+w.people[name] {
		w.t.Fatalf("%s should be sent back to the wait page: status %d, Location %q", name, rec.Code, rec.Header().Get("Location"))
	}
}

// hostReveals is the reveal: the host's alias for advancing to the Social,
// which opens every match page.
func (w *gherkinWorld) hostReveals() {
	if rec := w.post("/admin/reveal", url.Values{}, ""); rec.Code != http.StatusSeeOther {
		w.t.Fatalf("the reveal: status %d", rec.Code)
	}
}

// matchPageShowsMatch asserts the revealed match page: the match hero with
// the compatibility score the matchmaker gave the pair.
func (w *gherkinWorld) matchPageShowsMatch(name string) {
	rec := w.get("/user/match/"+w.people[name], w.people[name])
	body := rec.Body.String()
	for _, want := range []string{"It's a match!", "80% compatibility", "Find them in the room"} {
		if rec.Code != http.StatusOK || !strings.Contains(body, want) {
			w.t.Fatalf("%s's match page does not show %q: status %d", name, want, rec.Code)
		}
	}
}

// --- shared staging for the later journeys -----------------------------

func (w *gherkinWorld) hostAdvancesToShowTell() {
	if rec := w.post("/admin/phase", url.Values{"to": {"showtell"}}, ""); rec.Code >= 400 {
		w.t.Fatalf("advancing to Show & Tell: status %d, body %s", rec.Code, rec.Body.String())
	}
}

func (w *gherkinWorld) hostClosesEvent() {
	if rec := w.post("/admin/phase", url.Values{"to": {"closed"}}, ""); rec.Code >= 400 {
		w.t.Fatalf("closing the event: status %d, body %s", rec.Code, rec.Body.String())
	}
}

// haveConfirmedMeeting is the two-sided handshake as completed context.
func (w *gherkinWorld) haveConfirmedMeeting(a, b string) {
	w.confirmsMeeting(a, b)
	w.confirmsMeeting(b, a)
}

// matchmakerWroteDossier re-stages the pair's Match with a full dossier —
// the reason, the flags and the icebreakers the Match's page teases locked
// and the recap keeps. The pair stays the pair; only the payload grows.
func (w *gherkinWorld) matchmakerWroteDossier(a, b string) {
	result := &matchResult{
		Score:       80,
		Reason:      "Both ship on Fridays, no regrets",
		GreenFlags:  []string{"Ships tests with every feature"},
		RedFlags:    []string{"Rebases in public"},
		Icebreakers: []string{"Defend your tabs"},
	}
	if _, err := w.deps.handler.relations.Pair(Match{A: w.person(a), B: w.person(b), Result: result}); err != nil {
		w.t.Fatalf("the matchmaker's dossier for %s and %s: %v", a, b, err)
	}
}

// --- J6: the dossier and the mission card ------------------------------

func (w *gherkinWorld) dossierLocked(name string) {
	rec := w.get("/user/dossier/"+w.people[name], w.people[name])
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Your dossier is locked") {
		w.t.Fatalf("%s's dossier should be locked: status %d", name, rec.Code)
	}
}

func (w *gherkinWorld) dossierUnlocked(name string) {
	rec := w.get("/user/dossier/"+w.people[name], w.people[name])
	body := rec.Body.String()
	for _, want := range []string{"Both ship on Fridays, no regrets", "Green flags", "Defend your tabs"} {
		if rec.Code != http.StatusOK || !strings.Contains(body, want) {
			w.t.Fatalf("%s's unlocked dossier does not show %q: status %d", name, want, rec.Code)
		}
	}
	if strings.Contains(body, "Your dossier is locked") {
		w.t.Fatalf("%s's dossier is still locked", name)
	}
}

// missionCardLive waits for the background deal at the Social and asserts
// the participant's page carries the card.
func (w *gherkinWorld) missionCardLive(name string) {
	w.eventually(name+"'s mission card is dealt", func() bool {
		view, err := w.deps.missions.View(w.people[name])
		return err == nil && view != nil
	})
	rec := w.get("/user/mission/"+w.people[name], w.people[name])
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Your mission") {
		w.t.Fatalf("%s's mission card is not live: status %d", name, rec.Code)
	}
}

// missionCard is the debug record's shape: the tiles with their stamp
// state, the participant and their persona.
type missionCard struct {
	ParticipantID string `json:"participant_id"`
	PersonaName   string `json:"persona_name"`
	Tiles         []struct {
		PersonID string `json:"person_id"`
		Stamped  bool   `json:"stamped"`
	} `json:"tiles"`
}

// missionCardData polls the mission card's record until the background
// deal has landed.
func (w *gherkinWorld) missionCardData(name string) *missionCard {
	var card *missionCard
	w.eventually(name+"'s mission card is on record", func() bool {
		rec := w.get("/data/missioncard/"+w.people[name], "")
		if rec.Code != http.StatusOK {
			return false
		}
		var decoded missionCard
		if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
			return false
		}
		card = &decoded
		return true
	})
	return card
}

func (w *gherkinWorld) missionCardOnRecord(name string) {
	card := w.missionCardData(name)
	if card.ParticipantID != w.people[name] || card.PersonaName == "" || len(card.Tiles) == 0 {
		w.t.Fatalf("%s's mission card record: %+v", name, card)
	}
}

func (w *gherkinWorld) missionCardStamps(name, target string) {
	for _, tile := range w.missionCardData(name).Tiles {
		if tile.PersonID == w.people[target] {
			if !tile.Stamped {
				w.t.Fatalf("%s's mission card has not stamped %s's tile", name, target)
			}
			return
		}
	}
	w.t.Fatalf("%s's mission card has no tile for %s", name, target)
}

// --- J7: the recap ------------------------------------------------------

func (w *gherkinWorld) visitorOpensPublicRecap() {
	if rec := w.get("/recap", ""); rec.Code >= 500 {
		w.t.Fatalf("the public recap: status %d", rec.Code)
	}
}

func (w *gherkinWorld) publicRecapNotAvailable() {
	if rec := w.get("/recap", ""); rec.Code != http.StatusNotFound {
		w.t.Fatalf("the public recap should not be available yet: status %d", rec.Code)
	}
}

func (w *gherkinWorld) publicRecapOpens() {
	rec := w.get("/recap", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "The evening, recapped") {
		w.t.Fatalf("the public recap should open at the close: status %d", rec.Code)
	}
}

func (w *gherkinWorld) publicRecapContent() {
	rec := w.get("/recap", "")
	body := rec.Body.String()
	for _, want := range []string{"Top connectors", "❤️ 1", "The Gopher"} {
		if rec.Code != http.StatusOK || !strings.Contains(body, want) {
			w.t.Fatalf("the public recap does not show %q: status %d", want, rec.Code)
		}
	}
}

// publicRecapPersonasOnly asserts the public surface keeps what it must
// not share: identities and contact values stay off the page.
func (w *gherkinWorld) publicRecapPersonasOnly() {
	rec := w.get("/recap", "")
	assertNoIdentities(w.t, rec.Body.String(), w.deps.db)
}

func (w *gherkinWorld) recapLinkOffered(name string) {
	rec := w.get("/user/recap-link/"+w.people[name], w.people[name])
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "recap-qr") || !strings.Contains(body, "/user/recap/") {
		w.t.Fatalf("%s's personal page does not offer their recap link: status %d", name, rec.Code)
	}
}

// recapToken fetches the participant's unguessable recap token.
func (w *gherkinWorld) recapToken(name string) string {
	token, err := w.deps.handler.recaps.TokenFor(w.people[name])
	if err != nil {
		w.t.Fatalf("%s's recap token: %v", name, err)
	}
	return token
}

func (w *gherkinWorld) opensRecap(name string) {
	rec := w.get("/user/recap/"+w.recapToken(name), "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Your evening, recapped") {
		w.t.Fatalf("%s opening their recap: status %d", name, rec.Code)
	}
}

func (w *gherkinWorld) recapShowsEvening(name string) {
	rec := w.get("/user/recap/"+w.recapToken(name), "")
	body := rec.Body.String()
	for _, want := range []string{"💘 Your match", "80% compatibility", "❤️ 1 connection", "🤝 Your connections"} {
		if rec.Code != http.StatusOK || !strings.Contains(body, want) {
			w.t.Fatalf("%s's recap does not show %q: status %d", name, want, rec.Code)
		}
	}
}

func (w *gherkinWorld) recapShowsShared(name string) {
	rec := w.get("/user/recap/"+w.recapToken(name), "")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "Your contact details") || !strings.Contains(body, emailOf(name)) {
		w.t.Fatalf("%s's recap does not show the contact details they shared: status %d", name, rec.Code)
	}
}

// --- J8: show & tell ----------------------------------------------------

// signsUp records the participant's talk — a new one, or the update that
// takes the sign-up's place in place.
func (w *gherkinWorld) signsUp(name, talk string) {
	rec := w.post("/user/showtell/signup/"+w.people[name],
		url.Values{"title": {talk}, "description": {"One line about it"}}, w.people[name])
	if rec.Code != http.StatusOK {
		w.t.Fatalf("%s signing up with %q: status %d, body %s", name, talk, rec.Code, rec.Body.String())
	}
}

func (w *gherkinWorld) showtellShowsQueued(name, talk string) {
	rec := w.get("/user/showtell/"+w.people[name], w.people[name])
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, talk) || !strings.Contains(body, "In the queue") {
		w.t.Fatalf("%s's show & tell section does not show %q in the queue: status %d", name, talk, rec.Code)
	}
}

func (w *gherkinWorld) hostQueueLists(talk string) {
	rec := w.get("/admin/showtell-queue", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), talk) {
		w.t.Fatalf("the host's queue does not list %q: status %d", talk, rec.Code)
	}
}

func (w *gherkinWorld) hostQueueOmits(talk string) {
	rec := w.get("/admin/showtell-queue", "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), talk) {
		w.t.Fatalf("the host's queue should no longer list %q: status %d", talk, rec.Code)
	}
}

// signupOf fetches the participant's sign-up from the queue.
func (w *gherkinWorld) signupOf(name string) *ShowTellSignup {
	signup, err := w.deps.showtell.ForParticipant(w.people[name])
	if err != nil || signup == nil {
		w.t.Fatalf("%s has no sign-up in the queue: %v", name, err)
	}
	return signup
}

// latestQuestion fetches the participant's newest audience question.
func (w *gherkinWorld) latestQuestion(name string) *ShowTellQuestion {
	questions, err := w.deps.showtell.QuestionsForParticipant(w.people[name])
	if err != nil || len(questions) == 0 {
		w.t.Fatalf("%s has asked no question: %v", name, err)
	}
	return &questions[len(questions)-1]
}

// hostAction posts one of the host's queue actions on a sign-up.
func (w *gherkinWorld) hostSignupAction(name, action string) {
	signup := w.signupOf(name)
	rec := w.post("/admin/showtell/stage", url.Values{
		"id":     {strconv.FormatInt(signup.ID, 10)},
		"action": {action},
	}, "")
	if rec.Code != http.StatusSeeOther {
		w.t.Fatalf("the host's %s on %s's talk: status %d, body %s", action, name, rec.Code, rec.Body.String())
	}
}

func (w *gherkinWorld) hostStagesTalk(name string) { w.hostSignupAction(name, "stage") }
func (w *gherkinWorld) hostMovesTalkUp(name string) {
	signup := w.signupOf(name)
	rec := w.post("/admin/showtell/move", url.Values{
		"id":  {strconv.FormatInt(signup.ID, 10)},
		"dir": {"up"},
	}, "")
	if rec.Code != http.StatusSeeOther {
		w.t.Fatalf("the host moving %s's talk up: status %d", name, rec.Code)
	}
}

func (w *gherkinWorld) hostRemovesTalk(name string) {
	signup := w.signupOf(name)
	rec := w.post("/admin/showtell/remove", url.Values{"id": {strconv.FormatInt(signup.ID, 10)}}, "")
	if rec.Code != http.StatusSeeOther {
		w.t.Fatalf("the host removing %s's talk: status %d", name, rec.Code)
	}
}

func (w *gherkinWorld) hostQueueShowsOnStage(name string) {
	signup := w.signupOf(name)
	rec := w.get("/admin/showtell-queue", "")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, signup.Title) || !strings.Contains(body, "🎤 on stage") {
		w.t.Fatalf("the host's queue does not show %s's talk on stage: status %d", name, rec.Code)
	}
}

// questionFormOpen asserts the audience side of presenter mode: the
// ask-the-presenter form is live, bound to whoever is on stage.
func (w *gherkinWorld) questionFormOpen(name string) {
	rec := w.get("/user/showtell/"+w.people[name], w.people[name])
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "is on stage") {
		w.t.Fatalf("%s's question form is not open for the presenter: status %d", name, rec.Code)
	}
}

func (w *gherkinWorld) asksQuestion(name, text string) {
	rec := w.post("/user/showtell/question/"+w.people[name], url.Values{"question": {text}}, w.people[name])
	if rec.Code != http.StatusOK {
		w.t.Fatalf("%s asking %q: status %d, body %s", name, text, rec.Code, rec.Body.String())
	}
}

func (w *gherkinWorld) hostQuestionQueueCarries(name string) {
	question := w.latestQuestion(name)
	rec := w.get("/admin/showtell-questions", "")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, question.Text) || !strings.Contains(body, "asks") {
		w.t.Fatalf("the host's question queue does not carry %s's question: status %d", name, rec.Code)
	}
}

// hostQuestionAction posts one of the host's question-queue actions.
func (w *gherkinWorld) hostQuestionAction(name, action string) {
	question := w.latestQuestion(name)
	rec := w.post("/admin/showtell/question", url.Values{
		"id":     {strconv.FormatInt(question.ID, 10)},
		"action": {action},
	}, "")
	if rec.Code != http.StatusSeeOther {
		w.t.Fatalf("the host's %s on %s's question: status %d, body %s", action, name, rec.Code, rec.Body.String())
	}
}

func (w *gherkinWorld) hostAsksQuestion(name string)    { w.hostQuestionAction(name, "asked") }
func (w *gherkinWorld) hostAnswersQuestion(name string) { w.hostQuestionAction(name, "answered") }

func (w *gherkinWorld) questionAnswered(name string) {
	rec := w.get("/user/showtell/"+w.people[name], w.people[name])
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "✅ answered") {
		w.t.Fatalf("%s's question is not answered: status %d", name, rec.Code)
	}
}

// hostQueueOrder asserts the queue's visible order: the first talk renders
// before the second.
func (w *gherkinWorld) hostQueueOrder(first, second string) {
	rec := w.get("/admin/showtell-queue", "")
	body := rec.Body.String()
	at := func(name string) int {
		return strings.Index(body, w.signupOf(name).Title)
	}
	a, b := at(first), at(second)
	if rec.Code != http.StatusOK || a < 0 || b < 0 || a > b {
		w.t.Fatalf("the host's queue should show %s's talk ahead of %s's (at %d and %d)", first, second, a, b)
	}
}

func (w *gherkinWorld) showtellFormAgain(name string) {
	rec := w.get("/user/showtell/"+w.people[name], w.people[name])
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "Sign me up") || strings.Contains(body, "Update my sign-up") {
		w.t.Fatalf("%s's show & tell section should show the sign-up form again: status %d", name, rec.Code)
	}
}
