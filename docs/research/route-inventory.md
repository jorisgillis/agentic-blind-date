# Route and page inventory with existence markers

Research ticket: #179 (wayfinder:research). Question: *What routes and pages does
the app actually serve? Enumerate every registered route with its "existence
marker" — a heading, form, or element that proves it rendered — and map each to
the existing e2e journeys in `e2e/journeys`. Output: the census input list plus
the coverage gaps where a journey is missing.*

## Sources

- Route registration: `main.go` `buildMux()` (lines 100–183, the complete and
  only `http.ServeMux` registration site — verified by grepping the tree for
  `HandleFunc`/`mux.Handle`; only `main.go` and `fakes_test.go` match, and the
  latter is test scaffolding).
- Handlers: `handlers.go`, `qr.go`, `slides_handlers.go`, `board_handlers.go`.
- Templates: `templates/*.html` (full pages) and the fragment templates inside
  `templates/fragments.html`, `templates/fragment-persona-card.html`,
  `templates/fragment-phase-header.html`, `templates/fragment-phase-content.html`.
- Journeys: `e2e/journeys/journey1-no-github.spec.ts`,
  `journey2-github.spec.ts`, `journey3-meet-handshake.spec.ts`, plus
  `helpers.ts`, `harness.ts`, `stubs.ts`.

There are **84 registered routes** (83 pattern registrations plus the `GET /`
redirect). The app serves **11 full HTML pages**, **24 HTMX fragments**, **13
JSON/SSE endpoints**, **3 binary endpoints** (PNG QR, PDF inline, PDF download),
**1 markdown text endpoint** (`/admin/export`), and the rest are form POST
actions.

## Pages and their existence markers

| Page | Template | Existence marker (proves it rendered) |
|---|---|---|
| Landing | `landing.html` | `<h1>Agentic Blind Date</h1>`, form `#joinForm` with `#githubHandle` and `#noGitHubCheckbox` |
| Onboarding | `onboard.html` | `<h1 id="onboard-title">Quick interview` (or "The agents are on it"), `#pipeline-area` |
| Waiting (personal page) | `wait.html` | `<title>Agentic Blind Date — Waiting`, `#wait-status`, `#persona-card` |
| Match (personal page) | `match.html` | `<title>… — Your Match!`, `#qr`, "This is you:" line (`fragment-this-is-you.html`), `#phase-content` |
| Meet | `meet.html` | `<h1>You found … !` (persona name + symbol), `#meet-result` |
| Personal recap | `recap.html` | `<h1>` with the persona name, `<title>… — Your Recap` |
| Event recap | `event-recap.html` | `<h1>The evening, recapped` |
| Big screen | `screen.html` | `<h1>Agentic Blind Date</h1>` plus `#graph`, `#phase-label`, `#leaderboard`, `#presenter` |
| Host panel | `admin.html` | `<h1>Host panel`, forms POSTing to `/admin/phase`, `/admin/reset`, `/admin/rematch`, `/admin/public-theme`, and `#showtell-queue`, `#showtell-questions`, `#board-queue` |
| Host slides | `admin-slides.html` | `<h1>Slides`, upload form |
| Debug data | `data.html` | `<h1>Debug Data`, `<h2>Participants (…)`, `hx-delete` buttons |

## Complete route census

Legend — Type: page (full HTML), fragment (HTMX swap), action (POST/DELETE),
json, sse (server-sent events), binary, text. Coverage: J1/J2/J3 =
journey spec (direct or implicit via the page's HTMX wiring), "—" = no journey.
"implicit" means the journey drives the page but the route is exercised by the
page's own HTMX wiring (fragment refresh or stream), not asserted directly.

### Participant (`/user`) routes

| # | Route | Handler | Type | Existence marker | Journey |
|---|---|---|---|---|---|
| 1 | `GET /` | inline | redirect | 302 to `/user` | J1–J3 (`goto(app.url)`) |
| 2 | `GET /user` | `Landing` | page | `#joinForm`, `<h1>Agentic Blind Date` | J1, J2, J3 |
| 3 | `POST /user/join` | `Join` | action | 303 → `/user/onboard/{id}` | J1, J2, J3 |
| 4 | `GET /user/onboard/{id}` | `Onboard` | page | `#onboard-title`, `#pipeline-area` | J1, J2, J3 |
| 5 | `GET /user/pipeline/{id}` | `PipelineStatus` | fragment | `#pipeline-area` (step fragment) | J1–J3 implicit |
| 6 | `POST /user/answer/{id}` | `SubmitAnswer` | action | next question fragment swap in `#pipeline-area` | J1, J2, J3 |
| 7 | `GET /user/wait/{id}` | `Wait` | page | `#wait-status`, `#persona-card` | J1, J2, J3 |
| 8 | `GET /user/wait-status/{id}` | `WaitStatus` | fragment | `#wait-status` ("still crafting…" / phase copy) | J1–J3 implicit |
| 9 | `GET /user/persona/{id}` | `PersonaCard` | fragment | `#persona-card` with `<h2>` persona name | J1, J2, J3 (`awaitPersona`) |
| 10 | `GET /user/phase-header/{id}` | `PhaseHeader` | fragment | `#phase-header` strip | J3 implicit |
| 11 | `GET /user/phase-content/{id}` | `PhaseContent` | fragment | `#phase-content`; `<h1>It's a match!` on reveal | J3 implicit |
| 12 | `GET /user/match/{id}` | `Match` | page | `#qr`, "This is you:" | J3 (post-`/admin/phase`) |
| 13 | `GET /user/dossier/{id}` | `Dossier` | fragment | `#dossier` card | — |
| 14 | `GET /user/mission/{id}` | `MissionCard` | fragment | `#mission-card` | — |
| 15 | `GET /user/m/{id}` | `Meet` | page | `<h1>You found …!`, `#meet-result` | — |
| 16 | `POST /user/met/{id}` | `ConfirmMeeting` | action | `#meet-result` swap ("Pending — waiting for them…") | J3 |
| 17 | `GET /user/meet-status/{id}` | `MeetStatus` | fragment | `#meet-status` with "Your meet code" | J3 |
| 18 | `GET /user/match-stream/{id}` | `MatchStream` | sse | stream events drive `#meet-status` refresh | J3 implicit |
| 19 | `GET /user/settings/{id}` | `Settings` | fragment | `#settings` card | — |
| 20 | `POST /user/settings/{id}` | `SaveSettings` | action | re-rendered `#settings` | — |
| 21 | `POST /user/theme/{id}` | `SaveTheme` | action | re-rendered `#settings`, theme CSS change | — |
| 22 | `GET /user/recap-link/{id}` | `RecapLink` | fragment | `#recap` card, `#recap-qr` image | — |
| 23 | `GET /user/recap/{token}` | `Recap` | page | `recap.html` `<h1>` persona name | — |
| 24 | `POST /user/showtell/signup/{id}` | `ShowTellSignup` | action | `#showtell` fragment swap ("signed up" state) | — |
| 25 | `GET /user/showtell/{id}` | `ShowTellStatus` | fragment | `#showtell` section | — |
| 26 | `GET /user/slides/{id}` | `Slides` | fragment | `#slides` list | — |
| 27 | `POST /user/showtell/question/{id}` | `ShowTellQuestion` | action | `#showtell` fragment swap (question posted) | — |
| 28 | `GET /user/board/{id}` | `BoardSection` | fragment | `#board` section, `#board-text` input | — |
| 29 | `POST /user/board/{id}` | `BoardPost` | action | `#board` re-render (new post) | — |
| 30 | `POST /user/board/{id}/delete` | `BoardDelete` | action | `#board` re-render (post gone) | — |
| 31 | `GET /user/pipeline-stream/{id}` | `PipelineStream` | sse | stream drives onboard pipeline advance | J1–J3 implicit |
| 32 | `GET /user/wait-stream/{id}` | `WaitStream` | sse | stream drives `refresh` events on wait page | J1–J3 implicit |

### Public / big screen routes

| # | Route | Handler | Type | Existence marker | Journey |
|---|---|---|---|---|---|
| 33 | `GET /slides/{id}` | `SlideDeckOpen` | binary | `application/pdf` inline, `Content-Disposition: inline` | — |
| 34 | `GET /slides/{id}/download` | `SlideDeckDownload` | binary | `application/pdf`, `Content-Disposition: attachment` | — |
| 35 | `GET /recap` | `EventRecap` | page | `<h1>The evening, recapped` | — |
| 36 | `GET /qr` | `QR` | binary | `Content-Type: image/png` (size param, 32–1024) | — (embedded `#qr`/`#meet-qr`/`#recap-qr` imgs render from it, but never asserted) |
| 37 | `GET /bigscreen` | `Screen` | page | `<h1>Agentic Blind Date`, `#graph`, `#phase-label` | — |
| 38 | `GET /bigscreen/graph-data` | `GraphData` | json | `writeJSON(h.screen.Payload())` — nodes/edges payload | — |
| 39 | `GET /bigscreen/stream` | `ScreenStream` | sse | stream drives the big screen | — |

### Data (`/data`) routes — JSON, no pages

| # | Route | Handler | Existence marker | Journey |
|---|---|---|---|---|
| 40 | `GET /data` | `DataIndex` | `data.html` page `<h1>Debug Data` | — |
| 41 | `GET /data/participants` | `DataParticipants` | JSON array of scrubbed participants | — |
| 42 | `GET /data/participant/{id}` | `DataParticipant` | JSON scrubbed participant | — |
| 43 | `DELETE /data/participant/{id}` | `DeleteParticipant` | row removed (hx-delete from `data.html`) | — |
| 44 | `GET /data/activity` | `DataActivity` | JSON activity messages | — |
| 45 | `GET /data/state` | `DataState` | JSON `{"phase": …}` state | harness `waitUntilUp` (liveness probe only, not a journey) |
| 46 | `GET /data/meetings` | `DataMeetings` | JSON meetings list | — |
| 47 | `GET /data/leaderboard` | `DataLeaderboard` | JSON leaderboard/board counts | — |
| 48 | `GET /data/channels` | `DataChannels` | JSON channel counts | — |
| 49 | `GET /data/missioncard/{id}` | `DataMissionCard` | JSON mission card fields | — |

### Host (`/admin`) routes

| # | Route | Handler | Type | Existence marker | Journey |
|---|---|---|---|---|---|
| 50 | `GET /admin` | `Admin` | page | `<h1>Host panel`, `#showtell-queue`, `#board-queue` | — (J3 posts to `/admin/phase` but never loads the page) |
| 51 | `POST /admin/phase` | `AdvancePhase` | action | 303; evening phase advances | J3 |
| 52 | `POST /admin/reveal` | `TriggerReveal` | action | 303; matches revealed | — |
| 53 | `POST /admin/reset` | `Reset` | action | 303; event reset | — |
| 54 | `POST /admin/rematch` | `Rematch` | action | 303; rematch run | — |
| 55 | `POST /admin/showtell/stage` | `ShowTellStage` | action | 303; queue stage set | — |
| 56 | `POST /admin/showtell/move` | `ShowTellMove` | action | 303; queue order changed | — |
| 57 | `POST /admin/showtell/remove` | `ShowTellRemove` | action | 303; presenter removed | — |
| 58 | `POST /admin/showtell/question` | `ShowTellQuestionAction` | action | 303; question state changed | — |
| 59 | `GET /admin/showtell-queue` | `ShowTellQueue` | fragment | `#showtell-queue` | — |
| 60 | `GET /admin/showtell-questions` | `ShowTellQuestions` | fragment | `#showtell-questions` | — |
| 61 | `GET /admin/showtell-stream` | `ShowTellStream` | sse | stream drives the admin console | — |
| 62 | `GET /admin/export` | `AdminExport` | text | rendered markdown export (text/plain body) | — |
| 63 | `GET /admin/slides` | `AdminSlides` | page | `admin-slides.html` `<h1>Slides` | — |
| 64 | `POST /admin/slides` | `AdminSlidesUpload` | action | 303 (or 400 re-render with reason) | — |
| 65 | `POST /admin/slides/remove` | `AdminSlidesRemove` | action | 303 | — |
| 66 | `POST /admin/board` | `AdminBoard` | action | 303; board moderation | — |
| 67 | `POST /admin/public-theme` | `AdminPublicTheme` | action | 303; public theme set | — |

## e2e journeys and what they cover

| Journey | Spec | Routes it drives |
|---|---|---|
| J1 no-GitHub registration | `journey1-no-github.spec.ts` | `/` → `/user` → `POST /user/join` → `/user/onboard/{id}` → `POST /user/answer/{id}` (all 10+ shapes, asserting the `#multiselect-form` language question) → `/user/wait/{id}` → `/user/persona/{id}` (via streams) |
| J2 GitHub registration | `journey2-github.spec.ts` | same path as J1 with a GitHub handle against the stubbed API (`GITHUB_BASE_URL`) |
| J3 meet handshake | `journey3-meet-handshake.spec.ts` | two registrations, then `POST /admin/phase` (to `social`), landing on `/user/match/{id}` with `#meet-status`, exchanging meet codes via `POST /user/met/{id}` until "You met!" |

The harness (`harness.ts`) also polls `GET /data/state` as a liveness probe before
each journey — coverage by accident, not a journey.

## Coverage gaps — routes with no journey

Every route not reachable from the three journeys above. Grouped by the journey
that would naturally claim them:

1. **Meet page and reveal** — `GET /user/m/{id}` (the QR-code meet page), plus
   the reveal flow: `POST /admin/reveal`, and the reveal state of
   `GET /user/phase-content/{id}` (`<h1>It's a match!`). J3 verifies codes on
   the match page but nobody ever opens the meet page or the reveal.
2. **Dossier and mission** — `GET /user/dossier/{id}`, `GET /user/mission/{id}`,
   `GET /data/missioncard/{id}`. The personal pages' dossier and mission cards
   are never asserted.
3. **Settings and theming** — `GET /user/settings/{id}`,
   `POST /user/settings/{id}`, `POST /user/theme/{id}`,
   `POST /admin/public-theme`. The whole theme pipeline (recent commits
   `70528b8`…`db24657`) has no browser-level journey.
4. **Recap** — `GET /user/recap-link/{id}`, `GET /user/recap/{token}`,
   `GET /recap` (event recap). The recap token journey is untested end-to-end.
5. **Show & tell** — `POST /user/showtell/signup/{id}`,
   `GET /user/showtell/{id}`, `POST /user/showtell/question/{id}`, and all six
   host routes (`POST /admin/showtell/{stage,move,remove,question}`,
   `GET /admin/showtell-queue`, `GET /admin/showtell-questions`,
   `GET /admin/showtell-stream`).
6. **Slides** — `GET /user/slides/{id}`, `GET /slides/{id}`,
   `GET /slides/{id}/download`, `GET /admin/slides`, `POST /admin/slides`,
   `POST /admin/slides/remove`. No journey uploads or opens a deck.
7. **Board** — `GET /user/board/{id}`, `POST /user/board/{id}`,
   `POST /user/board/{id}/delete`, `POST /admin/board`. The wall has no
   browser coverage.
8. **Big screen** — `GET /bigscreen`, `GET /bigscreen/graph-data`,
   `GET /bigscreen/stream`. The big screen page is never opened by a test.
9. **Host console** — `GET /admin` (the page itself is never loaded by a
   journey), `POST /admin/reset`, `POST /admin/rematch`, `GET /admin/export`.
   J3 posts to `/admin/phase` blind via `request.post`.
10. **Debug data** — `GET /data`, `GET /data/participants`,
    `GET /data/participant/{id}`, `DELETE /data/participant/{id}`,
    `GET /data/activity`, `GET /data/meetings`, `GET /data/leaderboard`,
    `GET /data/channels`. Only `/data/state` is touched, as a liveness probe.
11. **QR** — `GET /qr` renders PNGs embedded in three pages (`#qr`, `#meet-qr`,
    `#recap-qr`) but the endpoint is never asserted (content type, size bounds).
12. **Phase header fragment** — `GET /user/phase-header/{id}` is only ever
    exercised as part of full-page renders; the standalone fragment route is
    not asserted. Same for `GET /user/pipeline/{id}` (only implicitly).
13. **Streams** — `GET /user/pipeline-stream/{id}`, `GET /user/wait-stream/{id}`,
    `GET /user/match-stream/{id}` work only as side effects of the journeys'
    pages; their event payloads are never asserted directly.

## Notes for the census consumer (#181)

- Every full page has a stable, assertable marker: a heading, a `#id`, or a
  distinctive `<title>`. Fragment routes all carry a top-level `#id`
  (`#pipeline-area`, `#wait-status`, `#persona-card`, `#phase-header`,
  `#phase-content`, `#dossier`, `#mission-card`, `#meet-status`,
  `#meet-result`, `#settings`, `#showtell`, `#showtell-queue`,
  `#showtell-questions`, `#slides`, `#board`, `#recap`).
- JSON routes are asserted by shape only (`writeJSON` in `handlers.go`);
  there is no JSON schema in the repo.
- SSE routes have no markers at all in the current journeys — a census
  protocol wanting to assert them needs its own event-wait pattern
  (the journey 3 "stream refreshes the section" style).
- `GET /` is a 302, not a page; the census should treat it as a redirect check.
