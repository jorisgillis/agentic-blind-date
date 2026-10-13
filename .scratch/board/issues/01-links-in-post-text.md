---
label: ready-for-agent
title: Post text renders as safe HTML with clickable links
status: closed
assignee: claude
blocked_by: []
---

## Parent

`../spec.md`

## What to build

A pure function that turns a Post's plain text into safe HTML, plus the template function that exposes it, and a way to get the Post's first link.

- Everything is escaped. Newlines are kept.
- `http://` and `https://` URLs become links with `target="_blank"` and `rel="noopener noreferrer"`. Nothing else becomes a link (no `javascript:`, no bare domains).
- Trailing punctuation (`.,;:!?`, and an unbalanced `)`) stays outside the link.
- The first link of a text is available as a plain string (empty when there is none), for the Big Screen QR.

## Acceptance criteria

- [ ] A table test covers: no link, one link, several links, trailing punctuation, parentheses, `javascript:` text, `<script>` text, `&` and quotes in a URL.
- [ ] The template function is available to every page.
- [ ] `scripts/check-coverage.sh` passes.
