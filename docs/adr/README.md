# Architecture Decision Records

This directory contains Architecture Decision Records (ADRs) for the Agentic Blind Date project.

## What is an ADR?

An Architecture Decision Record is a document that captures an important architectural decision made in the project, along with its context and consequences.

## Format

Each ADR follows this structure:
- **Status**: Accepted, Superseded, Deprecated, etc.
- **Context**: The problem being addressed
- **Decision**: The chosen solution
- **Consequences**: Good and bad outcomes of the decision
- **Alternatives Considered**: Other options that were evaluated

## List of ADRs

| Number | Title | Status | Date |
|--------|-------|--------|------|
| [0001](0001-matching-algorithm-design.md) | Three-Phase Matching Algorithm | Accepted | 2026-05-14 |
| [0002](0002-llm-caching-strategy.md) | LLM Response Caching Strategy | Accepted | 2026-05-14 |
| [0003](0003-consistent-interview-experience.md) | Consistent Interview Experience for All Participants | Accepted | 2026-05-14 |
| [0004](0004-big-screen-top-connections.md) | Big Screen Shows Top-3 Connections | Accepted | 2026-05-14 |

## When to Create an ADR

Create an ADR when a decision:
1. **Is hard to reverse** - The cost of changing your mind later is meaningful
2. **Is surprising without context** - A future reader will wonder "why did they do it this way?"
3. **Is the result of a real trade-off** - There were genuine alternatives and you picked one for specific reasons

## How to Add a New ADR

1. Create a new file with the next sequential number: `NNNN-description.md`
2. Follow the ADR template
3. Add the ADR to the table above
4. Submit a PR for review

## Resources

- [ADR GitHub Template](https://github.com/joel-costigliola/adr-templates)
- [ADR Original](https://adr.github.io/)
