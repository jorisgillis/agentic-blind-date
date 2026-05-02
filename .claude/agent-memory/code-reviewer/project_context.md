---
name: Project context
description: Agentic Blind Date — Go web app for live event matchmaking via GitHub + Mistral LLM
type: project
---

Agentic Blind Date is a single-binary Go web app designed for live tech meetup events.

**Why:** Participants register with their GitHub handle; an AI pipeline fetches their profile, generates a fun persona, asks interview questions, then matches them with other participants using Mistral LLM. Results are shown on a big-screen view (D3 graph) and individual match pages.

**Key architectural decisions:**
- SQLite + WAL mode for the database (single-process, event-grade durability)
- Continuous matching: each participant is matched as soon as they finish their interview
- 3-phase matching: heuristic top-5 → LLM scoring (concurrency=2) → greedy greedy assignment
- HTMX polling for real-time UI updates (no WebSocket)
- Admin panel at /admin — no authentication

**Known deliberate tradeoffs:**
- No auth on admin or data endpoints (event-context, trusted LAN)
- Errors from DB methods are silently swallowed in many places (event liveness priority)

**How to apply:** Suggestions should respect the event-grade, short-lived context. Security hardening is still worth noting but should be framed as "low-cost improvements" rather than "must-haves for production SaaS".
