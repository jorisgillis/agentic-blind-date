# Issue: Create ADR 0006 - Fallback Questions for LLM Failures

## Description
Create ADR documenting the LLM fallback behavior, including both custom questions and persona generation fallbacks.

## Details
The ADR should cover:
- When LLM custom question generation fails for GitHub users, fall back to ExtraQuestions
- When LLM persona generation fails, fall back to generateFallbackPersonaFromCompleteProfile
- Rationale for graceful degradation
- Impact on user experience

## Acceptance Criteria
- [ ] ADR 0006 created in docs/adr/0006-fallback-questions-for-llm-failures.md
- [ ] Documents both question and persona generation fallbacks
- [ ] Follows ADR format (Status, Context, Decision, Consequences, Alternatives)
- [ ] Linked from README or docs/adr/README.md

## Priority
High

## Labels
`enhancement, documentation, architecture`

## Type
Documentation
