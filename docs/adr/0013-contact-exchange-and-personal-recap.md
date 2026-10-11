# 0013: Contact Exchange and the Personal Recap

## Status
Accepted (2026-10-10)

## Context
"Force the Interaction" (v2) ends with two participants who just met wanting to stay in touch, and with the evening itself needing to outlive the event. The phases (ADR-0010), the meeting handshake (ADR-0011) and the reveal + locked dossier (ADR-0012) are in place; what is missing is a way to exchange contact details after a completed meeting, and a personal recap page each Participant keeps. Both are privacy-sensitive: contact data must be opt-in and mutual, GitHub handles must not leak outside stage, and the recap must survive a restart without being guessable. The debug endpoints must never become a side door to the same data.

## Decision
1. **Contact channels** (`contact.go`): the `contact_channels` table holds one row per Participant per channel — github, email, linkedin, instagram, mastodon, other — off by default. The channels are entered once in a settings section on the personal page (both the Match page and the wait page), saved with one POST (`GET`/`POST /user/settings/{id}`); the github channel is pre-filled from the Participant's GitHub profile (the profile login, else the handle; never pre-filled for Non-GitHub Users, whose generated handle is only a unique key).
2. **Exchange prompt**: after a completed meeting, the personal page's meet section shows an "Exchange contact details?" prompt with what the other side shares with them and what they share with the other side.
3. **The mutual rule** lives in exactly one place — `ContactChannels.Revealed(owner, viewer)`: a channel is revealed to the other side only if the owner opted in to it (enabled with a value) AND the pair's Meeting is completed (the two-sided handshake, ADR-0011). The one-sided case reveals nothing: A opted in, B did not, the meeting completed — A sees nothing of B. No contact data is visible anywhere without the completed meeting.
4. **Personal recap**: `GET /user/recap/{token}` is gated by an unguessable UUID token, one per Participant, stored in the `recaps` table and created lazily — so the page persists across restarts (SQLite) and an unknown token is a 404. The recap shows the Participant's persona card, their match dossier (always unlocked there — the evening is theirs to keep), every connection (a completed meeting) with the contact channels that connection shared, the channels the Participant shared themselves, and their personal stats (connections, leaderboard rank, champion). During `closed`, the personal page shows the recap link as a QR (`GET /user/recap-link/{id}`), pushed to the phones by the existing streams when the event closes.
5. **Debug endpoints**: `GET /data/channels` reports, per Participant, how many channels they opted in to — counts only, never values or channel names. The other `/data` endpoints carry no contact values either.
6. **Handles**: a GitHub handle appears on the owner's own recap only as a revealed github contact channel — the owner opted in — and nowhere else outside stage.

## Consequences

### Good
- The mutual rule has one home (`Revealed`), so the exchange prompt, the recap and any future surface can never disagree about what is visible; the one-sided case fails closed.
- The recap token is a bearer UUID stored server-side: the page is unguessable, needs no session, and survives restarts because the token lives in SQLite, not in memory.
- The dossier is always unlocked on the recap, which gives the recap a reason to exist beyond the Match page even for a pair that never completed their meeting.
- Counts-only debug data keeps the operator dashboard useful without becoming a leak.

### Bad
- The mutual rule means a pair that completed their meeting but never opted in exchanges nothing — by design, but it can feel like the feature is broken.
- Channels are entered once and stay off until turned on: a Participant who fills in a value and forgets the toggle shares nothing, silently.
- The recap token is a bearer credential: anyone who gets the link sees the page. That is the accepted trust level of a tongue-in-cheek meet-up app (the same level as the meet-code rule, ADR-0011), but it is a real property of the system.
- The recap page is gated by the token alone, not by phase: the UUID is the privacy boundary, and the QR is only shown during `closed`.

## Alternatives Considered

### Reveal a channel as soon as the owner opts in (one-sided)
The owner's opt-in alone could unlock the value for the other side.
- **Pros:** one tap shares.
- **Cons:** the exchange is mutual by definition — A opting in must not expose B's data, and "share" implies both sides agreed to the exchange, not just one.
- **Decision:** rejected; the mutual rule requires the completed meeting, and an opted-out channel is never revealed.

### Gate the recap page by phase as well as by token
The recap could 404 until the event is `closed`.
- **Pros:** the page appears only when the QR does.
- **Cons:** the token is already unguessable; phase-gating would break the "persists across restarts" property for an event that closes while the server is down, and the QR's closed-gating is the visible part of the rule.
- **Decision:** rejected; the UUID token is the gate, the QR display is closed-gated.

### Store the channels on the participant row
The six channels could be columns on `participants`.
- **Pros:** one less table.
- **Cons:** contact data is a Participant's own subdomain, cleaned up with them (delete, reset); a separate table keeps the scrubbing and the debug counts simple.
- **Decision:** rejected; own table, `contact_channels`.

### In-memory recap tokens
Tokens could be generated per process, no table.
- **Pros:** nothing to store.
- **Cons:** a restart would invalidate every recap link — the acceptance rule is that the recap persists across restarts.
- **Decision:** rejected; table-backed tokens.

### Show channel values in the debug endpoint, with a flag
The `/data/channels` endpoint could carry the values behind an admin flag.
- **Pros:** easier debugging.
- **Cons:** the debug endpoints are JSON, easily screenshotted and shared; counts answer the operational question ("did anyone opt in?") without the values.
- **Decision:** rejected; counts only, never values.

## Related
- GitHub: "Contact exchange + persistent personal recap page" (#117)
- ADR-0010 (Event Phase Machine): the recap QR shows during `closed`; Reset clears channels and tokens.
- ADR-0011 (Meeting Confirmation Handshake): the completed Meeting is the second half of the mutual rule.
- ADR-0012 (The Reveal Ceremony and the Locked Dossier): the recap is where the dossier is always unlocked; handles stay off every page outside stage.
- ADR-0008 (Quality Assurance Approach): the mutual rule, the toggles, the recap persistence and the counts-only endpoint are covered like everything else outside `main()`.
