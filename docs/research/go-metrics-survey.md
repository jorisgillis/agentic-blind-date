# Go static-analysis and metrics survey (with a lint baseline)

Wayfinder research for #178 (parent: #175). Question: which static-analysis and
metric signals does this repo adopt beyond lint, what does `golangci-lint` find
as-is, and what should the adoption set be?

## Method

All commands ran on 2026-10-17 against `master` (db24657) in this worktree, on
Linux, Go 1.26.2, CGO enabled (required by `mattn/go-sqlite3`). None of the
tools below were previously installed or configured in the repo; `golangci-lint`
ran with **no config file**, i.e. its v2 default linter set (errcheck, govet,
ineffassign, staticcheck, unused). Versions: `golangci-lint` v2.15.0,
`govulncheck` v1.9.0 (vuln DB 2026-10-08), `gocyclo` (fzipp) latest, plus the
toolchain's `go vet`, `gofmt`, and `go test`.

Codebase at this commit: 76 commits, 9,810 LOC production (single package
`main`), 20,664 LOC tests, 35+ `*_test.go` files.

## 1. Baseline: `golangci-lint` as-is (default linters)

`golangci-lint run ./...` (v2 defaults) exits **1** with **51 issues**:

| Linter | Findings |
|---|---|
| errcheck | 50 |
| ineffassign | 1 |
| govet | 0 |
| staticcheck | 0 |
| unused | 0 |

This is the input for the retrofit's full-pass exit line: after the errcheck /
ineffassign fixes land, a bare `golangci-lint run ./...` (no config) must exit 0.

errcheck split: **11 production / 39 test**.

Production errcheck sites (all `Close`/`Scan`/`Exec` return values, classic
`defer x.Close()` territory):

```
board.go:122     rows.Close
contact.go:52    rows.Close
contact.go:170   rows.Close
db.go:209,221    sqlDB.Exec        (migration statements)
db.go:311,418,426 rows.Scan       (existence probes)
db.go:339        tx.Rollback
github.go:277,330 resp.Body.Close
```

Test errcheck by file: `db_test.go` 21, `eventstate_test.go` 4,
`handler_errors_test.go` 3, `closing_test.go` 3, `cards_test.go` 2,
`board_test.go` 2, and 1 each in `faults_test.go`, `dossier_test.go`,
`contact_test.go`, `adapters_test.go`. Mostly `Close`/`Exec` in helpers.

The single ineffassign is `matcher.go:163` (`profile = &GitHubProfile{}` is
dead); `wastedassign` flags the same site, so one fix closes both.

`go vet ./...` and `gofmt -l .` are both clean. The codebase is otherwise in
good shape for a cold lint adoption.

## 2. Extended lint survey (candidate linters, finding counts)

One extra run with `--enable-only` over a broad candidate set
(staticcheck+errcheck included as controls; counts are independent findings):

| Linter | Findings | Nature |
|---|---|---|
| gosec | 23 | security, real signal (see below) |
| revive | 17 | style (16 unused-parameter, 1 package-comment) |
| usestdlibvars | 22 | style (`http.StatusOK` vs `"200"`) |
| errcheck | 50 | correctness (baseline above) |
| errorlint | 5 | correctness (`==`/type-assert on errors, `%v` instead of `%w`) |
| gocritic | 6 | 2 unlambda, 1 mapKey, 1 exitAfterDefer, 2 offBy1 |
| unparam | 4 | all in test helpers (fixed test inputs) |
| bodyclose | 3 | `httptest` response bodies in tests |
| wastedassign | 1 | same site as ineffassign |
| staticcheck | 0 | clean |
| govet / ineffassign | 0 / 1 | baseline |
| **dupl** | **0** | zero duplicated blocks at the default 150-token threshold |
| **gocyclo** | **0** | no function over the default complexity threshold of 30 |
| **maintidx** | **0** | no function over the default maintainability index threshold |
| unconvert, nakedret, predeclared, noctx, sqlclosecheck, rowserrcheck, contextcheck | 0 | clean |

Notable gosec findings (public-facing app, user-controlled GitHub handles are
fetched server-side, so these matter):

- G203 (x2): `template.HTML` / non-escaped output in handlers — XSS surface,
  needs a case-by-case audit, not a blanket suppress.
- G704 (x3) + G710 (x3): taint-analysis SSRF (`github.go` — outbound fetch on
  user input) and open redirects. The SSRF ones are inherent to the feature
  (calling the GitHub API with a participant's handle); worth an explicit
  allowlist decision.
- G124 (x3): cookies missing `Secure`/`HttpOnly`/`SameSite`.
- G112: no `ReadHeaderTimeout` on `http.Server` (Slowloris).
- G120: unbounded form parsing.
- G404: `math/rand` where unpredictability may matter (`meetcode`-style codes).
- G301/G302/G306 (x7): file permission pedantry, mostly in test/fixture code.
- G104, G304, G203: single instances each.

## 3. govulncheck

`govulncheck ./...` exits 3: **25 vulnerabilities reachable from this code,
all from the Go standard library at go1.26.2** (fixed in go1.26.3 — the
toolchain is one patch release behind). Example reachability paths are the
GitHub client (`http.Client.Do`) and `http.Server.ListenAndServe`, i.e. the two
real network surfaces. Additionally 5 vulnerabilities in imported packages and
4 in required modules are **not called** by this code.

Full reachable ID list: GO-2026-4918, GO-2026-4971, GO-2026-4980, GO-2026-4982,
GO-2026-5026, GO-2026-5037, GO-2026-5039, GO-2026-5856, GO-2026-5972,
GO-2026-6089, GO-2026-6090, GO-2026-6091, GO-2026-6218, GO-2026-6599,
GO-2026-6600, GO-2026-6603, GO-2026-6605, GO-2026-6607, GO-2026-6608,
GO-2026-6609, GO-2026-6610, GO-2026-6611, GO-2026-6612, GO-2026-6613,
GO-2026-6617.

Interpretation: the only remediation needed right now is bumping the toolchain
to 1.26.3; there is no dependency-level exposure. This makes govulncheck cheap
to adopt: a green run is one toolchain bump away.

## 4. Duplication

`dupl` (golangci-lint's built-in duplication linter) reports **0** at its
default 150-token threshold, in both production and test code. Given the
coverage-gated suite (ADR-0008), tests intentionally mirror scenarios, but not
at duplicated-block granularity. There is no signal to gate on; no separate
duplication tool (jscpd, PMD) is warranted for a ~10K LOC production codebase.
Templates (Go `html/template`) are outside dupl's reach, but the theme-role
system (recent commits) already consolidates styling.

## 5. Churn / hotspot

Churn = added+deleted lines per file over the full 76-commit history
(`git log --numstat`), current-file size from `wc -l`:

| File | Churn | Commits | Current LOC |
|---|---|---|---|
| handlers.go | 4,245 | 43 | 1,919 |
| db.go | 1,432 | 19 | 486 |
| matcher.go | 904 | 8 | 518 |
| github.go | (top-20) | — | 337 |
| agents.go | 2,680 | 19 | deleted in #86 |

Test files show high churn too (mission_test 1,718; showtell_test 1,413) but
mostly single large writes, not repeated rework. `agents.go` churn is
historical: the file was deleted in 8177612 (Refactor/interview and matcher,
#86).

`handlers.go` is the unambiguous hotspot: 43 of 76 commits touched it, it
accumulated 4,245 churn lines, and it is by far the largest file (1,919 LOC —
roughly 20% of production code). Complexity stays moderate: the highest
cyclomatic numbers in the tree are `(*Screen).Payload` (30, screen.go:125) and
`(*Matcher).PairScore` (30, matcher.go:255); nothing exceeds the default gocyclo
gate, and maintidx is clean. So the hotspot signal is **size + churn**, not
complexity: the retrofit's decomposition work (issue #182's bundle) should
target `handlers.go` first; `ParticipantStore.Change` (20, participantstore.go:141)
and `renderExportMarkdown` (19, closing.go:399) are the next candidates.

## 6. Flakiness

Probes run on the stdlib-only suite (~15.4s wall for one `go test ./...`):

- `go test . -count=3 -shuffle=on` — passes. No order-dependence or
  cross-run-state signal in this probe.
- `go test -race . -count=1` — passes. No data races, notable because the app
  runs a background LLM pipeline with a shared concurrency bound (ADR-0020).
- Playwright e2e (`e2e/playwright.config.ts`): `fullyParallel: true`, 2 workers
  in CI, **no retries configured** — failures fail, which is the right default
  for a young suite.

No flake history exists (no retry/quarantine configuration anywhere). The
cheap durable adoptions are `-shuffle=on` in the CI test step (near-zero cost,
catches order coupling early) and keeping the no-retry Playwright policy. A
dedicated flake-detection tool (e.g. flake hunters) is overkill at this size.

## 7. Recommended adoption set

Blocking gates (the quality bundle, #182):

1. **golangci-lint with the v2 default set** (errcheck, govet, ineffassign,
   staticcheck, unused) — the exit line for the full pass is: bare
   `golangci-lint run ./...` exits 0 after fixing the 51 baseline findings
   (50 errcheck — 11 prod, 39 test — and 1 ineffassign).
2. **gosec** — the only lint with new, decision-worthy signal (23 findings).
   Adopt with a small curated exclusion list (file-permission rules G301/G302/
   G306 and test-fixture G304 are noise here); explicitly resolve rather than
   suppress G203 (HTML escaping), G124 (cookies), G112 (server timeouts),
   G120 (form bounds), G404 (weak random), and the G704/G710 taint findings.
3. **govulncheck** in CI (push or scheduled) — currently 25 reachable stdlib
   vulns, all resolved by bumping Go to 1.26.3; adopt the gate together with
   that bump so it starts green.
4. **errorlint + bodyclose** — 8 combined findings, all mechanically fixable
   (`errors.Is`/`errors.As`, `%w`, closing test bodies). Correctness-adjacent,
   low noise.

Adopt as non-blocking conventions:

5. **`go test -shuffle=on`** in CI — free flakiness tripwire.
6. **`go vet` + `gofmt -l`** — already clean; keep as implicit baseline (go vet
   is subsumed by golangci-lint anyway).

Do not adopt (measured zero or negative signal at this size):

7. **dupl / gocyclo / maintidx** — 0 findings at defaults. Revisit gocyclo only
   if `handlers.go` decomposition regresses.
8. **revive / usestdlibvars / gocritic / unparam / wastedassign** — stylistic
   (57 findings across the four). If the team wants a style pass, take
   usestdlibvars (22, mechanical) and skip the rest; unparam's 4 findings are
   test helpers by design.
9. **Standalone duplication or flake-hunting tools** — no signal, added
   maintenance for a ~10K LOC production codebase.

Reasoning in one paragraph: this repo's real quality lever is already the
100%-coverage gate plus fault injection (ADR-0008); lint therefore only needs
to add what tests cannot see — security posture (gosec, govulncheck) and
error-handling correctness (errcheck, errorlint, bodyclose) — while the metric
surveys say the code is neither duplicated, nor overly complex, nor flaky, and
that the one genuine hotspot is `handlers.go` (size+churn, not complexity).

Sources: direct tool runs in this worktree (golangci-lint v2.15.0 default +
`--enable-only` survey; govulncheck v1.9.0; gocyclo; `go vet`; `gofmt`;
`go test -count=3 -shuffle=on`; `go test -race`; `git log --numstat`),
golangci-lint docs for the v2 default linter set and linter thresholds
(https://golangci-lint.run), govulncheck docs (https://go.dev/blog/vuln /
pkg.go.dev/golang.org/x/vuln/cmd/govulncheck), ADR-0008 for the coverage gate,
ADR-0020 for the shared LLM concurrency bound, and `e2e/playwright.config.ts`.
