# 0019: Participant Page Layout — Phase-Progressive Disclosure with a Persistent Phase Header

## Status

Accepted

## Context

The participant's personal pages have grown into a long single column of stacked cards: persona card, mission card, meet section, match reveal, dossier, show & tell, settings, share-profile QR, recap. During a live event this is a lot to scroll through, and the one thing a participant needs *right now* (the mission card during the social, the meet code for the handshake) sits at an arbitrary scroll position. The host, testing the live event, asked whether tabs, slides, a deck, or something else would be better — and whether the page can give a sense of which phase the evening is in (#133).

Two architectural facts constrain any answer:

- **One SSE stream per page.** Each personal page holds one `EventSource`, driven by the DB change feed (no polling). The server pushes named events (`refresh`, `redirect`); the client swaps fragments at stable DOM anchors (`handlers.go`, `wait.html`). The match page currently has no stream at all — it only changes by full navigation.
- **The phase machine already exists as a server-side fact.** #113 introduced onboarding → social → showtell → closed, driven from the host console and announced on the change feed. "What should be on screen" is therefore a function of (phase, participant state) that only the server can evaluate.

The research in #133 compared four layout options on glanceability during a live event, scroll vs. discoverability, phone ergonomics, SSE fragment-refresh compatibility, accessibility, and implementation cost.

## Decision

**Phase-progressive disclosure, with a persistent phase header, and a single "More" drawer for evergreen content.** No tabs, no slides, no deck.

1. **Phase header (persistent).** A server-rendered `fragment-phase-header` at the top of every personal page: a progress strip of the evening's steps (onboarding → social → showtell → closed) plus a one-line "Tonight: …" summary, with the participant's own status woven in — onboarding shows interview progress ("X of N answered"), social shows mission progress, showtell shows the participant's queue number (decided in #133: the queue number belongs on the persona screen; the queue itself is on the Big Screen), closed points at the recap. Late joiners need no special handling: the header renders from the phase machine's *current* state, so prior steps simply appear as done.
2. **Phase content (the main region).** Below the header, the page shows only what the current phase calls for. During the social, that is — in order — the **mission card first** (decided in #133: the mission card is the primary thing during the social), then the meet section with its QR, then the match reveal and dossier. Show & tell tooling appears when the showtell phase is active; the recap and export belong to closed.
3. **"More" drawer (evergreen content).** Settings, share-profile QR, GitHub summary, the recap link, and the **dossier** live behind a single bottom "More" toggle — server-rendered, always reachable, never competing with the phase content above. The dossier is out of the phase content (it never leads) but stays reachable at any time from the drawer, so a participant can peek mid-phase.
4. **Push mechanism.** The header and the phase content are ordinary anchored fragments refreshed by the page's existing SSE stream, exactly like `#wait-status` today. A phase transition additionally emits a distinct `phase` event so the client can swap the main region and animate the header in one beat. No new stream, no polling, no client-side state.

## Consequences

- **Glanceability:** the mission card and meet QR are above the fold during the social by construction, not by scroll position. The phase header answers "where are we tonight?" on every page.
- **One source of truth:** the page is a pure function of (phase, participant state); the server decides what is on screen, the same discipline the Big Screen follows.
- **Lowest mechanical risk:** no per-slide state, no carousel ARIA, no tab-routing of fragment events. The existing SSE → fragment-swap model survives unchanged.
- **Phase tooling is hidden by design:** show & tell tooling is invisible outside its phase. The dossier is the exception — it stays reachable from the "More" drawer in any phase (decided in #133), it just never leads.
- **Tabs may return locally:** if a single phase's content ever grows too long (the social is the candidate), a tab row *within* that phase is the sanctioned escape hatch — not tabs as the page's organizing principle.

## Alternatives Considered

- **(a) Status quo — one long column (reordered per #130).** Zero implementation cost and fully discoverable, but the live-event pain remains: the needed-now card sits at an arbitrary scroll depth and there is no phase sense. Chosen as the fallback ordering *within* the phase regions, rejected as the page's structure.
- **(b) Tabs (Mission / Meet / Match / More).** Good ergonomics and badge-able counts, but content a participant doesn't know exists is content they won't find while standing in a room; and fragment refreshes must swap hidden tabs and update badges without disturbing the visible one — extra client state for no glanceability win over (d).
- **(c) Slides / deck.** The best "one thing at a time" feel, but the worst fit for the architecture: per-slide state, deep-linking, and the meet handshake interrupting mid-flow; carousels are also the hardest pattern to make accessible. Rejected on cost and risk.
- **(d) + tab row hybrid.** Kept in reserve as the escape hatch above, not the starting point.
