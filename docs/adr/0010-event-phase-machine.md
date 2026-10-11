# 0010: Event Phase Machine

## Status
Accepted (2026-10-10)

## Context
The event used to have a binary state: before the Reveal (`onboarding`) and revealed (`revealed`), switched by the admin's Reveal button. The evening is really a sequence of phases — onboarding, the social with the Big Reveal, show & tell, and the close — and the v2 tickets hang features off them: matching freezes and mission cards at the Social, meeting confirmation, show & tell sign-up and the question queue, the closing view with the recap QR. A binary flag cannot express "everything in social stays", nor a forward jump such as `social` → `closed` for a short evening.

The Big Screen also showed GitHub handles once the Matches were revealed. The v2 privacy baseline is persona names only, in every phase: handles appear only via the host's explicit identity-reveal on stage, and on the owner's own recap.

## Decision
1. **`event_state.phase` is a four-value enum** — `onboarding`, `social`, `showtell`, `closed` — owned by the Event State store (`eventstate.go`) and managed through typed operations: `Advance(to)` moves the event forward. Phases only move forward: the host may jump ahead (e.g. `social` → `closed`), advancing to the current phase is a no-op, and backward transitions — or an unknown phase — are rejected with `ErrIllegalPhaseTransition`. `Reveal()` stays as an alias for advancing to `social`.
2. **The host console (`/admin`) drives the machine**: it shows the current phase and its label, the per-phase participant counts (registered/ready/matched during onboarding; the meetings, show & tell and final stats of later tickets extend these), and one transition button per reachable forward phase, each with a browser confirm. The buttons post to `POST /admin/phase` (the `to` form value); `POST /admin/reveal` stays as an alias for advancing to `social`.
3. **What each phase enables:**
   - `onboarding` (the default after a Reset): registration, the onboarding pipeline, Continuous Matching; Matches stay hidden and matched Participants wait and see a teaser.
   - `social` (the Big Reveal at the break): matching freezes, Matches are revealed and every Match page opens; mission cards go live and meeting confirmation opens.
   - `showtell`: everything in `social` stays; show & tell sign-up and the question queue go live, and the Big Screen gains presenter mode.
   - `closed`: meeting confirmation is disabled; the closing view shows a recap QR, and personal recap pages persist.
4. **Navigation is phase-aware**: a matched Participant moves from the wait page to their Match page when `social` begins — the wait-page push stream redirects them.
5. **Migration**: a database holding the legacy value `revealed` is mapped to `social` at startup (`NewDB`).
6. **Privacy baseline**: the Big Screen shows persona names only, in every phase. The graph payload stops carrying GitHub handles entirely; the screen renders persona names always.
7. **Reset** returns the event to `onboarding` (clearing Participants and activity as before; later tickets extend it to their tables and caches).

## Consequences

### Good
- The evening's progress is explicit state the host drives, and later tickets hang their features off a phase instead of inventing flags.
- Forward-only transitions make backward states unrepresentable; the no-op rule keeps a double-clicked button harmless.
- Personas-only on the Big Screen removes a whole class of identity leaks: no phase, jump or stream can carry a handle to the projector.

### Bad
- Four phases where two sufficed for the MVP, and the host console carries up to three transition buttons.
- The `revealed` → `social` migration is one-directional: downgrading to an older binary would read `social` as onboarding.

## Alternatives Considered

### Keep the binary Reveal flag and add per-feature flags
Each later ticket would add its own boolean (`showtell_open`, `event_closed`, ...).
- **Pros:** smallest change to the state table.
- **Cons:** no ordering between the flags, no forward-only rule, and every page re-implements "is everything up to X enabled".
- **Decision:** rejected; a single ordered phase expresses it directly.

### Allow backward transitions (re-open the event)
The host could undo a misclick by moving back a phase.
- **Pros:** forgiving host console.
- **Cons:** backward transitions would resurrect hidden state (matches, meetings) that later tickets build on; a Reset already exists for starting over.
- **Decision:** rejected; phases only move forward, a Reset starts over.

### Carry handles on the Big Screen after the Social
The screen could keep showing handles once revealed, as before.
- **Pros:** more information on the projector.
- **Cons:** the projector is the one screen everyone can photograph; personas identify Participants in the room, handles do not need to leave the server.
- **Decision:** rejected; persona names only, in every phase.

## Related
- GitHub: "Phase machine: onboarding → social → showtell → closed, driven from the host console" (#113)
- ADR-0008 (Quality Assurance Approach): the phase machine is covered like everything else outside `main()`.
