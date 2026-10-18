---
label: ready-for-agent
title: Participants post, the Host approves, everybody reads on their personal page
status: closed
assignee: claude
blocked_by: [01-links-in-post-text.md]
---

## Parent

`../spec.md`

## What to build

The Board end to end on the phones and the host console.

- `board_posts` table and the Board module: post (ready, not closed, 1–280 characters, cap 10 pending+approved), list what a Participant may see, delete own, Host moderation (pending → approved/rejected, approved → rejected), Signature, removal with a Participant.
- Personal page section on the wait and match pages: form, one list (approved of everyone plus own other Posts with status and delete button), refreshed by the personal streams.
- Host console section: pending with Approve/Reject, approved with Take down.
- Reset deletes all Posts; deleting a Participant deletes theirs.
- A ticker line when a Post is approved (never the text of a pending Post).
- Docs: `AGENT_OVERVIEW.md` routes and table, README route table.

## Acceptance criteria

- [ ] Guards: not ready, closed, empty, over 280 characters, over the cap (rejected/deleted do not count).
- [ ] A pending Post is visible only to its owner; an approved one to everyone; a rejected one only to its owner.
- [ ] Only the owner can delete; others get not found.
- [ ] The Host can moderate in every phase; invalid transitions are rejected.
- [ ] Personal streams push when a Post is approved, taken down or deleted.
- [ ] Reset and Participant deletion clean up; failures are reported.
- [ ] Error branches covered by fault injection; `scripts/check-coverage.sh` passes.
