# Spec: The Board

Participants share useful links and information with the rest of the meet-up through short Posts, in the manner of Twitter or Bluesky. Vocabulary follows `CONTEXT.md` ("Board and Post").

## Problem Statement

Participants often know something the rest of the room would value: a link to a repo, a talk, a paper, a tool. Today there is no place for it. The Slide Decks are the Host's alone, and a Show & Tell sign-up carries one repo link for one talk. People end up shouting URLs across the room.

## Solution

A **Board**. Any Participant who is `ready` writes a short plain-text **Post** (1–280 characters) that may contain links. The Host approves each Post before anyone else sees it. Approved Posts appear on every Participant's personal page by push, rotate on the Big Screen with a QR of the Post's link, and end up in the Public Event Recap and the Host Export Pack. Posts show the poster's Persona only.

## User Stories

1. As a Participant, I want to post a short message with a link, so that the room benefits from what I found.
2. As a Participant, I want to write in the manner of a Bluesky post (one short text), so that posting takes seconds.
3. As a Participant, I want links in my Post to be clickable for everyone, so that people can open them from their phones.
4. As a Participant, I want to be told when my Post is empty or over 280 characters, so that I can fix it.
5. As a Participant, I want my Post to carry my Persona and nothing else, so that I stay anonymous like everywhere else in the app.
6. As a Participant, I want to see my Post as "waiting for the Host" until it is approved, so that I know it was not lost.
7. As a Participant, I want to see when my Post was not approved, so that I do not wonder.
8. As a Participant, I want to delete my own Post, so that I can take back a mistake.
9. As a Participant, I want a cap of 10 Posts, so that nobody floods the Board, and I want a rejected or deleted Post not to use up my quota.
10. As a Participant who is not yet `ready`, I want to read the Board but not post, so that a Post always has a Persona.
11. As a Participant, I want approved Posts to appear on my page without a refresh, so that I see news as it comes.
12. As a Participant, I want the newest Posts first, so that I see what is fresh.
13. As a Participant, I want posting open in every phase until the event closes, so that I can share at any moment of the evening.
14. As a Participant, I want the Board to stay readable after the event closes, but not postable, so that the evening ends cleanly.
15. As the Host, I want a queue of pending Posts in the host console, so that I can approve or reject them.
16. As the Host, I want to take down an approved Post, so that I can fix a mistake fast.
17. As the Host, I want to moderate in every phase, including after the close, so that the Export Pack is clean.
18. As the Host, I want a rejected Post to be final and need no reason, so that moderating stays quick.
19. As the Host, I want only `http(s)` links to become clickable, so that nobody can post a `javascript:` link.
20. As the Host, I want links to open in a new tab with `noopener noreferrer`, so that the app is not exposed to the linked site.
21. As the room, I want approved Posts to rotate on the Big Screen with the poster's Persona, so that everybody sees what is shared.
22. As the room, I want a QR of the Post's link on the Big Screen, so that I can scan it with my phone.
23. As a presenter on stage, I want the Board to stay off the Big Screen while I present, so that it does not compete with me.
24. As a reader of the Public Event Recap, I want the approved Posts, newest first, so that I can find the links shared during the evening.
25. As the Host, I want the approved Posts in the Export Pack (JSON and Markdown), so that I can paste them in the post-meetup blast.
26. As the Host, I want a Reset to delete all Posts, and a deleted Participant's Posts to go with them, so that nothing outlives its owner.
27. As a Participant, I want Posts of a Participant who left the event to disappear, so that the Board never shows a stranger.
28. As the Host, I want a Post's text never to reach any surface before approval, so that a bad Post never shows up on anyone's phone or the Big Screen.

## Implementation Decisions

- A new **Board module** owns a `board_posts` table (id, participant, text, status, created). Statuses: `pending` → `approved` or `rejected`; an approved Post can still be rejected (the Host's take-down); rejected is final. Deleting removes the row.
- Posting needs a `ready` Participant, a phase other than `closed`, 1–280 characters (counted as characters, not bytes, trimmed), and fewer than 10 Posts that are pending or approved. The cap check and the insert share one transaction.
- Moderation is the Host's, in every phase. A Post's text is returned to other Participants, the Big Screen, the recap and the export only when approved. The poster sees their own Posts in every status.
- Only a Post's owner can delete it; asking for someone else's Post is "not found".
- Links: one pure function turns Post text into safe HTML. It escapes everything, and wraps `http://` and `https://` URLs in links that open in a new tab with `rel="noopener noreferrer"`, leaving trailing punctuation outside the link. The same URL finder gives the Post's first link, used by the Big Screen QR.
- Personal pages: a Board section next to the Slide Decks section on the wait and match pages. It holds the post form (when posting is open for this Participant) and one list: approved Posts of everyone plus the Participant's own other Posts, newest first, each own Post marked with its status and a delete button. It refreshes through the existing personal streams, which watch a Board signature.
- Host console: a Board section lists pending Posts with Approve and Reject, and approved Posts with Take down. The console already refreshes itself every few seconds.
- Big Screen: the graph payload carries up to the 10 newest approved Posts (persona name, symbol, colour, text, first link), except while a presenter is on stage. The screen rotates through them in a card, with a QR of the first link, rendered through the existing server-side QR route.
- Public Event Recap and Host Export Pack carry the approved Posts, newest first, attributed to Personas only; in Markdown the text is flattened to one line.
- Reset deletes every Post in its transaction; deleting a Participant deletes their Posts.
- Activity ticker: approving a Post logs "📣 {persona} posted on the board" — never the text of a pending Post.
- The Board is separate from Show & Tell: a Sign-up's repo link stays part of the talk.

## Testing Decisions

- Test external behaviour through the HTTP routes (`newTestServer`), the same seam as Slide Decks and the Show & Tell questions: guards, visibility, moderation, push, Big Screen payload, recap, export, Reset.
- Use the existing fault injection for error branches; ADR-0008 requires 100% function coverage outside `main()`.
- The link function gets a table test (it is pure): `javascript:` and tag injection, trailing punctuation, parentheses, several links, no link.
- Prior art: `showtell_questions_test.go`, `slides_test.go`.

## Out of Scope

- Replies, reactions, edits, images, link previews, fetching the linked pages.
- Rejection reasons, notifications to the poster beyond the status.
- Posts by the Host as a Participant; the Host's own material stays in Slide Decks.
- Searching, pinning, or per-Post permalinks.
- Rate limiting beyond the per-Participant cap.

## Further Notes

The design came out of a wayfinding session; see `CONTEXT.md` ("Board and Post") for the agreed vocabulary.
