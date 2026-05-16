# Issue: Implement creating_persona pipeline step in RunFinalSetup

## Description
Modify `RunFinalSetup` in agents.go to set `PipelineStep = "creating_persona"` at the start, then update to `"ready"` after persona generation completes.

## Details
Currently, `RunFinalSetup` sets the pipeline step directly to `"ready"`. We need to:
1. Set step to `"creating_persona"` at the beginning of `RunFinalSetup`
2. Keep the persona generation logic as-is
3. Set step to `"ready"` after persona generation and interest computation complete

## Acceptance Criteria
- [ ] `RunFinalSetup` sets `PipelineStep` to `"creating_persona"` at start
- [ ] `PipelineStep` is updated to `"ready"` after all setup completes
- [ ] Existing functionality is preserved
- [ ] Code compiles and tests pass

## Priority
High

## Labels
`enhancement, backend`

## Type
Feature
