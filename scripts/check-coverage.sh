#!/bin/sh
# Coverage gate (ADR-0008): every function except main() must be fully covered.
# main() is the composition root and is not unit-tested.
#
# Usage: scripts/check-coverage.sh [coverage profile, default coverage.out]
set -e

profile=${1:-coverage.out}
if [ ! -f "$profile" ]; then
    # Same invocation as quality.sh check (ADR-0021): a standalone gate
    # run must not silently score a weaker test command.
    go test -shuffle=on -race -coverprofile="$profile" .
fi
missing=$(go tool cover -func="$profile" | grep -v '^total:' | awk '$2 != "main" && $3 != "100.0%"')

if [ -n "$missing" ]; then
    echo "Coverage gate failed: ADR-0008 requires 100% outside main(). Not fully covered:"
    echo "$missing"
    exit 1
fi
echo "Coverage gate passed: every function outside main() is fully covered."
