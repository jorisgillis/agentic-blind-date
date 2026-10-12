# 0015: Show & Tell — Audience Questions Through Personas

## Status
Accepted (2026-10-10)

## Context
The show & tell phase (ADR-0014) has a stage but no audience. The v2 PRD ("Force the Interaction") wants the audience to participate: questions through their personas. Four properties are non-negotiable: questions render with the asker's persona only — the event is persona-first everywhere outside the stage; questioning must not require a meeting (meeting the presenter still needs the handshake of ADR-0011); questions earn nothing — the raffle is dropped from v2, and answered questions only count toward the audience-participation success metric (≥10 questions); and everything arrives by push, not polling.

## Decision
1. **One table** (`showtell_questions.go`, `showtell_questions`): one row per question — `participant_id` (the asker), `signup_id` (the Sign-up on stage when it was submitted), `text` (1–300 characters), `status` (`queued` → `asked` → `answered`, or `dismissed` from queued or asked; answered and dismissed are final) and `created_at`. The ShowTell module owns both tables.
2. **Submission** (`POST /user/showtell/question/{id}`, the asker's own id): gated on `Phase.IsShowTell()` (`ErrShowTellClosed`), live only while someone is on stage (`ErrNoPresenter` — the button is disabled while the stage is empty), with the text validated (`ErrInvalidQuestion`). No meeting required, and asking earns nothing: no connection, no raffle entry. The personal page's section (`GET /user/showtell/{id}`) carries the form and the asker's questions with their status.
3. **The host queue** in `/admin`: every question with its asker's Persona. `POST /admin/showtell/question` marks a question `asked` (putting it on the Big Screen — one current question at a time, `ErrAlreadyAsked`), `answered`, or `dismissed`; invalid transitions are rejected (`ErrInvalidQuestionTransition`). The section arrives by push: `GET /admin/showtell-questions` (HTMX fragment) refreshed by the admin stream, which watches a questions signature.
4. **The Big Screen**: the presenter payload gains the current question — its text and the asker's Persona (symbol, name, color), rendered personas only ("🐯 The Crab asks: …"). The screen's stream pushes it within one stream cycle; answering or dismissing takes it down.
5. **Push, not polling**: the personal pages' streams watch the Participant's question signature (id, status) alongside the showtell signature, so the form binds to whoever is on stage and the asker's list tracks status changes. The Big Screen's stream pushes the current question like every other change.
6. **The recap** (ADR-0013) gains the Participant's show & tell items: their Sign-up and their questions with how each ended up — personas only.
7. **Cleanup**: `Reset` deletes the questions in the same transaction as everything else; deleting a Participant takes their questions — and the questions bound to their Sign-ups — with them.

## Consequences

### Good
- Questioning needs no meeting and earns nothing: participation is its own reward, and the raffle is gone — answered questions count toward the ≥10-questions success metric.
- One current question at a time keeps the Big Screen legible: the host answers or dismisses before asking the next.
- Personas only, everywhere: the question joins the asker's Persona live (COALESCEd), so even a question whose asker left the event still lists — in debug views — with an empty persona, and no handle ever travels with a question.
- The queue's status machine is explicit: transitions are validated server-side, and answered and dismissed are final.

### Bad
- Questioning closes with the phase: no new questions before show & tell or after it closes, and none while the stage is empty — by design, but the disabled button is the visible failure mode.
- One current question at a time: a question stuck on the Big Screen blocks the next until the host answers or dismisses it.
- The 300-character cap is arbitrary, but it keeps one question from flooding the Big Screen.

## Alternatives Considered

### Free-floating questions, bound when asked
A question could carry no Sign-up and bind to whoever is on stage when the host marks it asked.
- **Pros:** one less foreign key; the question always targets the current talk.
- **Cons:** "the question is about the talk on stage" would stop being true the moment the stage changes before the host asks it; the binding is a fact of submission, not of display.
- **Decision:** rejected; questions bind to the on-stage Sign-up at submission.

### A raffle for asked questions
Asking or having a question asked could enter a raffle.
- **Pros:** a visible incentive to participate.
- **Cons:** dropped from v2 — questions earn nothing; they count toward the audience-participation success metric instead.
- **Decision:** rejected; no raffle.

### Real names on the Big Screen for questions
The screen could show the asker's name to give the moment weight.
- **Pros:** recognition for the asker.
- **Cons:** personas only, like everywhere outside the stage; the identity reveal (ADR-0012) is the deliberate exception and there is none for askers.
- **Decision:** rejected; questions are persona-only everywhere, including the asker's own recap.

### Several questions on the Big Screen at once
The screen could show the whole queue.
- **Pros:** the audience sees what is coming.
- **Cons:** one current question keeps the screen legible during a talk; the queue lives on the host console.
- **Decision:** rejected; the screen shows the current question only.

## Related
- GitHub: "Show & tell: audience questions through personas" (#121)
- ADR-0014 (Show & Tell — Sign-up, Host Queue, Presenter Mode): the stage the questions bind to; the presenter payload carries the current question.
- ADR-0010 (Event Phase Machine): questioning is live during show & tell only; Reset clears the questions.
- ADR-0011 (Meeting Confirmation Handshake): questioning needs no meeting — the handshake is for meetings.
- ADR-0012 (The Reveal Ceremony and the Locked Dossier): personas first; there is no reveal for askers.
- ADR-0013 (Contact Exchange and the Personal Recap): the recap's show & tell items.
- ADR-0008 (Quality Assurance Approach): the guards, the transitions, the streams and the cleanup are covered like everything else outside `main()`.
