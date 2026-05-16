# Issue: Add SelectionMode enum to Question struct

## Description
Add a `SelectionMode` enum to the Question struct to make multi-select vs single-select more explicit.

## Details
- Add type: `type SelectionMode int`
- Add constants: `SingleSelect SelectionMode = iota` and `MultiSelect`
- Keep existing `MaxSelections` field
- Update question definitions to use the enum

## Acceptance Criteria
- [ ] SelectionMode enum defined
- [ ] Question struct updated
- [ ] All questions use appropriate SelectionMode
- [ ] Code compiles
- [ ] Tests pass

## Priority
Medium

## Labels
`enhancement, backend`

## Type
Feature
