---
label: wayfinder:map
title: New UI and Themes
status: open
---

# New UI and Themes

This is a local-markdown tracker. Each ticket is a file in `issues/`, and the lines at the top of the file record its label (`wayfinder:<type>`), `status` (open/closed), `assignee` (the claim) and `blocked_by` (ticket files). The **frontier** is every open, unassigned ticket whose blockers are all closed.

## Destination

- **Themes built and shipped:** three Themes, each changing colours, typography, shape, motion and sound, all built on shared design tokens. Persona colours are the same in every Theme. Each Participant picks a Theme on their phone, and the admin picks the Big Screen's Theme.
- **A build-ready spec for one radically different UI idea,** covering both the Participant's phone journey and the Big Screen.

## Notes

- **Domain:** use CONTEXT.md vocabulary (Participant, Persona, Theme, Big Screen, Reveal, Interview, Match, Explore). Update CONTEXT.md as terms are resolved, using the domain-modeling skill.
- **Execution is in scope for the Themes.** Build them (test-first, using the tdd skill, at the handler and template seams) once their decisions are made. The UI idea stops at a build-ready spec.
- **Standing decisions from charting (2026-09-24):**
  - A Theme choice lives only on the Participant's device (browser storage); there is no server-side preference.
  - The Big Screen's Theme is chosen by the admin from the same Themes.
  - The first build ships three Themes.
  - UI concepts may go beyond the current stack (Go templates, HTMX, Tailwind via CDN). The chosen concept's spec records the stack decision.
  - Concepts are judged on these must-haves: fun and memorable, gets Participants to walk up to each other, and keeps anonymity until the Reveal. Tie-breakers: works on any phone in a noisy room, and buildable in a few days.
- **Specs and build tickets on GitHub:** "Spec: Themes" (#36), with its build tickets #40–#44 (the token set, the three looks and the Tailwind build step are decided, so they are `ready-for-agent`).
- **Skills:** research, prototype, grilling + domain-modeling, tdd, codebase-design (for the spec's modules).

## Decisions so far

- [How Themes can reach every page, and what browsers allow for motion and sound](issues/01-theme-delivery-and-browser-constraints.md): Themes are CSS variables under `<html data-theme>`, set before first paint and untouched by HTMX and SSE. Motion collapses under reduced motion. Sound needs one tap (or one click on the Big Screen) and must be optional.
- [Which tokens make up a Theme, and where motion and sound happen](issues/02-theme-token-set.md): 12 colour roles; Persona-coloured pages keep their background; three font families with at most one self-hosted web font; four radii, a border width and a glow; motion tokens on eight moments; synthesised sound cues, opt-in on phones; 3:1 Persona contrast; the default Theme is today's look. (Grown to 14 roles by the next decision.)
- [What the three Themes look, move and sound like](issues/03-three-theme-looks.md): Classic (today's look, the default), Neon and Paper, chosen from a five-variant prototype (branch `prototype/themes`). Two roles added: text on a Persona colour, and a Persona outline that lets light Themes pass the 3:1 rule.
- [Do Themes ship with a Tailwind build step instead of the Play CDN](issues/07-tailwind-build-step.md): no build step. Tailwind v4's browser build is vendored and pinned, and all front-end assets are self-hosted in `static/`, with no unstyled flash (ADR-0009).
- [Which radically different UI idea should we spec](issues/05-radical-ui-concepts.md): the Game Show flow. The Big Screen is the stage, phones are controllers, everyone answers rounds at once, and the Reveal is a show segment. It borrows the Swipe Deck's card animations (prototype on branch `prototype/ui-concepts`).
- [Spec the chosen UI concept](issues/06-spec-chosen-ui.md): the Game Show as a second Event Format beside the Classic flow.
  - A Host runs six Rounds (the Fixed Questions) from a host console.
  - Everyone answers 11 questions (6 Fixed and 5 personalised) in both formats.
  - LLM assessment happens in the background after the Lobby, and everyone is paired at once in the matchmaking segment.
  - The Reveal is pair by pair, and the Afterparty uses Continuous Matching.
  - A new Show module, on the existing stack.

## Not yet specified

Nothing: every decision toward the destination is made. What remains is building ("Build the Themes", GitHub #40–#44) and publishing the Game Show spec from the resolution of "Spec the chosen UI concept".

## Out of scope

- **LLM providers (Ollama, Scaleway)** are handled as a separate effort with no map, because every decision was already made at charting.
- **Server-side or per-account Theme preferences,** and light/dark following the device, were ruled out at charting.
