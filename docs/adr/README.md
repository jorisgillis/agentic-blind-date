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
| [0005](0005-dependency-injection.md) | Dependency Injection Strategy | Accepted | 2026-05-15 |
| [0006](0006-fallback-questions-for-llm-failures.md) | Fallback Questions for LLM Failures | Accepted | 2026-05-16 |
| [0007](0007-separation-of-matching-logic-into-matcher-module.md) | Separation of Matching Logic into Matcher Module | Accepted | 2026-05-16 |
| [0008](0008-quality-assurance-approach.md) | Quality Assurance Approach | Accepted | 2026-05-16 |
| [0009](0009-self-hosted-front-end-assets.md) | Self-Hosted Front-End Assets and In-Browser Tailwind v4 | Accepted | 2026-09-24 |
| [0010](0010-event-phase-machine.md) | Event Phase Machine | Accepted | 2026-10-10 |
| [0011](0011-meeting-handshake.md) | Meeting Confirmation Handshake | Accepted | 2026-10-10 |
| [0012](0012-reveal-ceremony-and-locked-dossier.md) | The Reveal Ceremony and the Locked Dossier | Accepted | 2026-10-10 |
| [0013](0013-contact-exchange-and-personal-recap.md) | Contact Exchange and the Personal Recap | Accepted | 2026-10-10 |
| [0014](0014-showtell-signup-host-queue-presenter-mode.md) | Show & Tell — Sign-up, Host Queue, Presenter Mode with Identity Reveal | Accepted | 2026-10-10 |
| [0015](0015-showtell-audience-questions.md) | Show & Tell — Audience Questions Through Personas | Accepted | 2026-10-10 |
| [0016](0016-closing-ceremony-public-recap-host-export.md) | Closing Ceremony, Public Recap and the Host Export Pack | Accepted | 2026-10-10 |
| [0017](0017-structured-leveled-server-logging.md) | Structured, Leveled Server Logging | Accepted | 2026-10-11 |
| [0018](0018-server-side-qr-generation.md) | Server-Side QR Generation | Accepted | 2026-10-11 |

| [0019](0019-participant-page-layout.md) | Participant Page Layout — Phase-Progressive Disclosure with a Persistent Phase Header | Accepted | 2026-10-11 |

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
