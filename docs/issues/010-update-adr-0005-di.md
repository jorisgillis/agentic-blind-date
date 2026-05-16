# Issue: Update ADR 0005 - Add AgentPipeline injection into Handler

## Description
Update ADR 0005 (Dependency Injection Strategy) to reflect that AgentPipeline is now injected into Handler.

## Details
Update the composition root example from:
```go
matcher := NewMatcher(github, mistral)
agents := NewAgentPipeline(db, github, mistral, matcher)
h := NewHandler(db, github, mistral)
```

To:
```go
matcher := NewMatcher(github, mistral)
agents := NewAgentPipeline(db, github, mistral, matcher)
h := NewHandler(db, github, mistral, agents)
```

## Acceptance Criteria
- [ ] ADR 0005 updated with new composition root example
- [ ] Handler constructor signature updated in documentation
- [ ] All examples are consistent

## Priority
Medium

## Labels
`enhancement, documentation, architecture`

## Type
Documentation
