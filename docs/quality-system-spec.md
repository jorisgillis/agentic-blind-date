# The Quality System — Spec

This is the assembled, implementation-ready spec produced by the wayfinder
effort ([map](https://github.com/jorisgillis/agentic-blind-date/issues/175)).
Each section records the decision and points at its ADR for context and
alternatives. Wiring the tools and performing the retrofit is the follow-on
execution effort; this document is what it receives.

## The sensor set and thresholds

| Sensor | Threshold | Where it runs | ADR |
|---|---|---|---|
| Coverage | 100% of statements outside `main()` | `check`, blocking, no ratchet | [0008](adr/0008-quality-assurance-approach.md) |
| Formatting / vet | `gofmt` clean, `go vet` clean | `hook` (advisory) + `check` | [0021](adr/0021-single-quality-command.md) |
| Lint | full `golangci-lint` pass, no exclusions | `check`, blocking under a count ratchet | [0021](adr/0021-single-quality-command.md) |
| Tests | suite green, `-shuffle=on -race` | `check`, blocking | [0021](adr/0021-single-quality-command.md) |
| Complexity | CRAP < 6 per function outside `main()` | `check`, blocking under a count ratchet | [0023](adr/0023-crap-complexity-gate.md) |
| Vulnerabilities | `govulncheck` clean (Go 1.26.3 toolchain) | `check`, blocking | [0021](adr/0021-single-quality-command.md) |
| Route census | every route has kind + marker + journey | `check` (census test), blocking | [0022](adr/0022-route-census-table.md) |
| Mutation | zero unexplained LIVED on the 22-file scope | `nightly`, non-blocking | [0024](adr/0024-mutation-testing-decision-logic.md) |
| Browser journeys | Playwright suite green | `e2e`, blocking on PRs | [0021](adr/0021-single-quality-command.md) |
| Secrets | gitleaks clean | separate blocking PR job, outside the bundle | [0021](adr/0021-single-quality-command.md) |

## One quality command

`scripts/quality.sh <profile>` — zero-dependency bash, the single entry
point for agent, commit hook, and CI ([ADR-0021](adr/0021-single-quality-command.md)).

- **`hook`** (~2 s, advisory): `gofmt -l`, `go vet ./...`,
  `golangci-lint run --new-from-rev HEAD`. Installed via a committed
  `.githooks/` dir + absolute `core.hooksPath`; one installer line covers
  all worktrees.
- **`check`** (default, blocking; agent and PR-CI parity): gofmt → vet →
  golangci-lint (full config: v2 defaults + curated gosec + errorlint +
  bodyclose) → `go test -shuffle=on -race -coverprofile=coverage.out .`
  (root package only — the `covdata` gotcha) → coverage gate (blocking, no
  ratchet) → CRAP gate (ratcheted) → `govulncheck`.
- **`e2e`**: wraps the Playwright journey suite.
- **`nightly`** (cron, non-blocking): gremlins on the decision-logic scope
  with `--timeout-coefficient 60`, `--output mutation.json`, plus a CRAP
  trend report.

**Ratchets** (retrofit only): `.quality/lint-baseline` and
`.quality/crap-baseline` are plain count files, gate `current ≤ baseline`,
retrofit commits decrement them, and deletion of the file is the bare
strict gate. Baselines open at ~75 (lint, recomputed with the final
config) and 76 (CRAP).

**CI**: PR + push run `check` and `e2e` as two thin blocking jobs (the lint
job is new); one nightly cron runs `nightly`; gitleaks runs as its own
blocking PR job.

## The route census

A declarative `routes.go` table (pattern, handler, kind, marker, journey) is
the single registration site; `buildMux()` loops over it and the census test
enforces both directions. Per-kind assertions: pages assert their inventory
marker against seeded fixtures, fragments assert their top-level `#id`,
JSON asserts shape, SSE asserts an event payload, binary asserts
content-type + size bounds. The 12 coverage gap groups are journeys
J4–J13 (table in [ADR-0022](adr/0022-route-census-table.md)). Maintenance
rule: no route lands without kind, marker, and journey.
[ADR-0022](adr/0022-route-census-table.md)

## The Gherkin workflow

One `.feature` file per census journey —
`features/journey{n}-{slug}.feature`, mirroring `e2e/journeys` naming —
written in declarative CONTEXT.md domain language (Given past, When
present, Then observable; participants by first name; no routes, form
fields, or selectors in prose). Bound by `godog.TestSuite` inside a
`testing.T` (`go test -run TestGherkinFeatures .`), in-process over the
in-package fakes via the `gherkinWorld` pattern: scenarios run inside
`check` in milliseconds and feed the coverage/CRAP gates. Joris authors
Gherkins for new features; the agent may draft legacy ones for review.
The prototype on `prototype/gherkin-happy-flows` is the adopted seed.
[ADR-0025](adr/0025-gherkin-journey-suite.md)

## The agent self-check protocol

`docs/agents/self-check.md`, linked from AGENTS.md — the matrix any session
applies before finishing: `check` always; census test + table row for route
changes; rendered marker assertions via Playwright MCP on the seed-and-serve
harness for template changes; both themes for CSS; `e2e` for flow changes;
gremlins diff mode when touching scoped mutation files. Standing rule: no
session ends with a page-level change the agent hasn't seen rendered.

## The retrofit

[ADR-0026](adr/0026-retrofit-sequencing.md) is the queue handed to the
execution effort, in full:

- **Step zero**: fix the 4 red theme-WIP coverage functions (the blocking
  coverage gate goes green first).
- **Sweeps, cheapest first**: lint (51 findings) → mutation survivors
  (7 at 3 sites, one slice, kill-or-waive per site, waivers in
  `.quality/mutation-waivers`) → CRAP (76 functions, worst-first).
- **Journeys in parallel**: the 12 gap groups J4–J13, one slice per group,
  a fourth independent track.
- **Slice**: one file's findings of the current sweep type (journeys: one
  gap group).
- **Queue**: derived from gate output — the lint findings list, the
  `scripts/crap` report, `mutation.json`, the census journey column —
  never a maintained ledger.
- **Slice DoD**: the gate stops reporting the slice's findings, ratchets
  only decrease, the self-check protocol passes for the change type.
- **Landing**: one coarse commit per sweep per working session.
- **Exit line**: full `golangci-lint` pass with no exclusions, CRAP green
  repo-wide, census covering every route, zero unexplained LIVED mutants.
  The ratchet files are deleted; the bare strict gates remain.

## Implementation order for the execution effort

1. `scripts/quality.sh` + the four profiles + `.githooks/` + installer +
   CI jobs (the bundle)
2. `routes.go` table + census test over the #179 marker inventory
3. `scripts/crap` ported from `research/crap-tooling` + ratchet files
4. Mutation wiring: gremlins pin, `.gremlins.yaml`, the nightly
   survivor-workflow script
5. Merge `prototype/gherkin-happy-flows` as the Gherkin seed
6. Step zero, then the sweeps and the journey track
