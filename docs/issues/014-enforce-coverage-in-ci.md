# Issue: Enforce 100% coverage in CI/CD

## Description
Configure CI/CD to fail the build if test coverage is below 100%.

## Details
- Add coverage check to GitHub Actions workflow
- Use `go test -coverprofile` and check coverage percentage
- Fail build if coverage < 100%

## Acceptance Criteria
- [ ] CI workflow updated with coverage check
- [ ] Build fails if coverage < 100%
- [ ] Coverage report is visible in CI output

## Priority
Medium

## Labels
`enhancement, ci, testing`

## Type
Tooling
