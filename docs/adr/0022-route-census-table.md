# 0022: The Route Census — a Declarative Route Table

## Status

Accepted (2026-10-17)

## Context

The app registers **84 routes** in a single registration site, `buildMux()` in `main.go`: 11 full HTML pages, 24 HTMX fragments, 13 JSON/SSE endpoints, 3 binary endpoints (QR PNG, PDF inline/download), 1 markdown endpoint (`/admin/export`), the `GET /` redirect, and the rest form POST actions. Yet green tests hide broken pages: the three e2e journeys cover only the registration/interview/meet-handshake spine (~15 routes), leaving 12 groups of routes with no journey at all — settings and theming, dossier and mission, recap, show & tell, slides, board, big screen, host console, debug data, QR, and the four SSE streams that only ever act as page side effects.

Two failure modes need a structural answer: a route whose page silently stops rendering (no test asserts it exists), and a route that exists but no flow ever exercises (the coverage gap). An imperative mux cannot answer "what exists?" without reading every registration line.

## Decision

**A declarative route table — `routes.go` — becomes the single registration site.** One row per route: pattern (Go 1.22 method+path), handler, kind, marker, journey. `buildMux()` becomes a loop over the table. The census test iterates the table, so both directions are enforced for free: a table row cannot go unregistered, and a route cannot exist outside the table.

**Route kinds** (from the inventory): `page`, `fragment`, `json`, `sse`, `binary`, `markdown`, `redirect`, `action`.

**Census assertions per kind** (Go test, `httptest` + seeded test DB):

- **Pages** (11): rendered against seeded fixtures, asserting the inventory's stable marker (heading / `#id` / `<title>`) — e.g. landing `#joinForm`, wait `#wait-status`, big screen `#graph` + `#phase-label`
- **Fragments** (24): top-level `#id` present (`#pipeline-area`, `#dossier`, `#mission-card`, …)
- **JSON** (13): status, content-type, and top-level shape keys per route
- **SSE** (4): event-wait pattern asserting at least one event payload
- **Binary** (3): content-type plus non-trivial size bounds (QR PNG, PDF)
- **`GET /`**: 302 redirect target; **`GET /admin/export`**: markdown content-type

**Route-to-journey mapping** — the 12 gap groups become journeys J4–J13, alongside the existing e2e journeys:

| Journey | Covers |
|---|---|
| J1 no-GitHub registration | landing → join → interview → wait → persona (e2e) |
| J2 GitHub registration | J1's path against the stubbed GitHub API (e2e) |
| J3 meet handshake | phase advance, match page, meet-code exchange (e2e) |
| J4 settings & theming | `/user/settings`, `/user/theme`, `/admin/public-theme` |
| J5 meet page & reveal | `/user/m/{id}`, `/admin/reveal`, match phase content |
| J6 dossier & mission | `/user/dossier/{id}`, `/user/mission/{id}`, `/data/missioncard/{id}` |
| J7 recap | `/user/recap-link/{id}`, `/user/recap/{token}`, `/recap` |
| J8 show & tell | signup/status/question + host showtell routes |
| J9 slides | participant and admin slides routes |
| J10 board | participant and admin board routes |
| J11 big screen | `/bigscreen`, graph-data, stream |
| J12 host console | `/admin` page, reset, rematch, export |
| J13 debug data | `/data/*` beyond the liveness-probed `/data/state` |

The assignment lives in the table's `journey` column. The census is **Go-level existence** — mandatory for every route; journeys are the flow layer on top, in the browser (e2e) or in-process (Gherkin, ADR-0025).

**Maintenance rule:** no route lands half-specified. Adding or changing a route requires a table row carrying kind, marker, and journey; the census test fails on any route missing any of the three.

## Consequences

- "What routes exist?" becomes a data question with a single answer, queryable by the census test, the agent, and reviewers.
- A page that stops rendering fails the census on its marker, not just a human noticing.
- Journey coverage becomes a column, not archaeology: the retrofit (ADR-0026) reads the table to find J4–J13 work.
- Rewriting `buildMux()` over the table is mechanical follow-on work, done in the implementation effort.

## Alternatives Considered

- **Keep the imperative mux** — no single inventory; existence checks require re-reading registration code every time.
- **Browser-only census** — slower and flakier for what is fundamentally an assertion about rendered shape; the browser layer stays for journeys.
- **Per-kind test files without a table** — the registration site and the test drift; the table makes both directions structural.
