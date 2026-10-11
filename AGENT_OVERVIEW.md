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
│  │  DB (SQLite) │  │ GitHub Client│  │  LLM Client  │  │  Handlers  │ │
│  │   (db.go)    │  │   (github.go)│  │   (llm.go)   │  │ (handlers.go)│ │
│  └──────────────┘  └──────────────┘  └──────────────┘  └────────────┘ │
│                              │                  │                   │
│                              ▼                  ▼                   ▼
│                    ┌─────────────────┐  ┌──────────┐  ┌──────────┐
│                    │  Onboarding &   │  │   D3.js  │  │  HTMX    │
│                    │  Matchmaking    │  │(screen)  │  │(templates)│
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
| **Database Layer** | `db.go` | SQLite schema, Participant type, change feed, activity log, LLM cache; the Participant store (`participantstore.go`) owns Participant SQL |
| **GitHub Client** | `github.go` | Fetches user profiles, repos, languages from GitHub API |
| **LLM Client** | `llm.go` | OpenAI-compatible chat completions adapter (`OpenAIClient`) for Mistral, Scaleway and Ollama |
| **Onboarding** | `onboarding.go` | Per-Participant preparation and Interview: GitHub fetch, questions, Persona, ready |
| **Matchmaking** | `matchmaking.go` | Continuous Matching and the admin Rematch; the Matcher (`matcher.go`) runs the 3-phase algorithm |
| **Fixed Questions** | `questions.go` | 5 predefined opinionated questions |
| **HTTP Handlers** | `handlers.go` | All route handlers, HTMX fragments, JSON endpoints |
| **Templates** | `templates/*.html` | Server-side rendered HTML with HTMX, Tailwind, D3.js |

---

## Workflow

### Participant Journey
1. **Landing** (`/user`) → Enter name + GitHub handle
2. **Onboarding** (`/user/onboard/{id}`) → Async AI pipeline runs in background
3. **Interview** → Answer 8 questions (5 fixed + 3 custom) via HTMX forms
4. **Waiting Room** (`/user/wait/{id}`) → See persona, profile summary, and a teaser of your future match (their emoji + colour + a disagreement hint)
5. **Match Reveal** (`/user/match/{id}`) → See your match's persona card (no handle) and score; the dossier (reason, flags, icebreakers) stays locked until you meet
6. **Social** (`/user/match/{id}` or `/user/wait/{id}`) → Meet people: your meet code + QR and "I met someone" during the social/show & tell; two-sided confirm completes a meeting and unlocks your dossier via push

### Admin Actions
- **Advance Phase** (`POST /admin/phase`) → Moves the event forward: `social` (the Big Reveal), `showtell`, `closed`
- **Trigger Reveal** (`POST /admin/reveal`) → Alias for advancing to the social phase
- **Reset Event** (`POST /admin/reset`) → Clears all data and returns to `onboarding`

### Big Screen
- **Projector View** (`/bigscreen`) → D3.js force-directed graph

---

## AI Pipeline

### Onboarding (Per Participant, Async)
```
GitHub Fetch Profile
    → Interview (Mistral) → Generate 3 Custom Questions
    → Store & Mark as "interviewing"
    → Personas (Mistral) → Generate Persona + Tagline
    → Mark as "ready" & hand to Continuous Matching
```

### Matchmaking (Continuous Matching + Admin Rematch)
The Matcher (`matcher.go`) decides who fits in three phases; Matchmaking (`matchmaking.go`) runs one matching operation at a time.

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
| GET | `/user/match/{id}` | Match reveal: persona card + locked dossier |
| GET | `/user/dossier/{id}` | HTMX fragment: the dossier (locked until the pair's meeting completes) |
| GET | `/user/match-stream/{id}` | SSE stream for the match page |
| GET | `/user/m/{id}` | Meet page (target's persona card + confirm) |
| POST | `/user/met/{id}` | Confirm a meeting (confirmer's id; target via `code` form value) |
| GET | `/user/meet-status/{id}` | HTMX fragment: meet section (code, QR, "I met someone") |
| GET | `/user/mission/{id}` | HTMX fragment: mission card section (3×3 grid, stamps, top connections) |
| GET | `/bigscreen` | Projector view with D3 graph |
| GET | `/bigscreen/graph-data` | JSON for D3 (polled 5s) |
| GET | `/admin` | Host control panel (phase, counts, transition buttons) |
| POST | `/admin/phase` | Advance the event to a later phase |
| POST | `/admin/reveal` | Reveal matches (alias for the social phase) |
| POST | `/admin/reset` | Clear all data |
| GET | `/data/meetings` | Debug: meetings (JSON) |
| GET | `/data/missioncard/{id}` | Debug: mission card (tiles, reasons, stamp state) |
| GET | `/data/*` | Debug endpoints (JSON) |

---

## Data Model

### SQLite Schema

**participants:**
```sql
id, github_handle, name, persona_name, persona_color, persona_symbol,
persona_tagline, profile_json, custom_questions, answers_json,
pipeline_step, matched_with, compat_score, compat_reason,
red_flags, green_flags, icebreakers, created_at, meet_code
```

**event_state:**
```sql
key ('phase'), value ('onboarding' | 'social' | 'showtell' | 'closed')
```

**meetings:**
```sql
participant_a, participant_b, a_confirmed_at, b_confirmed_at,
completed_at, origin ('match' | 'mission' | 'other'), created_at
-- PRIMARY KEY (participant_a, participant_b); participant_a is the
-- lexicographically lower id, one canonical row per pair
```

**mission_cards:**
```sql
participant_id (PRIMARY KEY), tiles (JSON: position, kind, person, reason), created_at
-- one card per participant, fixed at generation; stamps derive from meetings
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
├── llm.go               # OpenAI-compatible chat completions adapter (Mistral, Scaleway, Ollama)
├── onboarding.go        # Per-Participant pipeline: GitHub fetch, Interview, Persona
├── matchmaking.go       # Continuous Matching + admin Rematch
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
    ├── wait.html         # Waiting room with persona preview and match teaser
    ├── match.html        # Match reveal: persona card + locked dossier (unlocks on meeting)
    ├── screen.html       # D3.js big screen visualization
    ├── admin.html        # Host controls
    ├── data.html         # Debug dashboard
    └── fragments.html    # HTMX partials
```
