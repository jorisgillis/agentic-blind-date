---
label: ready-for-agent
title: Approved Posts rotate on the Big Screen with a QR of their link
status: closed
assignee: claude
blocked_by: [02-posting-and-host-approval.md]
---

## Parent

`../spec.md`

## What to build

The Big Screen payload carries the 10 newest approved Posts (persona name, symbol, colour, text, first link) in every phase, except while a presenter is on stage. The screen rotates through them in a card with a QR of the first link, using the server-side QR route. Personas only.

## Acceptance criteria

- [ ] The payload has the approved Posts only, newest first, capped at 10, with the first link.
- [ ] No pending or rejected Post text reaches the payload.
- [ ] While a presenter is on stage the payload carries no Board.
- [ ] The screen script parses (inline script test) and the card has no QR for a Post without a link.
- [ ] `scripts/check-coverage.sh` passes.
