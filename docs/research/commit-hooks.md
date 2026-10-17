# Commit-hook mechanism for this Go repo

Research for issue #180. Question: which commit-hook mechanism fits this repo,
what can realistically run inside a hook without making commits miserable, and
what must remain in CI?

## TL;DR — Recommendation

- **Mechanism: plain git hooks via `core.hooksPath`, set to an absolute path by a
  one-line setup script.** Not the pre-commit framework (adds a Python dependency
  to a Python-free Go repo and needs a per-checkout install), not Husky (puts
  Node at the repo root, against the zero-dependency philosophy of ADR-0009),
  not Lefthook (a fine Go binary, but a whole tool for what a 20-line shell
  script does here).
- **Hook contents: `gofmt -l`, `go vet ./...`, and `go tool golangci-lint run
  --new-from-rev HEAD`** — the ratcheted fast tier of the quality bundle
  specced in #182. Measured total: **about 2 seconds**.
- **CI keeps everything that is a branch invariant or slow:** the test suite +
  ADR-0008 coverage gate, the e2e Playwright suite, the *full* golangci-lint
  run (ratcheted until the 51-finding baseline is burned down, per #182), and
  the scheduled CRAP gate. CI is currently missing a lint job — add one.
- **Tests stay out of the hook.** `go test ./...` is 8.4 s warm but **63 s on a
  cold build cache** (CGO/go-sqlite3), and mid-task commits legitimately have
  red tests. The coverage gate is *currently failing on master locally* — a
  hook that blocks commits on it would make every commit miserable.

## Measured on this repo (Go 1.26.2, master @ db24657, 2026-10-17)

| Command | Time | Notes |
|---|---|---|
| `gofmt -l .` | 0.05 s | clean tree |
| `go vet ./...` | 0.43 s | warm cache |
| `go test ./...` | 8.4 s | warm build cache, one package: 5.4 s |
| `go test ./...` (cold `GOCACHE`) | **63 s** | fresh clone / fresh CI runner pays this for go-sqlite3 CGO |
| `go test -coverprofile=... ./...` + `scripts/check-coverage.sh` | 9.5 s | **gate fails on master today** (see below) |
| `golangci-lint v2.15.0 run` (full) | 1.2 s | **51 issues**: 50 errcheck + 1 ineffassign; no `.golangci` config exists |
| `golangci-lint run --new-from-rev HEAD` | ~1 s | ratchet verified empirically: a probe introducing 3 new `unused` issues surfaced exactly those 3, not the 51 baseline |

Coverage gate failure on master today (ADR-0008 requires 100 % outside
`main()`): `SaveTheme 77.8 %`, `AdminPublicTheme 50 %`, `Change 97.1 %`,
`themeByName 85.7 %`. This is the clearest evidence that a per-branch gate does
not belong in a per-commit hook: hooks run on work-in-progress, CI judges the
branch.

## Why not each candidate mechanism

### Plain `.git/hooks/*` — rejected
Not version-controlled; every clone copies them by hand. This is the failure
mode of the current `scripts/pre-commit.sh` (from docs/issues/013): it exists,
but nothing installs it, and it calls `staticcheck`, which is not installed
anywhere on this machine. Dead hook.

### Versioned hook dir + `core.hooksPath` — recommended
`core.hooksPath` points git at a hooks directory that can live **inside the
repo** and be versioned (githooks(5): "By default the hooks directory is
$GIT_DIR/hooks, but that can be changed via the core.hooksPath configuration
variable"). The only friction is a one-time `git config core.hooksPath ...` per
clone.

Two empirical findings that shape the setup:

1. **A relative `core.hooksPath` silently skips hooks in linked worktrees.**
   Verified in a scratch repo: with `core.hooksPath=hooks` the hook ran in the
   main checkout but was silently skipped in a `git worktree add` sibling,
   because the relative path resolves against the working directory and the
   worktree had no `hooks/` dir. Nothing warns; commits just skip the hook.
2. **An absolute `core.hooksPath` covers every linked worktree.** Repo-local
   config is shared across worktrees, so one `git config` in the main checkout
   gives agent worktrees (created under `/tmp` per the wayfinder procedure) the
   hook for free — no per-worktree install step. A linked worktree's gitdir has
   no `hooks/` directory of its own (verified), so any install-based mechanism
   needs re-installing per worktree.

Cost: a one-line `scripts/install-hooks.sh`
(`git config core.hooksPath "$(pwd)/.githooks"`). Everything else is plain Go +
shell, matching the repo's existing `scripts/` convention.

### pre-commit framework — good, but wrong dependency tree here
Pros (real ones): versioned declarative config (`pre-commit install` is the
only per-clone step), runs hooks against **staged content by temporarily
stashing unstaged changes**, and first-class hook definitions shipped by
golangci-lint (`.pre-commit-hooks.yaml`: `id: golangci-lint` = `golangci-lint
run --new-from-rev HEAD --fix`, and `golangci-lint-full` for CI) and by
gitleaks (README documents `repo: https://github.com/gitleaks/gitleaks`).

Cons for this repo: it is a **Python** tool (`pip`/`pipx` install; neither
`pre-commit` nor `pipx` is on this machine) in a repo whose toolchain is Go
plus e2e-only Node; and `pre-commit install` writes into each checkout's
gitdir, so every linked worktree needs a re-run. The auto-stash advantage is
small here because all Go checks are module-wide (`./...`), not per-file —
staged-scoping barely changes what runs.

### Husky / lint-staged — rejected
Puts a root `package.json` in a repo that deliberately keeps npm inside
`e2e/` so the app itself stays zero-dependency (ADR-0009).

### Lefthook — viable runner-up
Single Go binary, versioned config, parallel jobs. Fine choice, but it is a
new tool to learn and install for no capability this hook needs. The plain
script is trivially readable by humans and agents alike.

## What can realistically run in a hook

Budget rule: **under ~5 s warm**, because the hook runs on *every* commit —
and this repo is agent-heavy, with many commits per task. Measured tier:

| Check | Time | In hook? |
|---|---|---|
| `gofmt -l .` | 0.05 s | yes |
| `go vet ./...` | 0.43 s | yes |
| `go tool golangci-lint run --new-from-rev HEAD` | ~1 s | yes — ratcheted; blocks only *new* issues, so the 51-finding baseline does not freeze commits |
| gitleaks (secrets) | seconds | optional — needs a gitleaks binary; otherwise CI |
| `go test ./...` | 8.4 s warm / 63 s cold | **no** — and mid-task commits legitimately have red tests |
| coverage gate (ADR-0008) | 9.5 s | **no** — per-branch invariant, currently red on master |
| full `golangci-lint run` | 1.2 s but 51 existing findings | no — would block every commit until the #182 ratchet reaches the exit line |
| e2e Playwright suite | minutes + `npm ci` + Chromium download | no — CI only |

Caveat inherited from the official golangci-lint hook description: a
changed-files lint run cannot reliably run whole-program linters (`unused`
"won't work as expected" when only modified files are linted). That is exactly
what `--new-from-rev HEAD` in the hook is for (incremental ratchet), while the
full run stays in CI / the quality bundle.

## What must remain in CI

CI is the only enforcement point that agents and humans cannot bypass
(`--no-verify` skips any hook; fresh clones have none installed until
`install-hooks.sh` runs). Keep in `test-and-coverage.yml`:

1. `go test -coverprofile` + `scripts/check-coverage.sh` (ADR-0008 gate) — already there.
2. The e2e journey suite (`npm ci`, `npx playwright install chromium`, `npx
   playwright test`) — already there.
3. **Add a golangci-lint job** — there is none today. Run the full suite at the
   pinned version (`go tool golangci-lint run`), with the ratchet/exit-line
   policy owned by #182; also `golangci-lint config verify` once a
   `.golangci.yml` exists.
4. Secret scanning (gitleaks GitHub Action) — cheap in CI, catches what hooks miss on un-hooked clones.
5. Scheduled CRAP < 6 gate — per #182, not per-commit.

## Recommended wiring

Pin golangci-lint with Go's native **tool directive** (Go 1.24+, documented in
the Go modules reference) instead of any external package manager — verified
on this machine: `go get -tool
github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0` adds `tool
github.com/golangci/golangci-lint/v2/cmd/golangci-lint` to `go.mod` (pinned via
`go.sum`), and `go tool golangci-lint --version` reports v2.15.0.

```
.githooks/pre-commit          # committed, executable: exec scripts/pre-commit.sh
scripts/install-hooks.sh      # git config core.hooksPath "$(pwd)/.githooks"  (absolute!)
scripts/pre-commit.sh         # rewritten: fast tier only (below)
go.mod                        # + tool directive for golangci-lint v2.15.0
```

Rewritten `scripts/pre-commit.sh` (replaces the dead staticcheck-plus-tests
version; the quality bundle #182 remains the full tier):

```sh
#!/bin/sh
# Fast tier of the quality bundle (#182): static, ratcheted, < 5 s.
# Slow and branch-level checks (tests, coverage gate, e2e, full lint) live in CI.
set -e

UNFORMATTED=$(gofmt -l .)
if [ -n "$UNFORMATTED" ]; then
    echo "Files need formatting (run: gofmt -w):"
    echo "$UNFORMATTED"
    exit 1
fi

go vet ./...

# Ratchet: only issues introduced relative to HEAD block the commit.
# The 51-finding baseline (errcheck/ineffassign) is being burned down via #182.
go tool golangci-lint run --new-from-rev HEAD

echo "pre-commit: gofmt, vet, and ratcheted lint passed."
```

Notes:

- Skip WIP commits with the standard escape hatch `git commit --no-verify`;
  document it for agents. Hooks are advisory; CI is authoritative.
- `--fix` is deliberately omitted (the official pre-commit hook entry uses it):
  a hook that rewrites files mid-commit surprises partial staging; report and
  let the author fix.
- Once a `.golangci.yml` exists, `golangci-lint config verify` can join the
  hook cheaply (~0 cost) — optional.

## Sources

- githooks(5) — hooks directory, `core.hooksPath`, pre-commit semantics:
  https://git-scm.com/docs/githooks
- pre-commit framework — staged-content execution via temporary stash, install
  mechanics, supported languages: https://pre-commit.com
- golangci-lint official pre-commit hook definitions (`--new-from-rev HEAD`,
  full-run CI variant, partial-run `unused` caveat):
  https://github.com/golangci/golangci-lint/blob/main/.pre-commit-hooks.yaml
- gitleaks pre-commit usage: https://github.com/gitleaks/gitleaks (README, "Installing" / pre-commit section)
- Go tool directives (`go get -tool`, `tool` lines in go.mod):
  https://go.dev/ref/mod (Go modules reference)
- Local measurements and hook/worktree experiments run on 2026-10-17 against
  master @ db24657, Go 1.26.2, golangci-lint v2.15.0 (50 errcheck + 1
  ineffassign findings confirmed); coverage gate output quoted above.
- Repo context: `.github/workflows/test-and-coverage.yml`,
  `scripts/pre-commit.sh` (dead hook from docs/issues/013),
  `scripts/check-coverage.sh` (ADR-0008), ADR-0009 (npm confined to e2e/),
  issue #182 (quality bundle, ratchet/exit-line policy).
