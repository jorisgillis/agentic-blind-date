# 0023: The CRAP Complexity Gate

## Status

Accepted (2026-10-17)

Amends [0008](0008-quality-assurance-approach.md): adds a per-function complexity gate on top of the coverage gate.

## Context

100% coverage says every line ran, not that the code is maintainable. The codebase carries functions like `Matcher.PairScore` and `Screen.Payload` at cyclomatic complexity 30 — fully covered, fully unrefactorable, and the kind of code agents keep growing. A coverage-only gate is blind to this.

CRAP (Change Risk Analysis and Predictions) is the established composite: `CRAP = comp² × (1 − cov)³ + comp`, coupling complexity with how much of the function a test actually executes. Two Go tools exist (`unclebob/crap4go`, `padiazg/go-crap`); both are thin layers over the same inputs: per-function complexity from `go/ast`, per-function coverage from a `go test -coverprofile` profile.

## Decision

**Gate: CRAP < 6 for every function outside `main()`** (the same exclusion as ADR-0008's coverage gate).

**Tooling: a stdlib-only `scripts/crap`**, pinned in the repo, validated three ways on the research branch: complexity matches `gocyclo` v0.6.0 exactly (0 mismatches over 440 functions), coverage matches `go tool cover -func` exactly, and end-to-end scores match `crap4go` except for one convention — crap4go counts `default:` clauses as +1, gocyclo does not. We adopt the **gocyclo convention** (no `default:` +1), the Go de facto standard. Coverage ≥ 95% is clamped to fully covered. The gate consumes `coverage.out` from the `check` profile (ADR-0021), root package only — on Go 1.26.2 a `./...` coverprofile pattern fails with `go: no such tool "covdata"` when any package lacks test files.

**Gate semantics under ADR-0008's coverage target:** CRAP < 6 is exactly equivalent to **cyclomatic complexity ≤ 5** outside `main()` — CC ≥ 6 fails regardless of coverage (CRAP ≥ CC ≥ 6 at 100%), CC 5 needs > 65.7% coverage, CC 3 needs > 30.7%. The uncertainty term is real but only bites below full coverage (e.g. `ParticipantStore.Change`: CC 20 at 97.1% coverage, CRAP 20.01).

**Baseline (2026-10-17, master @ db24657):** 76 of 440 gated functions fail (17.3%). Worst: `Matcher.PairScore` (CC 30), `Screen.Payload` (CC 30), `ParticipantStore.Change` (CC 20), `renderExportMarkdown` (19), `Meetings.Confirm` (17), `SelectLLM` (15), `selectTiles` (15), `GitHubClient.FetchProfile` (14), `GitHubProfile.githubLines` (14). 41 of the 76 are CC 6–7 — one or two refactors away.

**Ratchet:** the gate runs under the `.quality/crap-baseline` count ratchet during retrofit (ADR-0021); at zero the file is deleted and CRAP < 6 holds repo-wide — an exit criterion.

## Consequences

- With the coverage gate, the rule reduces to CC ≤ 5: simple to reason about, enforced mechanically.
- The retrofit (ADR-0026) inherits a 76-function queue, worst-first, driven by the gate's own report.
- The gate is strict rather than advisory: the retrofit plan exists precisely to reach it.
- `default:` clauses stay free; the three CC-5 boundary functions (`MatchNewcomer`, `ShowTellSignup`, `trimLink`) pass under the chosen convention.

## Alternatives Considered

- **`gocyclo`/`maintidx` standalone** — complexity without the coverage term; at this repo's coverage level, equivalent to the CRAP gate but with no uncertainty signal below 100%.
- **`crap4go` / `go-crap` as dependencies** — unmaintained thin layers over what a 200-line stdlib script does; the +1 `default:` divergence would flip three functions' status for a reason nobody wants to defend.
- **A higher threshold (CRAP < 30)** — matches nothing; PHPUnit's own docs treat 30 as the alarm level. 6 is the strict line that forces refactoring, which is the point.
