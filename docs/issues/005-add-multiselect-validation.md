# Issue: Add backend validation for multi-select answers

## Description
Add validation in `SubmitAnswer` handler to ensure multi-select question answers don't exceed `MaxSelections`.

## Details
- Parse the JSON array answer
- Check that array length ≤ MaxSelections for the question
- Apply to questions where MaxSelections > 1

## Acceptance Criteria
- [ ] Validation logic added to SubmitAnswer
- [ ] Handles JSON parsing errors gracefully
- [ ] Works for both single-select and multi-select questions

## Priority
High

## Labels
`enhancement, backend, validation`

## Type
Feature
