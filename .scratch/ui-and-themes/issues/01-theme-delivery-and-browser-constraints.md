---
label: wayfinder:research
title: How Themes can reach every page, and what browsers allow for motion and sound
status: open
assignee: claude (research agent, branch research/theme-delivery)
blocked_by: []
---

## Question

Every page loads Tailwind from its CDN, and colours are written directly into the templates. We need the facts a decision about Themes waits on:

1. **Tokens.** How can pages use named design tokens (colours, fonts, radii, motion durations) that a Theme swaps at runtime? Options include CSS custom properties, the Tailwind CDN's runtime `tailwind.config`, and a small build step. What are the trade-offs of each?
2. **Choice.** How can a device-stored choice (browser storage) be applied before first paint, without a flash of the wrong Theme, when HTMX swaps fragments and SSE triggers refreshes?
3. **Motion and sound on phones and projectors.** What do browsers allow? Autoplay rules for audio (iOS Safari, Android Chrome), when a user gesture is required, `prefers-reduced-motion`, the silent switch on iOS, and whether audio can play on a Big Screen tab that nobody touches.

Record primary sources (MDN, WebKit, Chrome, Tailwind docs).
