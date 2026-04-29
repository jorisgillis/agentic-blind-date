# Agentic Blind Date - Project Overview

## Project Summary

**Agentic Blind Date** is a **web application for tech meetups** that demonstrates **agentic AI** in a fun, interactive format. It's inspired by the TV show *Blind Date* but matches participants based on technical preferences and GitHub profiles instead of romance.

## Core Concept

Participants register with their GitHub handle and are processed through an **AI pipeline** that:
1. Fetches their public GitHub profile data (languages, repos, bio, stats)
2. Generates a humorous anonymous persona (e.g., "The Grumpy Kernel King")
3. Conducts an interview with 8 questions (5 fixed + 3 AI-generated based on profile)
4. Matches them with their most compatible peer using AI analysis

The reveal happens simultaneously for all participants on a big screen with a visual graph.

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                        Go HTTP Server (main.go)                       │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌────────────┐ │
│  │  DB (SQLite) │  │ GitHub Client│  │ Mistral Client│  │  Handlers  │ │
│  │   (db.go)    │  │   (github.go)│  │  (mistral.go) │  │ (handlers.go)│ │
│  └──────────────┘  └──────────────┘  └──────────────┘  └────────────┘ │
│                              │                  │                   │
│                              ▼                  ▼                   ▼
│                    ┌─────────────────┐  ┌──────────┐  ┌──────────┐
│                    │  Agent Pipeline  │  │   D3.js  │  │  HTMX    │
│                    │    (agents.go)   │  │(screen)  │  │(templates)│
│                    └─────────────────┘  └──────────┘  └──────────┘
└─────────────────────────────────────────────────────────────────────┘
                              │
                              ▼
                    ┌─────────────────────┐
                    │   SQLite Database    │
                    │  (blind_date.db)     │
                    └─────────────────────┘
```

---

## Key Components

| Component | File | Purpose |
|-----------|------|---------|
| **Server & Routing** | `main.go` | HTTP server setup, route registration, .env auto-loading |
| **Database Layer** | `db.go` | SQLite schema, Participant type, all CRUD queries |
| **GitHub Client** | `github.go` | Fetches user profiles, repos, languages from GitHub API |
| **Mistral Client** | `mistral.go` | Handles chat completions with Mistral AI API |
| **AI Agents** | `agents.go` | Pipeline: Setup (persona, questions), Matching (3-phase algorithm) |
| **Fixed Questions** | `questions.go` | 5 predefined opinionated questions |
| **HTTP Handlers** | `handlers.go` | All route handlers, HTMX fragments, JSON endpoints |
| **Templates** | `templates/*.html` | Server-side rendered HTML with HTMX, Tailwind, D3.js |

---

## Workflow

### Participant Journey
1. **Landing** (`/user`) → Enter name + GitHub handle
2. **Onboarding** (`/user/onboard/{id}`) → Async AI pipeline runs in background
3. **Interview** → Answer 8 questions (5 fixed + 3 custom) via HTMX forms
4. **Waiting Room** (`/user/wait/{id}`) → See persona, profile summary, poll for reveal
5. **Match Reveal** (`/user/match/{id}`) → See partner, score, flags, icebreakers

### Admin Actions
- **Trigger Reveal** (`POST /admin/reveal`) → Runs matching algorithm
- **Reset Event** (`POST /admin/reset`) → Clears all data for new session

### Big Screen
- **Projector View** (`/bigscreen`) → D3.js force-directed graph

---

## AI Pipeline

### RunSetup (Per Participant, Async)
```
GitHub Fetch Profile
    → Profile Agent (Mistral) → Generate Persona + Tagline
    → Interviewer Agent (Mistral) → Generate 3 Custom Questions
    → Store & Mark as "interviewing"
```

### RunMatching (Triggered by Admin)
**Phase 1 - Candidate Selection:**
- Score all pairs: +3 per shared language, +1 per matching answer
- Each participant keeps **top 5 candidates**
- Collect unique pairs → O(n×5) max, not O(n²)

**Phase 2 - LLM Scoring:**
- Mistral scores each candidate pair for: score (0-100), reason, red/green flags, icebreakers
- **2 concurrent calls**, results **cached in-memory**

**Phase 3 - Greedy Assignment:**
- Sort pairs by LLM score descending
- Assign each participant to highest-scoring available partner
- Fallback to heuristic matching for stragglers

---

## Technical Stack

| Category | Technology | Notes |
|----------|------------|-------|
| Backend | Go 1.26+ | Standard library `net/http` |
| Database | SQLite3 | WAL mode, single file |
| AI | Mistral AI | `mistral-small-latest` model |
| Frontend | HTMX 1.9 | Dynamic updates without writing JS |
| Styling | Tailwind CSS | CDN (Play), no build step |
| Visualization | D3.js 7 | Force-directed graph |
| QR Code | QRCode.js | For participant join |

---

## Routes

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/` | Redirect → `/user` |
| GET | `/user` | Registration form |
| POST | `/user/join` | Create participant, start pipeline |
| GET | `/user/onboard/{id}` | Pipeline progress page |
| GET | `/user/pipeline/{id}` | HTMX fragment (polled 2s) |
| POST | `/user/answer/{id}` | Store answer, next question |
| GET | `/user/wait/{id}` | Waiting room |
| GET | `/user/wait-status/{id}` | HTMX fragment (polled 3s) |
| GET | `/user/match/{id}` | Match results |
| GET | `/bigscreen` | Projector view with D3 graph |
| GET | `/bigscreen/state` | HTMX fragment (polled 3s) |
| GET | `/bigscreen/graph-data` | JSON for D3 (polled 5s) |
| GET | `/admin` | Host control panel |
| POST | `/admin/reveal` | Start matching |
| POST | `/admin/reset` | Clear all data |
| GET | `/data/*` | Debug endpoints (JSON) |

---

## Data Model

### SQLite Schema

**participants:**
```sql
id, github_handle, name, persona_name, persona_color, persona_symbol,
persona_tagline, profile_json, custom_questions, answers_json,
pipeline_step, matched_with, compat_score, compat_reason,
red_flags, green_flags, icebreakers, created_at
```

**event_state:**
```sql
key ('phase'), value ('onboarding' | 'matching' | 'revealed')
```

**activity_log:**
```sql
id, message, created_at
```

### Participant States
1. `fetching_github` → `creating_persona` → `interviewing` → `ready` → `matched`

---

## Environment & Deployment

**Requirements:**
- Go 1.21+
- C compiler (for SQLite3 CGO dependency)
- Mistral AI API key (required)
- GitHub PAT (optional, increases rate limit from 60 to 5000 req/hr)

**Quick Start:**
```bash
cp .env.example .env
# Edit .env: MISTRAL_API_KEY=xxx, GITHUB_TOKEN=xxx (optional)
go build -o agentic-blind-date .
./agentic-blind-date  # → http://localhost:8080
```

**Deployment:** Single binary + SQLite file. No external services beyond Mistral API.

---

## Strengths

✅ **Clean Architecture** – Well-separated concerns
✅ **Efficient AI Usage** – 3-phase matching reduces Mistral calls from O(n²) to O(5n)
✅ **Real-Time UX** – HTMX polling provides live updates
✅ **Progressive Enhancement** – Works on mobile and projector simultaneously
✅ **Resilient** – Fallback behavior at every AI touchpoint
✅ **Observable** – `/data/*` endpoints expose full state for debugging
✅ **Simple Deployment** – Single binary, auto-provisioned SQLite database
✅ **Fun & Engaging** – Animal emojis, color coding, humorous AI-generated content

---

## Potential Improvements

| Area | Suggestion |
|------|------------|
| Scalability | Rate limiting, batch GitHub fetches |
| Security | Admin authentication, session cookies |
| Reliability | Retry logic, circuit breakers |
| Analytics | Track match feedback, participation stats |
| Customization | Configurable questions, persona themes |
| Multi-Event | Concurrent events with separate namespaces |
| Persistence | Database backup/restore |

---

## File Structure

```
agentic_blind_date/
├── main.go              # Entry point, server, routing, .env loading
├── db.go                # SQLite: schema, Participant struct, queries
├── github.go            # GitHub REST API client
├── mistral.go           # Mistral chat completions client
├── agents.go            # AI pipeline: RunSetup, RunMatching
├── questions.go         # 5 fixed questions + constants
├── handlers.go          # All HTTP handlers
├── AGENTS.md            # Project instructions for coding agents
├── AGENT_OVERVIEW.md    # This document
├── .env.example
├── go.mod
├── go.sum
├── *tests.go            # Unit and integration tests
└── templates/
    ├── landing.html      # Registration form
    ├── onboard.html      # Pipeline progress container
    ├── wait.html         # Waiting room with persona preview
    ├── match.html        # Match results with flags/icebreakers
    ├── screen.html       # D3.js big screen visualization
    ├── admin.html        # Host controls
    ├── data.html         # Debug dashboard
    └── fragments.html    # HTMX partials
```
