#!/bin/sh

# Pre-commit hook for Agentic Blind Date
# Runs formatting, linting, and tests before allowing commits

set -e

echo "Running pre-commit checks..."

# 1. Check formatting with gofmt
echo "Checking code formatting..."
UNFORMATTED=$(gofmt -l .)
if [ -n "$UNFORMATTED" ]; then
    echo "The following files need formatting:"
    echo "$UNFORMATTED"
    echo "Run 'gofmt -w' to format them."
    exit 1
fi

# 2. Run staticcheck for linting
echo "Running staticcheck..."
go vet ./...
staticcheck ./...

# 3. Run all tests
echo "Running tests..."
go test ./...

echo "All pre-commit checks passed!"
