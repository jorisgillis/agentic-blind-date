# 0021: One Quality Command — `scripts/quality.sh`

## Status

Accepted (2026-10-17)

Amends [0008](0008-quality-assurance-approach.md): the coverage gate and test-suite policies stand; this ADR adds the surrounding sensor set, the single entry point, and the enforcement points.

## Context

The app was built fast by agents, and its fragility modes are tests that stay green while pages break, logic regresses, and features silently disappear. Individual gates exist in isolation — a coverage gate (ADR-0008), a Playwright e2e suite, a lint baseline of 51 findings (50 errcheck, 1 ineffassign) with **no lint job in CI at all** — but each is invoked differently by the agent, the commit hook, and CI, so they drift: hooks check one thing, CI another, the agent forgets a third.

Three constraints shape the design:

- The repo stays zero-dependency (ADR-0009): no Node tooling for hooks, no Python.
- Working sessions are concurrent agent sessions, often in linked git worktrees, so per-checkout hook installs silently rot.
- Retrofit is coming: the gates cannot pass on day one, so every strict gate needs a ratchet path from today's baseline to the exit line.

## Decision

**One command, four profiles**: `scripts/quality.sh <profile>`, zero-dependency bash. The agent's self-check, the commit hook, and CI all invoke the same script, so parity is structural, not disciplined. An unknown argument prints usage and exits non-zero.

### Profiles

**`check` (default, blocking — agent and PR-CI parity):**

1. `gofmt -l .` — fail on any output
2. `go vet ./...`
3. `golangci-lint run ./...` — full config from day one: v2 defaults plus curated `gosec`, `errorlint`, `bodyclose` (the ~7 gosec permission rules suppressed); under the ratchet during retrofit
4. `go test -shuffle=on -race -coverprofile=coverage.out .` — **root package only** (a `./...` pattern trips `go: no such tool "covdata"` on Go 1.26.2 when a package has no test files); `-race` stays in `check` per ADR-0020, demoted to `nightly` only if it blows the CI budget once measured
5. coverage gate (`scripts/check-coverage.sh`, ADR-0008) — **blocking, no ratchet**: retrofit step zero fixes the 4 red theme-WIP functions first
6. CRAP gate — `scripts/crap` (ADR-0023) fed `coverage.out`, under the ratchet during retrofit
7. `govulncheck ./...` — blocking, adopted together with a Go toolchain bump so it starts green

> Amended 2026-10-17, at implementation: the bump shipped as **go1.26.9**, not 1.26.3 — on 1.26.3, govulncheck still reported 21 reachable stdlib vulnerabilities (net/http, html/template, crypto/tls, …; fixes spanning 1.26.4–1.26.9). The research-era "1.26.3 cures it" premise was stale; the ADR's remedy (bump to a patch level where govulncheck is green) was applied at today's level. Nothing suppressed.

**`hook` (advisory, ~2 s, run pre-commit):** `gofmt -l`, `go vet ./...`, `golangci-lint run --new-from-rev HEAD`. Tests stay out of the hook: warm cache is 8.4 s but cold is 63 s (CGO/go-sqlite3), and mid-task commits legitimately have red tests.

**`e2e`:** wraps the Playwright journey suite (`e2e/`); the CI job is a thin wrapper and the agent gets the same entry point.

**`nightly` (scheduled cron, non-blocking):** gremlins mutation run scoped to the decision-logic file set (ADR-0024) with `--timeout-coefficient 60` baked in, plus a non-blocking CRAP trend report against the ratchet baseline.

### Ratchet mechanics (retrofit only)

`.quality/lint-baseline` and `.quality/crap-baseline` are plain count files. Gate: `current ≤ baseline`. Retrofit commits decrement them; at zero the file is deleted and the bare strict gate remains — the exit line. The commit hook keeps `--new-from-rev HEAD` for immediate local feedback; CI enforces the global ratchet. Coverage and mutation have **no ratchet**: coverage is blocking from step zero, and the mutation ledger is the surviving-mutant issue queue (ADR-0024).

### Commit-hook mechanism

Plain git hooks via a committed `.githooks/` directory plus `core.hooksPath` set to its **absolute** path (a relative path silently skips hooks in linked worktrees) — one installer line, applies to all worktrees because config is shared. `golangci-lint` is pinned natively with Go's `tool` directive; no external package manager. Hooks are advisory (`--no-verify`, fresh clones); **CI is the enforcement point**.

### CI split

PR + push to master: two thin blocking jobs, `quality.sh check` and `quality.sh e2e` (the lint job is new — none exists today). One nightly cron job: `quality.sh nightly`. Secret scanning is a separate **blocking gitleaks job on PRs, outside the bundle** — it scans repo content, not Go quality.

## Consequences

- The agent's definition of "done" and CI's are the same invocation; drift between them becomes impossible rather than unlikely.
- The coverage gate is red on master until step zero lands — accepted, deliberately: it is why the gate cannot live in a per-commit hook.
- The lint ratchet opens at ~75 (recomputed once with the final config) and only ever decreases; the CRAP ratchet at 76 (ADR-0023).
- Retrofit progress is machine-readable from the two count files; the exit line is their deletion.

## Alternatives Considered

- **pre-commit framework** — the standard answer, but it drags a Python dependency into a Python-free repo and installs hooks per checkout gitdir, so every linked worktree needs a re-install.
- **Husky/Lefthook** — need a root `package.json`, violating ADR-0009's zero-dependency philosophy.
- **Tests in the hook** — rejected on the cold-cache 63 s measurement and mid-task commit reality.
- **Scattered per-gate scripts** (today's state) — the drift this ADR exists to end.
