# Issue: Add pre-commit hook for formatting, linting, and tests

## Description
Add a pre-commit hook that runs gofmt, staticcheck, and tests before allowing commits.

## Details
- Use git hooks or a tool like pre-commit
- Run: gofmt (formatting check)
- Run: staticcheck (linting)
- Run: go test ./... (all tests)
- Fail commit if any step fails

## Acceptance Criteria
- [ ] Pre-commit hook configured
- [ ] Runs gofmt, staticcheck, and tests
- [ ] Hook fails commit if any check fails
- [ ] Documentation added for contributors

## Priority
Medium

## Labels
`enhancement, tooling, testing`

## Type
Tooling
