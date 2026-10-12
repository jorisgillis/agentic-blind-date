# 0016: Closing Ceremony, Public Recap and the Host Export Pack

## Status
Accepted (2026-10-10)

## Context
The phase machine (ADR-0010) ends at `closed`, and the evening's artifacts exist: completed meetings and the leaderboard with its Champion (#116), contact exchange and the personal recap (#117), show & tell with its audience questions (#120, #121). What is missing is the close itself: a closing view on the Big Screen, a public recap of the evening, and the host's post-meetup blast pack. The raffle is dropped from v2: the champion of connections alone takes the Merge Trophy. Three properties are non-negotiable: privacy — the public recap shows personas, and the export pack carries personas and counts only, never contact-channel values or GitHub identity mappings; gating — the public recap exists only once the event is closed; and the personal recap pages from #117 stay live and untouched in behavior.

## Decision
1. **The closing view** (`closing.go`, the Big Screen's payload): when the phase is `closed`, the graph payload gains the evening's stats — participants, completed meetings, contact exchanges (a count of the opted-in channel values that changed hands on completed meetings, never the values), audience questions asked and answered — and `recap_url` (`/recap`). The screen already shows the final leaderboard naming the Champion, the graph with all meeting edges and the ticker's champion announcement; the closing view adds a stats panel, a champion line on the leaderboard, and swaps the join QR for the recap QR. Meeting confirmation stays disabled (`AllowsMeetings` is false at `closed`; the confirm POST is rejected with `ErrMeetingsClosed`).
2. **The public event recap**: `GET /recap`, available only once the event is `closed` (404 before). It shows the evening's stats, the top connectors (the top 5 of the connection leaderboard), the persona gallery (every Participant as their Persona), and the show & tell list with links. Personas only — no contact data, no identities.
3. **The host export pack**: `GET /admin/export`, downloaded as JSON (the default) or Markdown (`?format=md`), linked from the host console. The pack (`ExportPack`) carries the headline stats, the full connection leaderboard, the top Personas (top 5, with taglines), the best LLM-generated match reasons and icebreakers (the top 3 assessments by score, anonymized to the pair's Personas), the show & tell list, the question count, and the derived success metrics. Personas and counts only — the pack's structs have no field for a Participant id, a handle or a contact value, so the privacy rule holds by construction.
4. **Success metrics** (derived, never stored): completed meetings, connections made (one per side per completed meeting), answered questions, and the audience-participation goal — met at ten or more answered questions (ADR-0015).
5. **The Closing module** owns all of it (`closing.go`): `Stats`, `Leaderboard`, `Gallery`, `Talks`, `BestMatches`, `Pack`, `Recap` and the Markdown renderer. Every list is persona-only (`ExportPersona`/`ExportStanding`/`ExportTalk`/`ExportMatch` carry no ids); a list that cannot be read comes back empty (logged), so the closing surfaces degrade to fewer numbers, never to a failure.
6. **The personal recap is untouched**: `GET /user/recap/{token}` keeps its behavior from ADR-0013 — it stays live, and it remains the only place contact values appear, on the owner's own page.

## Consequences

### Good
- The privacy rule for the closing surfaces holds by construction: the export structs cannot carry an id, a handle or a contact value, and a test asserts the rendered JSON and Markdown contain none of the seeded secrets.
- The public recap is gated to `closed`, so the evening's numbers are final when they publish.
- The host's blast pack is one download in two formats, ready to paste.
- The stats derive from the meetings, the channels and the questions at read time — they cannot be edited, only earned.

### Bad
- A stat that cannot be read renders as zero — the closing view is a dashboard, not a ledger; the debug endpoints remain the source of truth mid-incident.
- The export's best matches are capped at three and its top personas at five: the blast stays short; the full lists live on the recap and the debug endpoints.

## Alternatives Considered

### Gate the export to closed
The host could only download the pack once the event is closed.
- **Pros**: the pack is final.
- **Cons**: the host console is a trusted surface, a mid-event export is useful for a live recap, and the ticket does not gate it.
- **Decision**: rejected; the export is available to the host at any time.

### Include the question texts in the export
The blast could quote the audience's questions.
- **Pros**: more fun content.
- **Cons**: questions render through personas everywhere (ADR-0015); the count suffices for the blast.
- **Decision**: rejected; question count only.

### Identity mappings in the host export
The host knows the participants; the pack could map personas to handles.
- **Pros**: the host could follow up personally.
- **Cons**: the privacy rule is non-negotiable — the pack is personas and counts only; the host's follow-up channel is the revealed contact exchange, not the export.
- **Decision**: rejected.

## Related
- GitHub: "Closing ceremony, public recap + host export pack (champion, no raffle)" (#122)
- ADR-0010 (Event Phase Machine): the `closed` phase enables the closing view.
- ADR-0013 (Contact Exchange and the Personal Recap): the personal recap stays live and untouched; the mutual rule keeps contact values off every other surface.
- ADR-0015 (Show & Tell — Audience Questions): the audience-participation success metric (≥10 answered questions).
- ADR-0008 (Quality Assurance Approach): the closing surfaces are covered like everything else outside `main()`, including a test asserting zero contact values and zero identity mappings in the export.
