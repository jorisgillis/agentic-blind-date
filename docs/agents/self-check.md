# Agent Self-Check Protocol

Every working session ends by checking its own work. The matrix below says
which checks apply for a given change. The standing rule: **no session ends
with a page-level change the agent hasn't seen rendered.**

## The matrix

| Change | Check |
|---|---|
| Any Go change | `scripts/quality.sh check` |
| Route/handler/`buildMux` change | census test + route-table row (pattern, handler, kind, marker, journey — see ADR-0022) |
| Template/page/fragment change | Playwright MCP: open the changed page(s) on the seeded app, assert the census marker in the DOM |
| CSS/theme change | same, affected pages in **both** themes, plus the Business Card (persona color survives theming) |
| Flow-level behavior change | `scripts/quality.sh e2e` |
| Change to a scoped mutation file (the 22-file decision-logic union, ADR-0024) | gremlins diff mode against the session's own diff before finishing |

`quality.sh check` is always the floor — it is the same command CI runs
(ADR-0021), so a locally-green session is a green PR by construction.

## Seed-and-serve

The Playwright MCP checks run against a known fixture state, not ad-hoc
state. One entry point over the existing e2e harness stubs (temp SQLite,
GitHub + LLM stubs) boots the app seeded with a known event — a couple of
participants, a match, mid-`social` — so every session's rendered-page
checks run against the same state and the same personas.

## Why these checks

The matrix is the direct answer to the three fragility modes this codebase
was built fast enough to acquire: green tests hiding broken pages (marker
assertions on rendered pages), logic regressions (mutation diff mode on
decision logic), and features silently disappearing (the route census and
its journey column).
