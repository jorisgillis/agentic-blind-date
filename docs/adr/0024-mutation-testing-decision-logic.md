# 0024: Mutation Testing on the Decision Logic

## Status

Accepted (2026-10-17)

## Context

Coverage says code ran under tests, not that the tests *decided* anything. The app's risk concentrates in code that makes decisions — pairing, personas, questions, meetings, phases — where a weak assertion is invisible to every other gate. The repo is a single root package, so "core domain packages" scoping has to become per-file scoping. Tool options are thin on the ground: `go-mutesting`'s 2019-era type-checker panics immediately on modern Go; `mutatest`'s repository no longer exists.

`gremlins` v0.6.0 is the one viable tool: it installs cleanly on Go 1.26, pins via the go `tool` directive, and produces a full Killed/Lived/Not-covered/Timed-out report. One measured trap: with default timeout settings gremlins mis-measures the coverage baseline (~0.2 s on this repo) and classifies every mutant as TIMED OUT — verified: 36/36 timed out at defaults and at coefficient 20, while coefficient 60 gave 33 killed / 0 lived / 3 timed out on one file. Scale: 1098 runnable mutants repo-wide, ~2 s wall per mutant at `--workers 4`; whole repo ~37 min.

## Decision

**Adopt gremlins v0.6.0**, pinned via the go `tool` directive, with `--timeout-coefficient 60` mandatory (carried in `.gremlins.yaml`) and `--output mutation.json` for the workflow script. Never a PR gate; nightly only.

**Scope: the 22-file decision-logic union** — the decision deciders (`matcher.go`, `mission.go`, `persona.go`, `interview.go`, `questions.go`, `meetings.go`, `meetcode.go`, `llm.go`, `structured.go`, `github.go`) plus the flow deciders (`showtell.go`, `showtell_questions.go`, `closing.go`, `screen.go`, `eventstate.go`, `participantstore.go`, `onboarding.go`, `matchmaking.go`, `relationships.go`, `recap.go`, `evening.go`, `steps.go`) — 647 runnable mutants. Criterion: code that decides pairing, personas, questions, and meetings; not HTTP handlers, stores, or template wiring.

**Target: zero unexplained LIVED mutants on the scoped set.** LIVED is the only failure state; KILLED and TIMED OUT both count as caught (the clock detected the mutant); NOT COVERED is carried to zero by the coverage gate. No percentage threshold — strict, matching the other exit lines. Measured baseline (2026-10-17, master @ db24657): test efficacy 98.71%, **7 survivors at 3 sites** — `meetings.go:354` and `onboarding.go:157` (CONDITIONALS_NEGATION each), and five on `structured.go:37` (ARITHMETIC_BASE, INVERT_NEGATIVES, CONDITIONALS_BOUNDARY).

**Escape hatch:** `.quality/mutation-waivers` — one line per waived mutant (`file:line:col TYPE — one-line justification`), treated as explained, reviewed like code.

**Survivor workflow:** the nightly run parses `mutation.json` and (a) opens one issue per LIVED mutant not already tracked, title identity `MUT: <file>:<line>:<col> <TYPE>` for dedupe, body carrying position, status, and the reproduction command; (b) comments and closes any tracked issue whose mutant no longer survives; (c) skips waived mutants. **No separate count file: the issue queue is the ledger.** It opens at 7 (3 sites); kill-vs-waiver is decided per site.

**After the exit line:** nightly runs gremlins diff mode (`-D`) against the previous day's main; weekly runs the full 22-file scope as the safety net. Both open issues per new survivor, same workflow. Mutation stays never-PR-blocking.

**Touched-files self-check:** a session changing a scoped file runs the profile's diff mode against its own diff before finishing (see the self-check protocol) — regressions caught at authoring time, not a day later in the queue.

## Consequences

- A green suite now means the assertions discriminate, at least on the code that decides.
- The nightly job (~23 min measured at `--workers 8`) is the price; non-blocking by design.
- Waivers are debt with an owner and a justification; the exit line tolerates explained survivors only.
- The workflow script itself is a retrofit slice (ADR-0026).

## Alternatives Considered

- **`go-mutesting` / `mutatest`** — dead: panic on modern Go / repository gone.
- **Percentage mutation threshold** — a 98.71% baseline says "good enough" while hiding a real survivor class; strict zero-unexplained is auditable.
- **PR-blocking mutation** — ~2 s/mutant makes any PR gate minutes-to-hours; the diff-mode self-check gives authoring-time feedback instead.
- **Default timeout settings** — demonstrably classify every mutant as TIMED OUT; coefficient 60 is non-negotiable.
