#!/usr/bin/env bash
#
# mutation-report.sh — the ADR-0024 mutation survivor workflow.
#
# Reconciles a gremlins mutation report (gremlins unleash --output
# mutation.json) with the GitHub issue tracker:
#
#   1. every un-waived LIVED (surviving) mutant gets one issue, with title
#      identity "MUT: <file>:<line>:<col> <TYPE>", deduplicated by an exact
#      open-issue title search before creating;
#   2. every tracked issue whose mutant no longer survives is commented
#      and closed — open AND closed issues are searched, closed ones are
#      left closed (nothing is ever reopened);
#   3. mutants listed in the waivers file are skipped (ADR-0024 escape
#      hatch).
#
# Waived mutants with a still-open issue count as no longer unexplained:
# their issue is closed with a note.
#
# The issue queue IS the mutation ledger — there is no count file
# (ADR-0024). This script is the nightly survivor workflow; it is never
# a PR gate.
#
# Usage:
#   scripts/mutation-report.sh [--dry-run] [--waivers FILE] [mutation.json]
#
# --dry-run prints every gh write operation instead of performing it; the
# read operations (issue list) still run so the preview is accurate.

set -euo pipefail

PROG="${0##*/}"

usage() {
    cat <<EOF
Usage: $PROG [--dry-run] [--waivers FILE] [mutation.json]

Reconciles the gremlins mutation report with the GitHub issue tracker
(ADR-0024 survivor workflow).

Options:
  --dry-run        print the gh write operations instead of performing them
  --waivers FILE   waivers file (default: .quality/mutation-waivers)
  -h, --help       show this help

The default input is ./mutation.json, in the gremlins unleash --output
JSON format. Only LIVED mutants are tracked; KILLED and TIMED OUT count
as caught, NOT COVERED is the coverage gate's business.
EOF
}

DRY_RUN=0
MUTATION_FILE="mutation.json"
WAIVERS_FILE=".quality/mutation-waivers"

while [ "$#" -gt 0 ]; do
    case "$1" in
        --dry-run) DRY_RUN=1 ;;
        --waivers)
            [ "$#" -ge 2 ] || { echo "$PROG: --waivers needs a value" >&2; exit 2; }
            WAVERS_FILE="$2"
            shift
            ;;
        -h | --help)
            usage
            exit 0
            ;;
        -*)
            echo "$PROG: unknown flag: $1" >&2
            usage >&2
            exit 2
            ;;
        *)
            MUTATION_FILE="$1"
            ;;
    esac
    shift
done

[ -f "$MUTATION_FILE" ] || { echo "$PROG: mutation report not found: $MUTATION_FILE" >&2; exit 1; }
[ -f "$WAVERS_FILE" ] || echo "$PROG: note: no waivers file at $WAVERS_FILE, treating as empty" >&2

# --- helpers ----------------------------------------------------------------

waiver_lines() {
    if [ -f "$WAVERS_FILE" ]; then
        grep -v '^[[:space:]]*#' "$WAVERS_FILE" || true
    fi
}

WAIVERS="$(waiver_lines)"

# is_waived <file:line:col TYPE> — the waivers file format is
# "file:line:col TYPE — justification", so the identity is a prefix match
# on non-comment lines.
is_waived() {
    printf '%s\n' "$WAIVERS" | grep -qF -- "$1"
}

if [ "$DRY_RUN" = 1 ]; then
    gh_write() { printf '[dry-run] gh %s\n' "$*"; }
else
    gh_write() { gh "$@"; }
fi

# --- extract the survivors --------------------------------------------------
# The repo is zero-dependency: the JSON report is parsed with an embedded
# stdlib-only Go program instead of jq.

WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

cat >"$WORKDIR/main.go" <<'GOEOF'
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type mutation struct {
	Type   string `json:"type"`
	Status string `json:"status"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

type outFile struct {
	Filename  string     `json:"file_name"`
	Mutations []mutation `json:"mutations"`
}

type report struct {
	Files []outFile `json:"files"`
}

func main() {
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "reading mutation report: %v\n", err)
		os.Exit(1)
	}
	var r report
	if err := json.Unmarshal(data, &r); err != nil {
		fmt.Fprintf(os.Stderr, "parsing mutation report: %v\n", err)
		os.Exit(1)
	}
	for _, f := range r.Files {
		for _, m := range f.Mutations {
			if m.Status != "LIVED" {
				continue
			}
			fmt.Printf("%s\t%d\t%d\t%s\n", f.Filename, m.Line, m.Column, m.Type)
		}
	}
}
GOEOF

SURVIVORS="$WORKDIR/survivors.tsv"
go run "$WORKDIR/main.go" "$MUTATION_FILE" | sort -u >"$SURVIVORS"

SURVIVOR_IDS="$(
    awk -F'\t' '{printf "%s:%s:%s %s\n", $1, $2, $3, $4}' "$SURVIVORS"
)"
SURVIVOR_TOTAL=0
if [ -n "$SURVIVOR_IDS" ]; then
    SURVIVOR_TOTAL=$(printf '%s\n' "$SURVIVOR_IDS" | wc -l)
fi

# survives_unexplained <id> — true when the mutant is LIVED in this run
# and carries no waiver.
survives_unexplained() {
    printf '%s\n' "$SURVIVOR_IDS" | grep -qxF -- "$1" && ! is_waived "$1"
}

OPENED=0
CLOSED=0
SKIPPED_TRACKED=0
SKIPPED_WAIVED=0
NOTED_STALE=0

# --- tracked issues: close the ones whose mutant no longer survives ----------

# The search is scoped to the title prefix; the exact-match check below
# filters out search noise. Both open and closed issues are listed.
TRACKED="$(gh issue list --state all --limit 1000 \
    --search 'MUT: in:title' \
    --json number,title,state \
    --jq '.[] | [.number, .state, .title] | @tsv' || true)"

while IFS=$'\t' read -r num state title; do
    [ -n "$title" ] || continue
    case "$title" in
    "MUT: "*) ;;
    *) continue ;;
    esac
    id="${title#MUT: }"

    if survives_unexplained "$id"; then
        if [ "$state" = "CLOSED" ]; then
            echo "$PROG: note: closed issue #$num matches current survivor $id; not reopening (stale)"
            NOTED_STALE=$((NOTED_STALE + 1))
        fi
        continue
    fi

    if [ "$state" = "OPEN" ]; then
        if printf '%s\n' "$SURVIVOR_IDS" | grep -qxF -- "$id" && is_waived "$id"; then
            comment="The mutant $id is now waived in $WAIVERS_FILE (ADR-0024 escape hatch). Closing."
        else
            comment="The mutant $id no longer survives the latest mutation run (killed or timed out). Closing — ADR-0024 survivor workflow."
        fi
        gh_write issue close "$num" --comment "$comment"
        echo "$PROG: closed #$num ($id)"
        CLOSED=$((CLOSED + 1))
    fi
    # Already closed and no longer surviving: leave it closed, never reopen.
done <<<"$TRACKED"

# --- survivors: open one issue per un-waived mutant --------------------------

while IFS=$'\t' read -r file line col type; do
    [ -n "$file" ] || continue
    id="$file:$line:$col $type"
    title="MUT: $id"

    if is_waived "$id"; then
        echo "$PROG: skipped waived survivor $id"
        SKIPPED_WAIVED=$((SKIPPED_WAIVED + 1))
        continue
    fi

    # Dedupe: an exact-title search over open issues first. The final
    # check is exact because GitHub search munges punctuation in titles.
    if gh issue list --state open --limit 1000 \
        --search "\"$title\" in:title" \
        --json number,title \
        --jq '.[].title' | grep -qxF -- "$title"; then
        echo "$PROG: already tracked: $title"
        SKIPPED_TRACKED=$((SKIPPED_TRACKED + 1))
        continue
    fi

    body="A mutation testing survivor is live: the test suite passes with this mutant applied (ADR-0024).

- **File**: \`$file\`
- **Position**: line $line, column $col
- **Mutant type**: $type
- **Status**: LIVED
- **Waived**: no

Reproduce:

\`\`\`
go tool gremlins unleash --timeout-coefficient 60 --output mutation.json
\`\`\`

Then inspect the \`$file\` mutations in \`mutation.json\`: the mutant survives when its status stays LIVED.

Title identity: \`$title\`"

    gh_write issue create --title "$title" --body "$body"
    echo "$PROG: opened issue for $title"
    OPENED=$((OPENED + 1))
done <"$SURVIVORS"

# --- summary -----------------------------------------------------------------

echo "$PROG: survivors: $SURVIVOR_TOTAL, waived: $SKIPPED_WAIVED, opened: $OPENED, already tracked: $SKIPPED_TRACKED, closed: $CLOSED, stale-noted: $NOTED_STALE"
if [ "$DRY_RUN" = 1 ]; then
    echo "$PROG: dry run, no tracker mutations were performed"
fi
