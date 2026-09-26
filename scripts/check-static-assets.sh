#!/bin/sh
# Vendored asset gate (ADR-0009): every self-hosted front-end asset in
# static/ must match its pinned checksum, so a change can't go unnoticed.
set -e

cd "$(dirname "$0")/.."/static

if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 -c CHECKSUMS.sha256
elif command -v sha256sum >/dev/null 2>&1; then
    sha256sum -c CHECKSUMS.sha256
else
    echo "Neither shasum nor sha256sum is available" >&2
    exit 1
fi
