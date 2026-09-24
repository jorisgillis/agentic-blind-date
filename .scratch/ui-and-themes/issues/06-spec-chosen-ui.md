---
label: wayfinder:grilling
title: Spec the chosen UI concept
status: open
assignee: claude
blocked_by: [05-radical-ui-concepts.md, 02-theme-token-set.md]
---

## Question

Turn the **Game Show** concept, with the Swipe Deck's card animations, into a build-ready spec. See the prototype on branch `prototype/ui-concepts`, concept D, and concept A for the animations. Questions it must answer:

- **Run of show:** who drives it (an admin as host, a timer, or both), and which segments exist (join, rounds, the matchmaking segment, the Reveal)?
- **Matching:** does the show replace Continuous Matching, pairing everyone in the matchmaking segment, or do they coexist? What happens to Participants who arrive late, after the rounds have started?
- **Shared rounds versus the Interview:**
  - Everyone answers the same question at once. What happens to Custom Questions (per GitHub profile) and Extra Questions (Non-GitHub Users)?
  - Which questions become rounds?
  - Is the rest still answered individually?
- **Phone as controller:**
  - the screens and states of the controller
  - where the swipe cards apply
  - how the phone shows "stand up, your Match is…" and switches to the Match's colour
- **Big Screen segments:** what the stage shows in each segment (live answer splits, the matchmaking drumroll, pairs lighting up), and how anonymity holds until the Reveal.
- **Real-time push:** is the current change feed with SSE enough to keep phones and the Big Screen in step within a round, or is something else needed?
- **Adoption:** does it replace the current UI, or run alongside it behind an event setting?
- **Themes:** how it uses the Theme tokens and the eight moments, which likely gain round-specific moments.
- **Domain:** which new terms go into CONTEXT.md (for example Round, Host, Show segment).

Use the codebase-design skill for the modules involved (Interview, Matchmaking, Big Screen, Event State).
