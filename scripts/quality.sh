#!/usr/bin/env bash
#
# quality.sh — the one quality command (ADR-0021).
#
# One entry point for the agent's self-check, the commit hook, and CI, so
# parity between them is structural, not disciplined. Four profiles:
#
#   hook     ~2 s, advisory: gofmt, go vet, golangci-lint --new-from-rev HEAD
#   check    blocking (default): the full gate set — the PR gate
#   e2e      the Playwright journey suite in e2e/
#   nightly  non-blocking (~23 min): gremlins on the ADR-0024
#            decision-logic scope, the survivor issue queue, a CRAP trend
#
# Ratchets (retrofit only, ADR-0021): .quality/lint-baseline and
# .quality/crap-baseline are plain count files with a header comment.
# Gate: current <= baseline. Retrofit commits decrement them; at zero the
# file is deleted and the bare strict gate remains.
#
# Exit codes: 0 green, 1 a gate failed, 2 usage error.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

LINT_BASELINE=".quality/lint-baseline"
CRAP_BASELINE=".quality/crap-baseline"

fail() {
    printf 'quality: FAIL: %s\n' "$1" >&2
    exit 1
}

warn() {
    printf 'quality: warning: %s\n' "$1" >&2
}

usage() {
    cat <<'EOF'
Usage: scripts/quality.sh <profile>

Profiles:
  hook      advisory, ~2 s: gofmt -l, go vet ./..., golangci-lint run
            --new-from-rev HEAD
  check     blocking (default): gofmt, go vet, golangci-lint under the
            lint ratchet, go test -shuffle=on -race -coverprofile (root
            package only), the blocking coverage gate (ADR-0008), the
            CRAP gate under its ratchet (ADR-0023), govulncheck
  e2e       the Playwright journey suite in e2e/ (same invocation as CI)
  nightly   non-blocking (~23 min): gremlins mutation run scoped to the
            22-file decision-logic union (ADR-0024), the survivor issue
            queue (scripts/mutation-report.sh), a CRAP trend report

Exit codes:
  0  green
  1  a gate failed
  2  usage error
EOF
}

# The integer on the first bare-number line; comment lines are ignored.
# Empty output means no baseline file: the gate is bare and strict.
baseline() {
    [ -f "$1" ] || return 0
    awk '/^[0-9]+$/ { print; exit }' "$1"
}

# gofmt -l . — any output is a failure (ADR-0021 step 1).
check_gofmt() {
    local unformatted
    unformatted="$(gofmt -l .)"
    if [ -n "$unformatted" ]; then
        fail "gofmt: these files need formatting (run gofmt -w):
$unformatted"
    fi
    echo "gofmt: clean"
}

check_vet() {
    go vet ./... || fail "go vet: findings (see above)"
    echo "go vet: clean"
}

# golangci-lint full config under the count ratchet. One finding per
# "file:line:col:" line of the text output.
check_lint() {
    local out count max
    if out="$(go tool golangci-lint run ./... 2>&1)"; then
        count=0
    else
        count="$(printf '%s\n' "$out" | grep -cE '^[[:alnum:]_./-]+\.go:[0-9]+:[0-9]+:' || true)"
    fi
    max="$(baseline "$LINT_BASELINE")"
    if [ -z "$max" ]; then
        # Bare strict gate: the exit line, no ratchet file.
        if [ "$count" -gt 0 ]; then
            printf '%s\n' "$out" >&2
            fail "lint: $count findings, strict gate (no baseline file)"
        fi
    else
        if [ "$count" -gt "$max" ]; then
            printf '%s\n' "$out" >&2
            fail "lint: $count findings > lint-baseline $max — fix findings or decrement the ratchet"
        fi
    fi
    echo "lint: $count findings, ${max:+within ratchet $max, }clean under the gate"
}

# The CRAP gate (ADR-0023) fed the check-profile coverage profile.
check_crap() {
    local current max
    current="$(go run ./scripts/crap -failing -profile coverage.out)"
    max="$(baseline "$CRAP_BASELINE")"
    if [ -n "$max" ]; then
        if [ "$current" -le "$max" ]; then
            echo "CRAP gate: $current failing functions <= ratchet $max"
        else
            go run ./scripts/crap -profile coverage.out >&2 || true
            fail "CRAP gate: $current failing functions > crap-baseline $max"
        fi
    else
        # Bare strict gate: no baseline file, -gate mode must pass.
        go run ./scripts/crap -gate -profile coverage.out \
            || fail "CRAP gate: strict mode (no baseline file), see report above"
    fi
}

# The 22-file decision-logic union (ADR-0024): the deciders plus the flow
# deciders. The nightly gremlins run is scoped to exactly this set.
SCOPE=(
    matcher.go mission.go persona.go interview.go questions.go
    meetings.go meetcode.go llm.go structured.go github.go
    showtell.go showtell_questions.go closing.go screen.go eventstate.go
    participantstore.go onboarding.go matchmaking.go relationships.go
    recap.go evening.go steps.go
)

scope_has() {
    local wanted="$1" name
    for name in "${SCOPE[@]}"; do
        if [ "$name" = "$wanted" ]; then return 0; fi
    done
    return 1
}

# gremlins scopes by exclusion only: exclude every .go file on disk that
# is not in the scoped union. Built at run time so files added later stay
# out of scope until the union is extended. The regexp anchors each
# basename between a path separator and the .go suffix, so it matches
# gremlins' filepath form however it is prefixed.
mutation_exclude() {
    local p base name excluded=""
    while IFS= read -r p; do
        base="${p##*/}"
        if scope_has "$base"; then continue; fi
        name="${base%.go}"
        excluded="${excluded:+$excluded|}$name"
    done < <(find . -name '*.go' -not -path './.git/*' | LC_ALL=C sort)
    [ -n "$excluded" ] || fail "mutation scope: every .go file is scoped?"
    printf '(^|/)(%s)\\.go$' "$excluded"
}

profile_hook() {
    check_gofmt
    check_vet
    go tool golangci-lint run --new-from-rev HEAD \
        || fail "hook profile: new findings vs HEAD (see above)"
    echo "hook: clean"
}

profile_check() {
    check_gofmt
    check_vet
    check_lint
    echo "tests: go test -shuffle=on -race -coverprofile=coverage.out . (root package only)"
    go test -shuffle=on -race -coverprofile=coverage.out . \
        || fail "tests: red (see above)"
    bash scripts/check-coverage.sh coverage.out \
        || fail "coverage gate (ADR-0008): blocking, no ratchet"
    check_crap
    go tool govulncheck ./... \
        || fail "govulncheck: reachable vulnerabilities (see above)"
    echo "check: all gates green"
}

profile_e2e() {
    command -v npm >/dev/null 2>&1 \
        || fail "e2e: npm not found — the Playwright suite needs Node (e2e/)"
    ( cd e2e && npm ci ) || fail "e2e: npm ci failed"
    ( cd e2e && npx playwright install --with-deps chromium ) \
        || fail "e2e: Chromium install failed"
    ( cd e2e && npx playwright test ) || fail "e2e: Playwright suite red (see above)"
    echo "e2e: journeys green"
}

profile_nightly() {
    echo "nightly: recomputing the coverage profile"
    go test -race -coverprofile=coverage.out . \
        || fail "nightly: test suite red"
    local exclude
    exclude="$(mutation_exclude)"
    echo "nightly: gremlins on the ADR-0024 decision-logic scope (non-blocking, ~23 min)"
    if ! go tool gremlins unleash --output mutation.json --exclude-files "$exclude"; then
        warn "gremlins exited non-zero — survivors or tool error; the issue queue is the ledger"
    fi
    [ -f mutation.json ] || fail "nightly: gremlins produced no mutation.json"
    bash scripts/mutation-report.sh mutation.json \
        || fail "nightly: survivor workflow failed (gh issue queue)"
    echo "nightly: CRAP trend report (non-blocking)"
    go run ./scripts/crap -profile coverage.out \
        || warn "CRAP trend report failed"
    echo "nightly: done"
}

if [ $# -gt 1 ]; then
    usage >&2
    exit 2
fi

case "${1:-check}" in
    hook) profile_hook ;;
    check) profile_check ;;
    e2e) profile_e2e ;;
    nightly) profile_nightly ;;
    -h | --help) usage ;;
    *)
        usage >&2
        exit 2
        ;;
esac
