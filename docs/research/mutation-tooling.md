# Mutation testing tooling for Go: validate and pick

Research for wayfinder ticket #176. Question: which mutation-testing tool
does this repo adopt, validated against go 1.26.2 at real scale?

## Verdict

**Adopt gremlins v0.6.0** (`github.com/go-gremlins/gremlins/cmd/gremlins`).
It installs cleanly on Go 1.26 (requires go >= 1.25), pins via go's `tool`
directive (`go get -tool ...`, verified: `go tool gremlins` runs), and
produces a full Killed/Lived/Not covered/Timed out report end to end.

One non-negotiable flag: `--timeout-coefficient 60` (see below). With the
default settings every mutant in this repo is misclassified as TIMED OUT.

Rejected candidates:

- **go-mutesting** (zimmski): abandoned since ~2019. It type-checks the
  source with a 2019-era `golang.org/x/tools` pinned against go 1.22
  internals and panics immediately on this repo (`go/types` crash in
  `packages.loadPackage`). Dead on any modern Go code; not viable.
- **mutatest** (vatine): repository no longer exists on GitHub; not viable.

## The timeout-coefficient footgun

gremlins derives each mutant's test timeout from its measured coverage-gathering
time. On this repo that measurement lands at ~0.2 s (broken, likely a
go 1.26 coverage-profile quirk), so with defaults every mutant is killed by
the clock before the real suite (8.4 s warm, ~63 s cold due to CGO/sqlite3)
can finish.

Measured on `eventstate.go` (36 mutants, `--workers 4`):

| Config | Wall time | Result |
|---|---|---|
| default coefficient | 6.7 s | 36/36 TIMED OUT (test efficacy 0%) |
| `--timeout-coefficient 20` | 39 s | 36/36 TIMED OUT (4 s wall per mutant ≈ the timeout) |
| `--timeout-coefficient 60` | 72 s | **33 KILLED, 0 LIVED, 3 TIMED OUT, efficacy 100%** |

Any scheduled invocation must therefore pass `--timeout-coefficient 60`
(or a `.gremlins.yaml` config carrying the equivalent setting). LIVED
mutants were not observed on eventstate.go (its tests are strong), but
the classification machinery is exercised: Killed/Timed out split
correctly, and gremlins reports each result with file:line.

## Scoping: this repo is a single package

There are no "core domain packages" to scope to: the whole app is one
root `package main` (~30K LOC, 36 non-test files). Scoping is therefore
**per file**, via gremlins' `-E/--exclude-files` filepath regexps. A run
over a core-domain file set is assembled by excluding everything else:

```bash
gremlins unleash -E '^handlers\.go$' -E '^github\.go$' ... --timeout-coefficient 60
```

(Demonstrated live: the eventstate.go run above was scoped exactly this way.)
A `.gremlins.yaml` config or a script holding the exclusion list keeps this
from becoming 36 hand-typed flags.

Two other useful modes validated:

- `--dry-run`: counts mutants per file without running tests, in ~0.1 s.
  Cheap triage: which files are worth mutating, and how big is a run.
- `-D/--diff <ref>`: mutate only what changed vs a ref — the natural
  mechanism for the "mutants in touched files" rule after the retrofit.

## Mutant counts per file (dry run, 2026-10-17)

1098 runnable mutants repo-wide, 49 not covered, mutant coverage 95.73%.

| File | Mutants | | File | Mutants |
|---|---|---|---|---|
| handlers.go | 221 | | board_links.go | 18 |
| github.go | 78 | | llmprovider.go | 15 |
| matcher.go | 73 | | qr.go | 14 |
| showtell.go | 56 | | structured.go | 12 |
| mission.go | 55 | | relationships.go | 12 |
| closing.go | 48 | | meetcode.go | 12 |
| screen.go | 43 | | contact.go | 12 |
| db.go | 41 | | evening.go | 8 |
| llm.go | 40 | | navigation.go | 7 |
| eventstate.go | 36 | | recap.go | 5 |
| meetings.go | 35 | | steps.go | 4 |
| participantstore.go | 34 | | questions.go | 1 |
| slides.go | 28 | | theme.go | 26 |
| interview.go | 26 | | persona.go | 24 |
| main.go | 23 | | slides_handlers.go | 22 |
| showtell_questions.go | 21 | | board_handlers.go | 21 |
| onboarding.go | 19 | | matchmaking.go | 19 |
| cards.go | 19 | | board.go | 19 |

## Runtime and feasibility

Measured: **~2.0 s wall per mutant at `--workers 4`** (eventstate.go: 72 s /
36 mutants; 5.4 effective cores in use — the machine is not saturated, more
workers help).

Extrapolations at `--workers 4`:

- Whole repo (1098 mutants): ~37 min
- Candidate core-domain set (matcher, showtell, mission, closing, screen,
  eventstate, meetings, participantstore, interview, persona, onboarding,
  matchmaking, relationships, recap, structured, evening, meetcode, steps,
  questions ≈ 518 mutants): ~17 min
- Single worst file (handlers.go, 221): ~7 min

All comfortably inside a nightly/weekly scheduled CI job; none of them
belong on a PR gate. `--dry-run` alone (~0.1 s) could even run per PR as
a size signal.

## Machine-readable output

`--output <file>` emits JSON per-mutant results with status and position,
so the surviving-mutant workflow (issue per LIVED mutant during retrofit)
is scriptable straight from the report.

## CI job sketch (for the quality-bundle ticket)

```yaml
# scheduled (cron), never on pull_request
- run: go tool gremlins unleash --timeout-coefficient 60 --workers 4 \
        --output mutation.json $(core-file-exclusion-args)
- run: parse mutation.json → report efficacy; open an issue per LIVED mutant
```

Sources: gremlins v0.6.0 help output and live runs on this worktree
(branch `research/mutation-tooling`); go-mutesting panic trace from a live
run; go 1.26.2 toolchain via go.mod.
