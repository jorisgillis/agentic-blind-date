# 0025: The Gherkin Journey Suite under godog

## Status

Accepted (2026-10-17)

## Context

The route census (ADR-0022) makes journey coverage a column, but journeys need a voice: e2e specs (`e2e/journeys/journey{n}-{slug}.spec.ts`) verify flows in a real browser but are slow and Node-side, and in-package Go tests verify mechanics but read as code, not as behavior. The author (Joris) wants to author Gherkins for new features and needs a form legible to a non-Gherkin expert; the agent may draft legacy ones for review.

The approved voice was prototyped on branch `prototype/gherkin-happy-flows` (two scenarios, 15 steps, ~190 ms, both passing) and reviewed: declarative steps in the domain's own language, bound to the in-package fakes.

## Decision

**Tool: `godog` v0.16, run as a `godog.TestSuite` inside a `testing.T`** — `go test -run TestGherkinFeatures .` — not the standalone godog CLI. Scenarios therefore inherit Go test flags and run inside the `check` profile (ADR-0021), feeding the coverage and CRAP gates like every other test. Browser-dependent checks stay in e2e.

**Step vocabulary:**

- Declarative domain sentences in CONTEXT.md language only (Participant, Interview, Persona, Meet Code, Meeting, Social, Event): no routes, no form fields, no status codes, no selectors in prose — the bindings own all mechanics.
- Tense: **Given** past (completed context), **When** present (the behavior under test), **Then** observable outcome. Then steps assert what a participant or host can observe, not internal mechanics.
- Actors: participants by first name (bindings map names to session ids); system actors by role ("the host").
- One behavior per When step; Given steps may compose smaller steps.
- Reuse-first: parameterize an existing step by actor name before inventing new prose.

**Feature placement:** one `.feature` file per census journey — `features/journey{n}-{slug}.feature`, mirroring `e2e/journeys/journey{n}-{slug}.spec.ts`, so Gherkin and Playwright journeys cross-reference naturally. The census journey column is the coverage unit; the files follow it.

**Binding conventions:** the `gherkinWorld` pattern — one scenario gets a fresh in-process server via `newTestServer` with scripted fakes (`fakeLLM`, `fakeGitHub`), a name-to-id map, and `eventually` polling for the async pipeline. A single `gherkin_test.go` until size demands a split; then step registration splits by domain over the one shared world type. Step regexes are anchored (`^...$`); captured groups only for actor names and quoted values.

**Seed:** the prototype bindings are adopted, not rewritten — merge `prototype/gherkin-happy-flows`, rename the two features to journey numbering (`journey1-no-github.feature`, `journey3-meet-handshake.feature`), drop the PROTOTYPE banners.

**Authorship:** Joris authors Gherkins for new features; the agent may draft legacy ones for review (standing rule).

## Consequences

- Journeys get a voice legible to the author and to future agents: the `.feature` reads as the event's script.
- Scenarios run in milliseconds in-process, so journey growth does not slow the `check` profile the way e2e growth would.
- The in-package fakes and the Node e2e stubs script the same seams (LLM, GitHub) — two scriptings of one contract, accepted for the speed/legibility win.
- Census journeys can be covered either in e2e or in Gherkin; the browser layer is reserved for what genuinely needs a browser (SSE streaming, HTMX wiring, visual theming).

## Alternatives Considered

- **The godog CLI runner** — needs a bootstrap function in package main, lives outside the Go test gates, and duplicates the runner the TestSuite API already provides.
- **Browser-driven Gherkin (e.g. via Playwright steps)** — collapses the two journey layers but pays browser speed for every scenario and drags Node into the binding layer.
- **No Gherkin; e2e only** — the author cannot author; journey intent stays locked in TypeScript.
