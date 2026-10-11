# 0012: The Reveal Ceremony and the Locked Dossier

## Status
Accepted (2026-10-10)

## Context
"Force the Interaction" (v2) turns the reveal into a scheduled ceremony with a curiosity gap, and it needs the Match's fun payload — the reason, the red/green flags, the icebreakers — to stay locked until the pair has actually met. The evening's phases (ADR-0010) and the meeting handshake (ADR-0011) are in place; what is missing is what each phase shows, when the dossier unlocks, and what the JSON debug endpoints may carry before then. Explore — peeking at your compatibility with any other Participant — does not survive the v2 privacy principles: mission cards and the locked dossier replace it.

## Decision
1. **Teaser (onboarding).** A matched Participant's wait page shows the teaser only: their future Match's emoji and colour, and a hint at the first question the pair answered differently ("You disagreed on \"Would you deploy on a Friday?\" — reveal at the break"). The teaser carries no identity, score, reason, flags or icebreakers. An unmatched Participant sees the ready count while the event is still onboarding, and a clear no-match state once the Social has begun.
2. **Reveal (social).** The Match page reveals the full persona card — symbol, name, tagline — and the "find them in the room" panel (persona colour + emoji). The partner's GitHub handle is removed: personas only, per the v2 privacy principles. The compatibility score is revealed with the Match itself; it is not part of the dossier.
3. **Dossier unlock.** The reason, flags and icebreakers render as locked cards — playful blurred placeholders with "🔒 meet 🐙 to unlock", the payload itself never in the page — until the pair's dossier unlocks. The dossier unlocks once the pair's Meeting is completed (the two-sided handshake, ADR-0011) or once the event is closed, after which the evening is over and the recap takes over. The unlock arrives by push, without a manual refresh: the match page's existing stream (`/user/match-stream/{id}`, change-feed driven like the wait stream) already fires when the Participant's meetings change, and it now re-renders the dossier section (`GET /user/dossier/{id}`) alongside the meet section.
4. **JSON scrubbing.** Until the pair's dossier unlocks, the assessment — reason, flags, icebreakers — is scrubbed from the JSON debug endpoints (`/data/participants`, `/data/participant/{id}`): the Participant's `Assessment` is served as `null`. The rule is one predicate shared with the page: unlocked once the pair's Meeting is completed or the event is closed. The Big Screen's graph payload is unaffected (it carries scores only, never the assessment).
5. **Explore removed.** The route (`GET /user/explore/{myId}/{otherId}`), its handler, `explore.html`, the "Others at the event" section on the Match page and its tests are gone. Its route now 404s and no template links to it.

## Consequences

### Good
- The curiosity gap is real: during onboarding there is no endpoint, page or fragment that carries the partner's identity or the assessment.
- One predicate (`dossierUnlocked`) decides both the page's lock and the JSON scrub, so the page and the debug endpoints can never disagree about what is visible.
- The unlock needs no new stream machinery: the meeting handshake already announces completions on the change feed, and the match stream already watched the Participant's meetings.
- Removing Explore deletes a whole class of "assess any pair on demand" surface; the LLM assessment is now computed only by matching.

### Bad
- The locked dossier means a pair that never confirms their meeting never sees their flags and icebreakers on the page (they do after the event closes, and on the recap).
- The blurred placeholders are decoration: the payload is simply absent from the locked HTML, so the lock is enforced server-side, not by CSS.
- The no-match state on the wait page is permanent for the evening: an unmatched Participant is never re-paired once matching has frozen.

## Alternatives Considered

### Unlock the dossier on a one-sided confirmation
The first confirm could unlock the payload for the confirmer.
- **Pros:** one tap unlocks.
- **Cons:** the two-sided handshake is the whole point of the primitive (ADR-0011) — a dossier unlocked on one person's say-so leaks the payload to someone whose partner never showed up.
- **Decision:** rejected; only a completed Meeting unlocks.

### Scrub only the flags and icebreakers from the JSON, keep the reason and score
The acceptance rule names flags/icebreakers.
- **Pros:** the debug endpoints keep more context.
- **Cons:** the reason is the payload's headline and the assessment is stored and decoded as one unit; a partial scrub is a second, leakier rule to maintain.
- **Decision:** rejected; the whole assessment is scrubbed until the dossier unlocks.

### Keep the dossier locked forever once the event closes
The lock keyed only on the completed Meeting.
- **Pros:** the lock is purely meeting-driven.
- **Cons:** after `closed` the evening is over and the recap shows the assessment; a permanently locked Match page would contradict the JSON endpoints, which show it again.
- **Decision:** rejected; the dossier unlocks at `closed` too.

### Keep Explore behind a feature flag
The page is personas-only, so it leaks no identity.
- **Pros:** the LLM cache and the page stay for the curious.
- **Cons:** v2 replaces free exploration with mission cards and the locked dossier; a flag keeps a second way to assess arbitrary pairs alive, and every future privacy tightening has to consider it.
- **Decision:** rejected; Explore is removed outright.

### Poll for the dossier unlock
The match page could re-fetch the dossier on an interval.
- **Pros:** no stream wiring.
- **Cons:** the repo convention is push via the change feed + SSE streams, no polling.
- **Decision:** rejected; the existing match stream drives the refresh.

## Related
- GitHub: "Big reveal + locked dossier; remove Explore" (#119)
- ADR-0010 (Event Phase Machine): the teaser lives in onboarding, the reveal at the social, the recap after closed.
- ADR-0011 (Meeting Confirmation Handshake): the completed Meeting is what unlocks the dossier.
- ADR-0008 (Quality Assurance Approach): the teaser, the dossier and the scrub are covered like everything else outside `main()`.
