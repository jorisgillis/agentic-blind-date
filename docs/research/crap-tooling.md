# CRAP tooling for Go and a CRAP < 6 baseline

Research for issue #177. Question: is CRAP computed for Go by an existing
tool or a hand-rolled script over a complexity tool plus the coverage
profile? Validate the formula, run it repo-wide on a fresh profile, and
report what a CRAP < 6 gate means for today's codebase.

Run date: 2026-10-17. Baseline: `db24657` (master). Toolchain: go1.26.2
linux/amd64, CGO on (go-sqlite3). Fresh profile from
`go test -coverprofile=/tmp/crap-coverage.out ./...` — suite passes in
18.2s, 98.8% of statements.

## Short answer

Both exist. The formula is small enough (one line) that every Go
implementation is a thin hand-rolled layer over the same two inputs:
per-function cyclomatic complexity from `go/ast` and per-function
coverage from a `go test -coverprofile` profile.

```
CRAP = comp^2 * (1 - cov)^3 + comp      (cov as a fraction of 1)
```

Existing tools found:

| Tool | Formula | Complexity convention | Notes |
|---|---|---|---|
| [`unclebob/crap4go`](https://github.com/unclebob/crap4go) | `cc^2*(1-cov)^3+cc` | counts `default:` clauses as +1 | runs `go test ./...` itself, prints a worst-first report |
| [`padiazg/go-crap`](https://github.com/padiazg/go-crap) | `cc^2*(1-cov)^3+cc` | gocyclo-identical | also merges mutation data, baseline diffs |
| this repo: `scripts/crap` (this branch) | same | gocyclo-identical | stdlib only, ~200 lines |

All three agree on the formula. They differ only on the `default:`
clause convention (see below).

## The formula and its provenance

The CRAP metric (Change Risk Anti-Pattern) combines cyclomatic
complexity with code coverage: complex code that is also untested is
the riskiest to change. The canonical, still-maintained implementation
is PHPUnit's `php-code-coverage`
([`src/Node/CrapIndex.php`](https://github.com/sebastianbergmann/php-code-coverage/blob/master/src/Node/CrapIndex.php),
the metric originated with Alberto Savoia and Bob Evans' crap4j):

```php
$crap = $cyclomaticComplexity ** 2 * (1 - $codeCoverage / 100) ** 3 + $cyclomaticComplexity;
```

with two implementation quirks in PHPUnit:

- coverage 0 → `comp^2 + comp` (same value as the formula),
- coverage >= 95% is clamped to fully covered → CRAP = comp.

The quirk is immaterial for a CRAP < 6 gate: clamping only changes
results when `comp >= 6` (CRAP >= 6, fails either way) or `comp <= 5` at
>= 95% coverage (CRAP < 6 either way). The clamp never flips a pass/fail
decision at this threshold.

The two Go tools' own sources confirm the identical formula:

- crap4go `internal/crap/crap.go`:
  `score := cc*cc*uncov*uncov*uncov + cc` with `uncov = 1 - cov/100`.
- go-crap `internal/score/score.go`:
  `comp*comp*(1-cov)*(1-cov)*(1-cov) + comp`.

"Coverage uncertainty" in the ticket is the `(1 - cov)^3` term: CRAP
grows cubically as coverage drops, so uncovered code is punished far
more than its complexity alone would.

## Validation of the hand-rolled script

`scripts/crap/main.go` on this branch computes, per named function
(`_test.go` excluded): gocyclo-style cyclomatic complexity from the AST,
statement coverage from the profile blocks mapped onto the function's
line range, then the CRAP formula.

Cross-validation, all against the fresh profile:

| Cross-check | Result |
|---|---|
| Coverage vs `go tool cover -func` | identical for 440/441 functions (main excluded by display format only) |
| Complexity vs `gocyclo` v0.6.0 | 0 mismatches over 440 functions |
| Whole scores vs `crap4go` | identical except +1 per `default:` clause |
| Formula vs PHPUnit reference values | matches: (2, 0%) = 6, (3, 100%) = 3, (4, 50%) = 6.00 |

The cross-validation earned its keep: the first version of the script
forgot the base of 1 in the complexity count, making every function
one decision point short of gocyclo. The uniform +1 gap in the
comparison exposed it; the fix took the failing count from 60 to 76.

### Convention differences that matter at the boundary

- `default:` clauses: gocyclo, go-crap, and this script do NOT count
  them (+0); crap4go counts each as +1. Three functions sit exactly on
  this boundary at CC 5 with one `default:` each —
  `Matcher.MatchNewcomer`, `Handler.ShowTellSignup`, `trimLink` — they
  pass the CRAP < 6 gate under the gocyclo convention and fail under
  crap4go's. The gate spec (#175) should name its convention; gocyclo's
  is the de facto Go standard and is what this baseline uses.
- Function literals: attributed to the enclosing named function here
  (so closures cannot hide complexity from the gate). Moot today: a
  gocyclo scan shows no function literals in production code, so both
  conventions give identical numbers.

## Repo-wide results (fresh profile, 2026-10-17)

- 441 functions scored (440 outside `main()` plus `main()` itself).
- Gate `CRAP < 6`, with `main()` excluded exactly as ADR-0008 excludes
  it from the coverage gate: **76 of the 440 gated functions fail
  (17.3%)**.

At today's near-100% coverage, CRAP is effectively equal to cyclomatic
complexity, so the gate is a CC <= 5 gate with one exception: the
uncertainty term only visibly moves `ParticipantStore.Change` (97.1%
covered, CRAP 20.01 vs 20.00).

### What CRAP < 6 demands, per complexity

| CC | Passes CRAP < 6 iff coverage ... | Note |
|---|---|---|
| 1 | always | CRAP <= 2 |
| 2 | > 0% | any coverage at all |
| 3 | > 30.7% | |
| 4 | > 50% | |
| 5 | > 65.7% | |
| 6+ | never | CRAP >= CC >= 6 even at 100% |

So with ADR-0008's 100%-outside-main target holding, "CRAP < 6" is
strictly equivalent to "cyclomatic complexity <= 5 outside main()".

### Worst offenders (top 20 of 76)

| Function | CC | Cov | CRAP |
|---|---|---|---|
| matcher.go: (*Matcher).PairScore (L255) | 30 | 100.0% | 30.00 |
| screen.go: (*Screen).Payload (L125) | 30 | 100.0% | 30.00 |
| participantstore.go: (*ParticipantStore).Change (L141) | 20 | 97.1% | 20.01 |
| closing.go: renderExportMarkdown (L399) | 19 | 100.0% | 19.00 |
| meetings.go: (*Meetings).Confirm (L159) | 17 | 100.0% | 17.00 |
| llmprovider.go: SelectLLM (L45) | 15 | 100.0% | 15.00 |
| mission.go: selectTiles (L290) | 15 | 100.0% | 15.00 |
| github.go: (*GitHubClient).FetchProfile (L91) | 14 | 100.0% | 14.00 |
| github.go: (*GitHubProfile).githubLines (L176) | 14 | 100.0% | 14.00 |
| db.go: scanParticipant (L240) | 12 | 100.0% | 12.00 |
| llm.go: (*OpenAIClient).doChat (L108) | 12 | 100.0% | 12.00 |
| matchmaking.go: (*Matchmaking).matchOne (L112) | 12 | 100.0% | 12.00 |
| showtell_questions.go: (*ShowTell).TransitionQuestion (L199) | 12 | 100.0% | 12.00 |
| closing.go: (*Closing).BestMatches (L251) | 11 | 100.0% | 11.00 |
| eventstate.go: (*EventStore).Reset (L265) | 11 | 100.0% | 11.00 |
| handlers.go: computeBadges (L24) | 11 | 100.0% | 11.00 |
| handlers.go: (*Handler).Meet (L755) | 11 | 100.0% | 11.00 |
| handlers.go: (*Handler).recapData (L1080) | 11 | 100.0% | 11.00 |
| mission.go: differenceScore (L392) | 11 | 100.0% | 11.00 |
| showtell.go: (*ShowTell).Move (L349) | 11 | 100.0% | 11.00 |

Failing distribution by CC: 6x16, 7x25, 8x7, 9x6, 10x2, 11x7, 12x4,
14x2, 15x2, 17x1, 19x1, 20x1, 30x2. The bulk (41 of 76) is CC 6-7, i.e.
functions one or two refactors away from passing; the long tail
(CC >= 12) is where the real decomposition work is. The full 76-row
report is in the Appendix.

Related observation: four functions are below 100% coverage today
(theme work-in-progress on master) — `SaveTheme` 77.8%,
`AdminPublicTheme` 50%, `themeByName` 85.7%, `Change` 97.1%. The
ADR-0008 gate is therefore currently red on master. None of the
low-CC ones fails CRAP < 6 (e.g. `themeByName`: CC 5 at 85.7% → CRAP
5.07, passes), which illustrates the gate's tolerance: coverage gaps
are survivable at CC <= 5, and no coverage figure rescues CC >= 6.

## Gotchas for the quality bundle (#182)

1. Go 1.26.2 + `go test -coverprofile ./...` breaks with
   `go: no such tool "covdata"` when the pattern includes a package
   with no test files (reproduced: `go test -coverprofile=/tmp/y.out
   ./scripts/crap` fails; the same pattern passed before the
   subpackage existed). The production code is a single root package,
   so scope the quality command to `go test -coverprofile=coverage.out .`
   — or give any new subpackage a test file.
2. If a CRAP tool is adopted rather than the script, exclude tooling
   directories from its scan (crap4go skips `target/` and `_test.go`
   but would score `scripts/` if pointed at `./...`).
3. crap4go's `default:` counting makes it 3 boundary functions
   stricter than gocyclo at this threshold; decide the convention once
   and pin the tool version.

## The script

`scripts/crap/main.go` — stdlib only (go/ast, go/parser, plus a
~40-line coverage-profile parser). Usage:

```sh
go test -coverprofile=coverage.out .
go run ./scripts/crap -profile coverage.out -gate 6   # -all for every row
```

Exit code is 0 regardless; the report lists offenders worst-first.
Wiring a nonzero exit for gate enforcement is a one-line change for
#182, deliberately not made on this throwaway research branch.

## Sources

- PHPUnit / php-code-coverage `CrapIndex.php` (canonical formula,
  95% clamp): https://github.com/sebastianbergmann/php-code-coverage/blob/master/src/Node/CrapIndex.php
- unclebob/crap4go formula:
  https://github.com/unclebob/crap4go/blob/master/internal/crap/crap.go
- unclebob/crap4go complexity (counts `default:`):
  https://github.com/unclebob/crap4go/blob/master/internal/complexity/complexity.go
- padiazg/go-crap formula:
  https://github.com/padiazg/go-crap/blob/master/internal/score/score.go
- padiazg/go-crap complexity (gocyclo rules):
  https://github.com/padiazg/go-crap/blob/master/internal/complexity/complexity.go
- fzipp/gocyclo complexity rules (the Go de facto standard):
  https://github.com/fzipp/gocyclo/blob/master/complexity.go
- Repo: ADR-0008 (100% coverage gate, main() excluded),
  scripts/check-coverage.sh, map issue #175 (CRAP < 6 decision).

## Appendix: all 76 gate failures (worst first)

```
FUNCTION                       CC     COV      CRAP
------------------------------------------------------
matcher.go: *Matcher.PairScore (L255)   30  100.0%     30.00  (score >= 6)
screen.go: *Screen.Payload (L125)   30  100.0%     30.00  (score >= 6)
participantstore.go: *ParticipantStore.Change (L141)   20   97.1%     20.01  (score >= 6)
closing.go: renderExportMarkdown (L399)   19  100.0%     19.00  (score >= 6)
meetings.go: *Meetings.Confirm (L159)   17  100.0%     17.00  (score >= 6)
llmprovider.go: SelectLLM (L45)   15  100.0%     15.00  (score >= 6)
mission.go: selectTiles (L290)   15  100.0%     15.00  (score >= 6)
github.go: *GitHubClient.FetchProfile (L91)   14  100.0%     14.00  (score >= 6)
github.go: *GitHubProfile.githubLines (L176)   14  100.0%     14.00  (score >= 6)
db.go: scanParticipant (L240)   12  100.0%     12.00  (score >= 6)
llm.go: *OpenAIClient.doChat (L108)   12  100.0%     12.00  (score >= 6)
matchmaking.go: *Matchmaking.matchOne (L112)   12  100.0%     12.00  (score >= 6)
showtell_questions.go: *ShowTell.TransitionQuestion (L199)   12  100.0%     12.00  (score >= 6)
closing.go: *Closing.BestMatches (L251)   11  100.0%     11.00  (score >= 6)
eventstate.go: *EventStore.Reset (L265)   11  100.0%     11.00  (score >= 6)
handlers.go: computeBadges (L24)   11  100.0%     11.00  (score >= 6)
handlers.go: *Handler.Meet (L755)   11  100.0%     11.00  (score >= 6)
handlers.go: *Handler.recapData (L1080)   11  100.0%     11.00  (score >= 6)
mission.go: differenceScore (L392)   11  100.0%     11.00  (score >= 6)
showtell.go: *ShowTell.Move (L349)   11  100.0%     11.00  (score >= 6)
main.go: loadDotEnv (L13)      10  100.0%     10.00  (score >= 6)
slides.go: *SlideDecks.Add (L60)   10  100.0%     10.00  (score >= 6)
closing.go: *Closing.Stats (L41)    9  100.0%      9.00  (score >= 6)
handlers.go: *Handler.phaseHeaderStatus (L452)    9  100.0%      9.00  (score >= 6)
handlers.go: *Handler.ShowTellStage (L1289)    9  100.0%      9.00  (score >= 6)
handlers.go: *Handler.ShowTellQuestionAction (L1356)    9  100.0%      9.00  (score >= 6)
handlers.go: *Handler.DeleteParticipant (L1729)    9  100.0%      9.00  (score >= 6)
showtell.go: *ShowTell.Stage (L246)    9  100.0%      9.00  (score >= 6)
board_handlers.go: *Handler.BoardPost (L49)    8  100.0%      8.00  (score >= 6)
handlers.go: *Handler.ConfirmMeeting (L811)    8  100.0%      8.00  (score >= 6)
handlers.go: *Handler.WaitStream (L1863)    8  100.0%      8.00  (score >= 6)
llm.go: detailText (L203)       8  100.0%      8.00  (score >= 6)
matcher.go: *Matcher.greedyMatch (L496)    8  100.0%      8.00  (score >= 6)
mission.go: *Missions.GenerateFor (L87)    8  100.0%      8.00  (score >= 6)
slides.go: *SlideDecks.Sweep (L218)    8  100.0%      8.00  (score >= 6)
board.go: *Board.Post (L137)    7  100.0%      7.00  (score >= 6)
board_handlers.go: *Handler.AdminBoard (L128)    7  100.0%      7.00  (score >= 6)
db.go: NewDB (L64)              7  100.0%      7.00  (score >= 6)
github.go: *GitHubProfile.extraLines (L222)    7  100.0%      7.00  (score >= 6)
handlers.go: *Handler.MatchStream (L906)    7  100.0%      7.00  (score >= 6)
handlers.go: *Handler.showtellData (L1224)    7  100.0%      7.00  (score >= 6)
handlers.go: *Handler.watch (L1794)    7  100.0%      7.00  (score >= 6)
handlers.go: *Handler.PipelineStream (L1822)    7  100.0%      7.00  (score >= 6)
interview.go: *Interview.Submit (L125)    7  100.0%      7.00  (score >= 6)
interview.go: extraAnswersFrom (L171)    7  100.0%      7.00  (score >= 6)
interview.go: interviewText (L233)    7  100.0%      7.00  (score >= 6)
llm.go: errorText (L176)        7  100.0%      7.00  (score >= 6)
matcher.go: *Matcher.followNote (L213)    7  100.0%      7.00  (score >= 6)
matcher.go: *Matcher.MatchPool (L353)    7  100.0%      7.00  (score >= 6)
matchmaking.go: *Matchmaking.Rematch (L46)    7  100.0%      7.00  (score >= 6)
meetcode.go: ensureMeetCode (L47)    7  100.0%      7.00  (score >= 6)
navigation.go: destination (L11)    7  100.0%      7.00  (score >= 6)
onboarding.go: *Onboarding.becomeReady (L138)    7  100.0%      7.00  (score >= 6)
qr.go: *Handler.QR (L41)        7  100.0%      7.00  (score >= 6)
relationships.go: *Relationships.Pair (L36)    7  100.0%      7.00  (score >= 6)
screen.go: *Screen.presenter (L320)    7  100.0%      7.00  (score >= 6)
showtell.go: *ShowTell.Signup (L132)    7  100.0%      7.00  (score >= 6)
showtell.go: *ShowTell.Signature (L227)    7  100.0%      7.00  (score >= 6)
showtell.go: *ShowTell.RevealIdentity (L290)    7  100.0%      7.00  (score >= 6)
showtell_questions.go: *ShowTell.Ask (L127)    7  100.0%      7.00  (score >= 6)
board.go: *Board.Moderate (L185)    6  100.0%      6.00  (score >= 6)
board_handlers.go: *Handler.boardData (L26)    6  100.0%      6.00  (score >= 6)
closing.go: *Closing.Pack (L316)    6  100.0%      6.00  (score >= 6)
handlers.go: *Handler.SubmitAnswer (L263)    6  100.0%      6.00  (score >= 6)
handlers.go: *Handler.Wait (L295)    6  100.0%      6.00  (score >= 6)
handlers.go: *Handler.phaseHeaderQueueBadge (L427)    6  100.0%      6.00  (score >= 6)
handlers.go: *Handler.ShowTellQuestion (L1261)    6  100.0%      6.00  (score >= 6)
llm.go: *OpenAIClient.Chat (L229)    6  100.0%      6.00  (score >= 6)
matcher.go: *Matcher.mutualFollow (L185)    6  100.0%      6.00  (score >= 6)
matcher.go: *Matcher.candidatePairs (L477)    6  100.0%      6.00  (score >= 6)
meetcode.go: backfillMeetCodes (L80)    6  100.0%      6.00  (score >= 6)
mission.go: *Missions.generate (L157)    6  100.0%      6.00  (score >= 6)
mission.go: *MissionAgent.ask (L465)    6  100.0%      6.00  (score >= 6)
persona.go: nextLook (L106)     6  100.0%      6.00  (score >= 6)
showtell.go: *ShowTell.Done (L320)    6  100.0%      6.00  (score >= 6)
slides_handlers.go: *Handler.AdminSlidesUpload (L89)    6  100.0%      6.00  (score >= 6)

441 functions scored, 76 fail the CRAP < 6 gate (main excluded).
```
