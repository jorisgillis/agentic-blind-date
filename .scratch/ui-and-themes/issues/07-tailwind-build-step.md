---
label: wayfinder:grilling
title: Do Themes ship with a Tailwind build step instead of the Play CDN
status: open
assignee: claude
blocked_by: []
---

## Question

Every page loads the Tailwind v3 Play CDN, which Tailwind documents as development-only. Themes work with it (utilities mapped to CSS variables), but shipping Themes is a natural moment to decide:

- keep the CDN
- move to the v4 CDN
- switch to the standalone Tailwind CLI, which needs no Node, producing a static CSS file that the Go server embeds and serves

The decision has consequences: a build step in development and in CI, dynamically built Persona classes that must be listed in full, and offline or venue-network behaviour. See the research on the Theme delivery ticket.
