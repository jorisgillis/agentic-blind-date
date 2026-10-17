#!/usr/bin/env bash
#
# install-hooks.sh — point git at the committed hooks (ADR-0021).
#
# core.hooksPath must be the ABSOLUTE path of this checkout's .githooks:
# a relative path silently skips hooks in linked worktrees. Derived from
# this script's own location at run time — never hardcoded — so every
# worktree installs its own absolute path.
set -euo pipefail

hooks_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.githooks" && pwd)"
git config core.hooksPath "$hooks_dir"
echo "core.hooksPath = $hooks_dir"
echo "pre-commit hook installed (advisory; runs scripts/quality.sh hook)"
