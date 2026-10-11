# 0014: Show & Tell — Sign-up, Host Queue, Presenter Mode with Identity Reveal

## Status
Accepted (2026-10-10)

## Context
The event's `showtell` phase (ADR-0010) needs a stage: Participants volunteer a talk, a Host runs the queue, and the Big Screen presents the current speaker. The phase machine, the reveal + locked dossier (ADR-0012) and the contact exchange (ADR-0013) are in place; what is missing is the show & tell itself. Three properties are non-negotiable: sign-up must be live while people are mingling (the social) so talks exist by the time the phase starts; the Big Screen must never show a real identity — the whole event is persona-first — until the Host explicitly reveals the presenter; and everything arrives by push, not polling.

## Decision
1. **One table** (`showtell.go`, `showtell_signups`): one row per Participant — `title`, `description`, `repo_url`, `status` (`queued` → `on_stage` → `presented`), `identity_revealed` (0/1), the Hype Agent's `hype` line, and `position` (the queue order). Re-signing up updates the talk in place (`ON CONFLICT(participant_id) DO UPDATE`); a fresh sign-up takes `MAX(position) + 1`, so late sign-ups join the tail.
2. **Sign-up** (`POST /user/showtell/signup/{id}`): a title (required), a one-line description and an optional repo link (must be an `http(s)` URL). Gated on `Phase.AllowsShowTell()` — open during the social and show & tell only; anything else is `ErrShowTellClosed`. The personal page (both the wait page and the Match page) carries the section via `GET /user/showtell/{id}`, refreshed by the page's stream.
3. **The host queue** in `/admin`: every sign-up with its presenter's Persona and status. Queued talks can be reordered (up/down, swapping `position` with the neighbour in one atomic `UPDATE`) and removed, whatever their status. The section arrives by push: `GET /admin/showtell-queue` (HTMX fragment) refreshed by `GET /admin/showtell-stream` (SSE, watching a queue signature — id, status, position, reveal — of every sign-up).
4. **Presenter mode** (`POST /admin/showtell/stage`, actions `stage` / `reveal` / `done`), gated on `Phase.IsShowTell()` per ADR-0010 — the Big Screen gains presenter mode at show & tell. `stage` puts one queued sign-up on stage (`ErrAlreadyOnStage` while the stage is full, `ErrSignupNotQueued` for a talk that already happened), writing the Hype Agent's intro line first — never inside the transaction — and falling back to a generic line on any LLM failure, so staging never waits on the LLM. `done` marks the talk presented and frees the stage.
5. **The identity reveal** is the Host's explicit action and the only one: `reveal` sets `identity_revealed` (idempotent, only for the on-stage presenter). The Big Screen's presenter payload (`buildGraphPayload`, showtell only) carries the Persona card, the talk and the hype line — personas only. The GitHub handle/name is added to that payload only inside `if signup.IdentityRevealed`; it is the only place a handle reaches the screen besides the owner's own recap (ADR-0013). The Hype Agent's prompt carries the Persona and the talk, never the handle. Other participants' pages never carry the presenter's handle.
6. **Push, not polling**: the personal pages' streams (`/user/wait-stream/{id}`, `/user/match-stream/{id}`) watch the Participant's showtell signature (id, status, position, reveal) alongside the match signature, so the section refreshes on sign-up, staging, the reveal and done. The Big Screen's stream (`/bigscreen/stream`) pushes the presenter card and the reveal by push, like every other change.
7. **Cleanup**: `Reset` deletes the sign-ups in the same transaction as everything else; deleting a Participant (`DELETE /data/participant/{id}`) takes their sign-up with them.

## Consequences

### Good
- The reveal has exactly one door: the Host's button. No phase transition, no timing, no other page can flip it, and the persona-only baseline holds in every phase without the reveal.
- The queue is one source of truth: `position` is derived state, reorders are one atomic swap, and the ends, unknown sign-ups and non-queued sign-ups are no-ops, so the Host can hammer the buttons.
- Staging never blocks on the LLM: the hype line is generated before the write and the generic line stands on failure.
- Everything the Host and the phones see arrives by push; the streams already existed, the show & tell just joins their signatures.

### Bad
- Sign-up closes at `closed`: a Participant who missed the social and the show & tell cannot volunteer — by design, but the empty queue is the visible failure mode.
- One presenter at a time: a second `stage` is rejected while the stage is full; the Host must mark the talk done first.
- The hype line is generated once, at staging: a re-stage of the same talk (impossible — presented talks cannot return to the stage) would not regenerate it. Accepted: one talk, one intro.
- The queue section on `/admin` renders only while sign-up is open; the page's meta-refresh picks it up when the phase advances.

## Alternatives Considered

### Presenter mode from the social on
Staging could be live while sign-up is, so the Host can rehearse.
- **Pros:** more time to play with it.
- **Cons:** ADR-0010 gives the Big Screen presenter mode at show & tell; opening it earlier would put presenter cards on the public screen during the social, before the phase the audience expects them in.
- **Decision:** rejected; sign-up opens with the social, presenter mode with show & tell.

### Auto-advance the stage on done
Marking a talk done could put the next queued sign-up on stage by itself.
- **Pros:** one less click for the Host.
- **Cons:** the Host controls the pace of the show; an automatic next presenter would appear on the Big Screen without the Host's hype line being ready, and would make the reveal-timing a side effect of a status write.
- **Decision:** rejected; `done` frees the stage, the Host stages the next talk.

### Store the identity on the sign-up row
The presenter payload could carry handle/name from the start and the reveal only flips a flag read client-side.
- **Pros:** one less read at render time.
- **Cons:** the identity would live on the show & tell row — outside the Participant — and any read of the queue (host console, debug) would carry it; the reveal is a server-side fact, not a display flag.
- **Decision:** rejected; the identity stays on the Participant and is read only when `identity_revealed` is set.

### Poll the queue like the graph
The host console could re-fetch the queue on an interval like the Big Screen's graph-data.
- **Pros:** no new stream.
- **Cons:** the convention is push via the change feed and SSE streams; polling was already rejected for the personal pages.
- **Decision:** rejected; the queue stream watches a signature and pushes refreshes.

## Related
- GitHub: "Show & tell: sign-up, host queue, presenter mode with identity reveal" (#120)
- ADR-0010 (Event Phase Machine): sign-up opens with the social (`AllowsShowTell`), presenter mode with show & tell; Reset clears the sign-ups.
- ADR-0012 (The Reveal Ceremony and the Locked Dossier): personas first; the identity reveal is the deliberate exception, here for the stage.
- ADR-0013 (Contact Exchange and the Personal Recap): the only other place a handle leaves the server — the owner's own recap.
- ADR-0008 (Quality Assurance Approach): the guards, the hype fallback, the streams and the cleanup are covered like everything else outside `main()`.
