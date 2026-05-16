# Issue: Update CONTEXT.md - Add Non-GitHub User section

## Description
Add a dedicated section for Non-GitHub Users in CONTEXT.md after the GitHub Profile section.

## Details
The section should document:
- Handle format: `"no-github-{uuid-prefix}"`
- Skip GitHub profile fetching
- Pipeline starts at `interviewing` (not `fetching_github`)
- Answer ExtraQuestions instead of FixedQuestions + CustomQuestions
- ExtraAnswers are used for their profile data

## Acceptance Criteria
- [ ] CONTEXT.md has a new "Non-GitHub User" section after GitHub Profile
- [ ] All characteristics of non-GitHub users are documented
- [ ] Language is consistent with existing CONTEXT.md style

## Priority
High

## Labels
`enhancement, documentation, domain`

## Type
Documentation
