---
label: wayfinder:prototype
title: Which radically different UI idea should we spec
status: closed
assignee: claude
blocked_by: []
---

## Question

Sketch 3–4 very different UI concepts as cheap throwaway prototypes. Each concept must span both the Participant's phone journey (landing, Interview, wait, Match) and the Big Screen. Judge them against the map's criteria:
- **Must-haves:** fun and memorable, gets Participants to walk up to each other, keeps anonymity until the Reveal.
- **Tie-breakers:** works on any phone in a noisy room, buildable in a few days.

Anything goes technically. The human picks the one to spec.

## Resolution

Decided with the human on 2026-09-24, after a throwaway prototype of four structurally different concepts. The prototype is on branch `prototype/ui-concepts`: open `prototype/ui-concepts.html` and step through each concept.

- **Chosen: the Game Show flow** (concept D). The Big Screen is the stage and hosts a live show. Phones are controllers: a buzzer, then answer pads. Everyone answers the same Interview question at the same moment, and the room sees the split instantly. The Reveal is a show segment: pairs light up on the Big Screen, the phone switches to the Match's colour, and matched pairs stand up.
- **Borrowed from the Swipe Deck** (concept A): its card animations, meaning swipeable answer cards on the phone during rounds and the full-screen "It's a Match!" pop at the Reveal. Exactly how they apply is settled in the spec.
- **Not chosen:** the Matchmaker chat (B) and the Split-code quest (C). In the prototype C scored best on "walk up to each other" and B fitted the agentic theme best; the human preferred D's room moment.
