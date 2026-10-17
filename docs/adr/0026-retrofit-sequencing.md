# 0026: The Retrofit — Sequencing, Slices, and the Derived Queue

## Status

Accepted (2026-10-17)

## Context

The gates land with non-zero baselines: 51 lint findings (50 errcheck, 1 ineffassign), 76 functions failing CRAP < 6 (ADR-0023), 7 surviving mutants at 3 sites (ADR-0024), and 12 census journey gap groups J4–J13 (ADR-0022). Wiring the tooling and performing this retrofit is the follow-on execution effort; this ADR is the queue it receives. The work must survive concurrent agent sessions landing feature work on master throughout.

## Decision

**Sweep order — finding-type sweeps, cheapest to most expensive:**

1. **Lint** — 51 mechanical fixes (errcheck, one ineffassign), near-zero regression risk; drops the ratchet fast and leaves a lint-clean tree for everything after
2. **Mutation survivors** — all 7 at 3 sites as one slice; kill-or-waive decided per site
3. **CRAP** — 76 refactors of fully-covered code, the expensive tail, **worst-first** (`Matcher.PairScore` CC 30, `Screen.Payload` CC 30, `ParticipantStore.Change` CC 20, …): the riskiest refactors land while the tests are freshest, and the top offenders are 100%-covered — the safest big refactors available

**Journeys are a fourth, independent track.** The CRAP failures are 100%-covered functions: journey coverage is not a precondition for refactoring them. The 12 gap groups (J4–J13) are a separate track, one slice per gap group, runnable in parallel with the sweeps. Step zero of the whole retrofit: fix the 4 red theme-WIP coverage functions so the blocking coverage gate goes green.

**Slice shape:** one slice = one file's findings of the current sweep type (journeys: one gap group). The gate reports per file, making slices reviewable and self-delimiting.

**The queue is derived, never maintained.** Each sweep's queue is whatever its gate currently reports — the lint findings list, the `scripts/crap` report, `mutation.json` survivors, the census test's missing journeys. No per-slice issues, no tracking doc; this generalizes the mutation decision that the ledger is the queue. A hand-maintained ledger would drift from the gates the first week.

**Definition of done for a slice — no bespoke criteria:**

1. The sweep's gate no longer reports the slice's findings
2. Ratchets only ever decrease (ADR-0021's monotonic rule)
3. The agent self-check protocol passes for the change type (`quality check` always, e2e for flow changes, marker assertions for page/template changes)

**Landing: one coarse commit per sweep per working session.** A session squashes its completed slices of the current sweep into a single commit at session end. Explicitly not per-slice commits, and not one commit held until an entire multi-day sweep completes — the latter would collide with concurrent feature work and leave days of uncommitted refactors in working trees. Gate deltas stay coarse; the tree never holds week-old work.

**Exit criteria (the line where the retrofit ends):** full `golangci-lint` pass with no exclusions, the CRAP gate green repo-wide, census covering every route with a passing journey per column, and zero unexplained LIVED mutants on the scoped set. The ratchet files are deleted; the bare strict gates remain.

## Consequences

- The execution effort receives an ordering, a slicing rule, a DoD, and a landing convention — but no queue to maintain; it reads its work straight from gate output.
- Cheap wins land first: after the lint sweep the ratchet is at zero and the bare strict lint gate holds from then on.
- Worst-first CRAP sequencing front-loads risk while energy is high; the 41 CC 6–7 functions at the tail are the mechanical bulk.
- Coarse commits trade bisectability for fewer, larger, session-shaped commits — accepted by the author.

## Alternatives Considered

- **Per-domain-area slices** — mix finding types in one slice, violating the cheap-to-expensive ordering and re-linting the same files repeatedly.
- **Per-function slices** — 76 one-function units of overhead for CRAP alone.
- **Maintained ledger / per-slice issues** — drifts from gate truth; the gates already enumerate their findings.
- **Per-slice commits** — rejected in favor of coarse session commits (author's call).
- **One commit per whole sweep** — unlanded work piling up across days of concurrent sessions; rejected.
