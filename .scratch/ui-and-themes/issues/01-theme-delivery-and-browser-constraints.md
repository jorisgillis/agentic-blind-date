---
label: wayfinder:research
title: How Themes can reach every page, and what browsers allow for motion and sound
status: closed
assignee: claude
blocked_by: []
---

## Question

Every page loads Tailwind from its CDN, and colours are written directly into the templates. We need the facts a decision about Themes waits on:

1. **Tokens.** How can pages use named design tokens (colours, fonts, radii, motion durations) that a Theme swaps at runtime? Options include CSS custom properties, the Tailwind CDN's runtime `tailwind.config`, and a small build step. What are the trade-offs of each?
2. **Choice.** How can a device-stored choice (browser storage) be applied before first paint, without a flash of the wrong Theme, when HTMX swaps fragments and SSE triggers refreshes?
3. **Motion and sound on phones and projectors.** What do browsers allow? Autoplay rules for audio (iOS Safari, Android Chrome), when a user gesture is required, `prefers-reduced-motion`, the silent switch on iOS, and whether audio can play on a Big Screen tab that nobody touches.

Record primary sources (MDN, WebKit, Chrome, Tailwind docs).

## Resolution

Findings, with primary sources: branch `research/theme-delivery`, file `research/theme-delivery.md`.

- **Tokens:** a Theme is a block of CSS custom properties under `[data-theme="…"]`, and Tailwind utilities map onto those variables. This works with the current v3 Play CDN (`rgb(var(--x) / <alpha-value>)`), the v4 CDN, and a build step (`@theme inline`). The Tailwind docs call both Play CDNs development-only; the standalone Tailwind CLI, which needs no Node, is the production route. Persona colours stay fixed `*-400` classes that never read Theme variables.
- **Choice:** on phones, an inline classic `<head>` script copies the `localStorage` choice to `<html data-theme>` before first paint. On the Big Screen, the server renders the attribute and SSE pushes live changes. HTMX and SSE swaps never touch `<html>`, so the Theme survives them, as long as fragments hard-code no colours.
- **Motion:** duration and easing tokens that collapse under `prefers-reduced-motion`; JS animation checks `matchMedia`.
- **Sound:** every browser needs sticky user activation. Use one lazily created `AudioContext`, resumed on the first tap; it stays unlocked for the page's life, and SSE and HTMX never reload the page. On iOS, Web Audio is `ambient` and respects the silent switch; `navigator.audioSession.type = 'playback'` (Safari 17+) plays through it. The Big Screen needs one visible "enable sound" click per page load (or a kiosk launch flag), and every Theme must work with no sound.
- **Gaps:** Mozilla's own autoplay article couldn't be loaded, so Firefox relies on MDN. Whether activation survives a full reload in Chrome is unverified; plan for one click per load.

Follow-up: raised the Tailwind build question as its own ticket, "Do Themes ship with a Tailwind build step instead of the Play CDN" (07).
