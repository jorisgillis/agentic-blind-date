---
label: wayfinder:grilling
title: Do Themes ship with a Tailwind build step instead of the Play CDN
status: closed
assignee: claude
blocked_by: []
---

## Question

Every page loads the Tailwind v3 Play CDN, which Tailwind documents as development-only. Themes work with it (utilities mapped to CSS variables), but shipping Themes is a natural moment to decide:

- keep the CDN
- move to the v4 CDN
- switch to the standalone Tailwind CLI, which needs no Node, producing a static CSS file that the Go server embeds and serves

The decision has consequences: a build step in development and in CI, dynamically built Persona classes that must be listed in full, and offline or venue-network behaviour. See the research on the Theme delivery ticket.

## Resolution

Decided with the human on 2026-09-24 and recorded as **ADR-0009** ("Self-Hosted Front-End Assets and In-Browser Tailwind v4"):

- Tailwind **v4's browser build** is vendored as a pinned file; there is no CLI build step. Pages define the Theme tokens in a `<style type="text/tailwindcss">` block using `@theme inline`, generated from the Theme catalogue.
- **All front-end assets are self-hosted** in `static/`, served under `/static/`: Tailwind, htmx, marked, d3 and the Themes' web fonts. There are no third-party requests, and the Docker image copies `static/`.
- Vendored files are **committed at pinned versions**, and CI checks each one's version and checksum.
- **No unstyled flash:** an inline rule hides the page until Tailwind has compiled, with a safety timeout of about 1 second.
- The standalone CLI was rejected for now, and ADR-0009 records when to revisit it (slow compiling on phones).
