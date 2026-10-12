# 0011: Meeting Confirmation Handshake

## Status
Accepted (2026-10-10)

## Context
"Force the Interaction" (v2) needs a primitive everything else derives from: two participants confirming they physically met. Stamps, dossier unlock, the leaderboard and the contact gate all hang off it. The confirmation has to work from two phones in the room within seconds — one scans the other's QR, the other types a 6-character code — and it has to be honest: a meeting only counts when *both* sides confirmed it, not when one person claims it.

The event's phases (ADR-0010) decide when confirmation is open: during `social` and `showtell`, closed when `closed`. The personal page (the Match page, and the wait page for Participants who are not matched yet) is where a Participant shows their own QR and starts "I met someone".

## Decision
1. **Meet codes** (`meetcode.go`): every Participant gets a 6-character code at registration, drawn from an unambiguous alphabet (no `0/O/1/I`). The code is derived deterministically from the Participant's id (a hash, not a sequence), so it is stable across restarts; a collision re-derives deterministically with a salt until the code is unique. Codes are unique in the database (a unique index), and Participants who registered before codes existed are backfilled at startup. A database from before meet codes is upgraded in place: the migration adds the column, the index is created after the migrations, the backfill fills it.
2. **Meet link**: `{base}/user/m/{id}` is the Participant's meet page (`GET /user/m/{id}`), rendered as a QR on their personal page. Scanning it opens the target's persona card ("You found 🐙 The YAML Wrangler!") with a confirm button.
3. **Canonical pair ordering** (`meetings.go`): the `meetings` table holds exactly one row per pair — `participant_a` is the lexicographically lower id, reusing the matcher's pair-key ordering — so it does not matter who confirms first. Each side's confirmation is tracked separately (`a_confirmed_at` / `b_confirmed_at`), and `completed_at` is set when both sides have confirmed.
4. **Two-sided confirm**: confirming records *your* side of the meeting. A second confirm from the same side is a friendly no-op ("You already met 🐙!") — idempotent per side, never an error. A one-sided confirmation shows as "pending" on the initiator's card and as an offer to confirm back on the other side's. Self-entries (`ErrSelfMeeting`) and unknown codes are rejected with clear messages. Confirmation is enabled only while the phase allows meetings (`social`/`showtell`); outside it the confirm is rejected (`ErrMeetingsClosed`). When both sides have confirmed, the meeting is completed: the ticker announces it ("🤝 The Gopher and The Crab met!", the pair in canonical order) and each participant earns 1 connection — derived from completed meetings, never stored. A Reset clears all meetings silently: one-sided confirmations expire, no message, no nag.
5. **Origin tagging**: the row's `origin` (`match` / `mission` / `other`) is tagged automatically at creation, from the initiator's perspective. A pair that are each other's current Match meets as `match` — symmetric, whoever initiates and whatever context they came from. Otherwise the initiator's origin stands (`mission` is not symmetric). Meeting anyone counts, not only mission tiles.
6. **Cookie/code identity rule**: the confirmer is identified by the existing participant cookie. `POST /user/met/{id}` takes the confirmer's own id in the path and the target's meet code as the `code` form value; a missing or mismatched cookie is rejected. Without a cookie, the meet page offers entering one's *own* meet code (`GET /user/m/{id}?me=CODE`), which resolves the Participant and sets the cookie — the same trust level as the code-based confirm itself: anyone who knows a code can act as that Participant, on this ticket's routes only.
7. **Push**: every state change (a confirm, a completion, a reset, a Participant leaving) announces on the change feed, so phones and the Big Screen update through the existing SSE streams — no polling. The personal page's meet section (`GET /user/meet-status/{id}`, included by both the Match page and the wait page) refreshes from the page's stream when the Participant's meetings or the phase change.

## Consequences

### Good
- One canonical row per pair makes the handshake race-free: confirms serialize on the row, and "did we meet?" is one lookup regardless of who confirmed first.
- Two-sided confirmation is honest by construction — a meeting exists only when both people say so — and the friendly no-op makes double-taps harmless.
- Deterministic codes need no lookup table to explain, and the unique index makes collisions impossible rather than unlikely.
- Deriving connections from completed meetings keeps the count honest: it cannot be edited, only earned.

### Bad
- The cookie/code rule means a code is a bearer credential for the meet routes: anyone who sees a code can confirm as that Participant. That is the accepted trust level of a tongue-in-cheek meet-up app, but it is a real property of the system.
- One row per pair with per-side timestamps is slightly more bookkeeping than a single `confirmed` flag; the pending/awaiting states exist only to be shown, never to be acted on.
- The origin is fixed at creation: if a pair meets as `other` and only later become Matches, the meeting stays `other`.

## Alternatives Considered

### One confirm completes the meeting
The first person to confirm could complete it, with the second confirm as a no-op.
- **Pros:** one phone, half the taps.
- **Cons:** a meeting would exist on one person's say-so — a mis-tap or a prank creates a connection the other person never agreed to. The two-sided handshake is the whole point of the primitive.
- **Decision:** rejected; both sides must confirm.

### Two rows per pair (one per direction)
Each side's confirm could write its own row, joined at read time.
- **Pros:** each confirm is a plain insert, no read-then-write.
- **Cons:** two phones confirming at the same moment race both inserts; "one row per pair" becomes an invariant every reader must reconstruct, and completion becomes a join. The canonical row makes the race impossible by construction.
- **Decision:** rejected; one canonical row, confirms serialized on it.

### Sequential codes or the participant id as the code
Codes could be short counters, or the meet link could carry a shared secret per pair.
- **Pros:** shorter codes; no backfill.
- **Cons:** sequential codes are guessable (bearer credentials must not be), and a per-pair secret breaks the "meet anyone" flow — the code has to exist before the pair is known. Deterministic codes from the id need no storage beyond the column itself.
- **Decision:** rejected; deterministic, salted, unique codes derived from the id.

### Trusting the `code` form value as the confirmer's identity
The confirm POST could take both ids/codes from the form, no cookie needed.
- **Pros:** one less concept.
- **Cons:** anyone could confirm as anyone else — the handshake would record meetings nobody agreed to. The cookie pins the confirmer; the code only identifies the target.
- **Decision:** rejected; the confirmer is the cookie, the code names the target.

## Related
- GitHub: "Meeting confirmation: the handshake (meet codes, QR meet links)" (#114)
- ADR-0010 (Event Phase Machine): confirmation is open during `social`/`showtell`, closed when `closed`; Reset returns the event to `onboarding` and clears meetings.
- ADR-0008 (Quality Assurance Approach): the handshake is covered like everything else outside `main()`.
- ADR-0001 (Three-Phase Matching Algorithm): the canonical pair ordering reuses the matcher's pair-key ordering.
