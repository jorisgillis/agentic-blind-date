package main

import "net/http"

// The route census table (ADR-0022): the app's single registration site.
// Every route is one row — the pattern in Go 1.22 "METHOD /path" form, the
// handler, the kind of response it serves, the stable existence marker
// that proves it served, and the journey that covers it (J1–J3 are the
// existing e2e journeys; J4–J13 are the retrofit groups). The census test
// iterates the table, so a route cannot exist outside it.
//
// The marker means different things per kind, and the census test asserts
// it accordingly: a heading / #id / <title> for pages, the top-level #id
// for fragments, a top-level shape descriptor for JSON, a content-type
// for binaries and markdown, the redirect target for redirect routes,
// and — for actions — the response's signature (a Location or
// HX-Redirect target, or a bare status like "200").

// routeKind classifies a route by what it serves; the census test asserts
// each kind its own way.
type routeKind string

const (
	kindPage     routeKind = "page"     // a full HTML page
	kindFragment routeKind = "fragment" // an HTMX fragment swap
	kindJSON     routeKind = "json"     // a JSON debug/data endpoint
	kindSSE      routeKind = "sse"      // a server-sent event stream
	kindBinary   routeKind = "binary"   // a QR PNG or a slide-deck PDF
	kindMarkdown routeKind = "markdown" // the host export pack
	kindRedirect routeKind = "redirect" // GET / → the landing page
	kindAction   routeKind = "action"   // a form POST (or DELETE) that changes state
)

// route is one row of the census table.
type route struct {
	Pattern string // "METHOD /path", the http.ServeMux registration form
	Handler func(*Handler, http.ResponseWriter, *http.Request)
	Kind    routeKind
	Marker  string
	Journey string
}

// root is the GET / handler: the bare path redirects to the landing page;
// any other unmatched path is a plain 404 (the "/" pattern would otherwise
// swallow it).
func (h *Handler) root(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/user", http.StatusFound)
}

// routes is the complete inventory: buildMux registers exactly these
// patterns, in this order. A route is added, changed or removed by editing
// this table — the census test fails on any route that drifts from it.
var routes = []route{
	{"GET /", (*Handler).root, kindRedirect, "/user", "J1"},

	// The participant spine: landing, join, interview, wait.
	{"GET /user", (*Handler).Landing, kindPage, `id="joinForm"`, "J1"},
	{"POST /user/join", (*Handler).Join, kindAction, "/user/onboard/", "J1"},
	{"GET /user/onboard/{id}", (*Handler).Onboard, kindPage, `id="onboard-title"`, "J1"},
	{"GET /user/pipeline/{id}", (*Handler).PipelineStatus, kindFragment, `id="pipeline-area"`, "J1"},
	{"POST /user/answer/{id}", (*Handler).SubmitAnswer, kindAction, `id="pipeline-area"`, "J1"},
	{"GET /user/wait/{id}", (*Handler).Wait, kindPage, `id="wait-status"`, "J1"},
	{"GET /user/wait-status/{id}", (*Handler).WaitStatus, kindFragment, `id="wait-status"`, "J1"},
	{"GET /user/persona/{id}", (*Handler).PersonaCard, kindFragment, `id="persona-card"`, "J1"},

	// The match page and its swapped cards.
	{"GET /user/phase-header/{id}", (*Handler).PhaseHeader, kindFragment, `id="phase-header"`, "J3"},
	{"GET /user/phase-content/{id}", (*Handler).PhaseContent, kindFragment, `id="phase-content"`, "J3"},
	{"GET /user/match/{id}", (*Handler).Match, kindPage, `id="qr"`, "J3"},
	{"GET /user/dossier/{id}", (*Handler).Dossier, kindFragment, `id="dossier"`, "J6"},
	{"GET /user/mission/{id}", (*Handler).MissionCard, kindFragment, `id="mission-card"`, "J6"},

	// The meet handshake.
	{"GET /user/m/{id}", (*Handler).Meet, kindPage, "You found", "J5"},
	{"POST /user/met/{id}", (*Handler).ConfirmMeeting, kindAction, `id="meet-result"`, "J3"},
	{"GET /user/meet-status/{id}", (*Handler).MeetStatus, kindFragment, `id="meet-status"`, "J3"},
	{"GET /user/match-stream/{id}", (*Handler).MatchStream, kindSSE, "event: refresh", "J3"},

	// Settings, theming and the personal recap.
	{"GET /user/settings/{id}", (*Handler).Settings, kindFragment, `id="settings"`, "J4"},
	{"POST /user/settings/{id}", (*Handler).SaveSettings, kindAction, `id="settings"`, "J4"},
	{"POST /user/theme/{id}", (*Handler).SaveTheme, kindAction, "HX-Redirect", "J4"},
	{"GET /user/recap-link/{id}", (*Handler).RecapLink, kindFragment, `id="recap"`, "J7"},
	{"GET /user/recap/{token}", (*Handler).Recap, kindPage, "Agentic Blind Date — Your Recap", "J7"},

	// Show & tell, from the audience side.
	{"POST /user/showtell/signup/{id}", (*Handler).ShowTellSignup, kindAction, `id="showtell"`, "J8"},
	{"GET /user/showtell/{id}", (*Handler).ShowTellStatus, kindFragment, `id="showtell"`, "J8"},
	{"POST /user/showtell/question/{id}", (*Handler).ShowTellQuestion, kindAction, `id="showtell"`, "J8"},

	// Slide decks and the board, from the audience side.
	{"GET /user/slides/{id}", (*Handler).Slides, kindFragment, `id="slides"`, "J9"},
	{"GET /user/board/{id}", (*Handler).BoardSection, kindFragment, `id="board"`, "J10"},
	{"POST /user/board/{id}", (*Handler).BoardPost, kindAction, `id="board"`, "J10"},
	{"POST /user/board/{id}/delete", (*Handler).BoardDelete, kindAction, `id="board"`, "J10"},

	// The personal pages' streams.
	{"GET /user/pipeline-stream/{id}", (*Handler).PipelineStream, kindSSE, "event: refresh", "J1"},
	{"GET /user/wait-stream/{id}", (*Handler).WaitStream, kindSSE, "event: refresh", "J1"},

	// Public: slide decks, the event recap and the join QR.
	{"GET /slides/{id}", (*Handler).SlideDeckOpen, kindBinary, "application/pdf", "J9"},
	{"GET /slides/{id}/download", (*Handler).SlideDeckDownload, kindBinary, "application/pdf", "J9"},
	{"GET /recap", (*Handler).EventRecap, kindPage, "The evening, recapped", "J7"},
	{"GET /qr", (*Handler).QR, kindBinary, "image/png", "J3"},

	// The big screen.
	{"GET /bigscreen", (*Handler).Screen, kindPage, `id="graph"`, "J11"},
	{"GET /bigscreen/graph-data", (*Handler).GraphData, kindJSON, "activity,edges,nodes,phase,phase_label,revealed", "J11"},
	{"GET /bigscreen/stream", (*Handler).ScreenStream, kindSSE, "data: ", "J11"},

	// Debug data.
	{"GET /data", (*Handler).DataIndex, kindPage, "Debug Data", "J13"},
	{"GET /data/participants", (*Handler).DataParticipants, kindJSON, "[]", "J13"},
	{"GET /data/participant/{id}", (*Handler).DataParticipant, kindJSON, "ID,PersonaName", "J13"},
	{"DELETE /data/participant/{id}", (*Handler).DeleteParticipant, kindAction, "200", "J13"},
	{"GET /data/activity", (*Handler).DataActivity, kindJSON, "[]", "J13"},
	{"GET /data/state", (*Handler).DataState, kindJSON, "phase,count,ready", "J1"},
	{"GET /data/meetings", (*Handler).DataMeetings, kindJSON, "[]", "J13"},
	{"GET /data/leaderboard", (*Handler).DataLeaderboard, kindJSON, "[]", "J13"},
	{"GET /data/channels", (*Handler).DataChannels, kindJSON, "[]", "J13"},
	{"GET /data/missioncard/{id}", (*Handler).DataMissionCard, kindJSON, "participant_id,persona_name,tiles,created_at", "J6"},

	// The host console.
	{"GET /admin", (*Handler).Admin, kindPage, "Host panel", "J12"},
	{"POST /admin/phase", (*Handler).AdvancePhase, kindAction, "/admin", "J3"},
	{"POST /admin/reveal", (*Handler).TriggerReveal, kindAction, "/admin", "J5"},
	{"POST /admin/reset", (*Handler).Reset, kindAction, "/admin", "J12"},
	{"POST /admin/rematch", (*Handler).Rematch, kindAction, "/admin", "J12"},
	{"POST /admin/showtell/stage", (*Handler).ShowTellStage, kindAction, "/admin", "J8"},
	{"POST /admin/showtell/move", (*Handler).ShowTellMove, kindAction, "/admin", "J8"},
	{"POST /admin/showtell/remove", (*Handler).ShowTellRemove, kindAction, "/admin", "J8"},
	{"POST /admin/showtell/question", (*Handler).ShowTellQuestionAction, kindAction, "/admin", "J8"},
	{"GET /admin/showtell-queue", (*Handler).ShowTellQueue, kindFragment, `id="showtell-queue"`, "J8"},
	{"GET /admin/showtell-questions", (*Handler).ShowTellQuestions, kindFragment, `id="showtell-questions"`, "J8"},
	{"GET /admin/showtell-stream", (*Handler).ShowTellStream, kindSSE, "event: refresh", "J8"},
	{"GET /admin/export", (*Handler).AdminExport, kindMarkdown, "text/markdown", "J12"},
	{"GET /admin/slides", (*Handler).AdminSlides, kindPage, "Slides — Agentic Blind Date", "J9"},
	{"POST /admin/slides", (*Handler).AdminSlidesUpload, kindAction, "/admin/slides", "J9"},
	{"POST /admin/slides/remove", (*Handler).AdminSlidesRemove, kindAction, "/admin/slides", "J9"},
	{"POST /admin/board", (*Handler).AdminBoard, kindAction, "/admin", "J10"},
	{"POST /admin/public-theme", (*Handler).AdminPublicTheme, kindAction, "/admin", "J4"},
}
