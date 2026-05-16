# Issue: Create ADR 0007 - Separation of Matching Logic into Matcher Module

## Description
Create ADR documenting the decision to extract matching logic into a separate Matcher module.

## Details
The ADR should cover:
- The matching functions extracted: GenerateMatch, PairScore, Top5Candidates, CollectCandidatePairs, GreedyMatch
- Benefits: better separation of concerns, easier testing, clearer dependencies
- Trade-offs considered
- How it integrates with the rest of the system

## Acceptance Criteria
- [ ] ADR 0007 created in docs/adr/0007-separation-of-matching-logic-into-matcher-module.md
- [ ] Documents the extraction decision and rationale
- [ ] Follows ADR format
- [ ] Linked from README or docs/adr/README.md

## Priority
Medium

## Labels
`enhancement, documentation, architecture`

## Type
Documentation
