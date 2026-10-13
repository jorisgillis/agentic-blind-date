---
label: ready-for-agent
title: Approved Posts appear in the Public Event Recap and the Host Export Pack
status: closed
assignee: claude
blocked_by: [02-posting-and-host-approval.md]
---

## Parent

`../spec.md`

## What to build

The approved Posts, newest first, attributed to Personas only, in the Public Event Recap (clickable links through the link function) and in the Host Export Pack (a `board` list in JSON; a section in Markdown, text flattened to one line).

## Acceptance criteria

- [ ] The recap and both export formats list approved Posts only.
- [ ] Deleted, rejected and pending Posts are absent.
- [ ] No handle, name or participant id appears.
- [ ] Links in the recap are clickable with `rel="noopener noreferrer"`.
- [ ] `CONTEXT.md` glossary entries for the recap and export mention the Board; `scripts/check-coverage.sh` passes.
