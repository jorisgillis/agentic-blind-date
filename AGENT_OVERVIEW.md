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
| **Fixed Questions** | `questions.go` | 6 predefined opinionated questions |
| **HTTP Handlers** | `handlers.go` | All route handlers, HTMX fragments, JSON endpoints |
| **Closing Ceremony** | `closing.go` | The closing view's stats, the public event recap and the host export pack (personas and counts only) |
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
- **Export Pack** (`GET /admin/export`) → Download the post-meetup blast pack (JSON, or Markdown with `?format=md`) — personas and counts only
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
| AI | Mistral AI | `mistral-large-4` model |
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
| GET | `/user/settings/{id}` | HTMX fragment: contact-details settings section (channels off by default, github pre-filled) |
| POST | `/user/settings/{id}` | Save the contact channels (entered once) |
| GET | `/user/recap-link/{id}` | HTMX fragment: the recap link — the recap page as a QR during closed |
| GET | `/user/recap/{token}` | Personal recap page (persona card, dossier, connections, show & tell items, stats) — unguessable UUID token |
| POST | `/user/showtell/signup/{id}` | Sign up for show & tell (title, one-line description, optional repo link) — open during the social and show & tell |
| GET | `/user/showtell/{id}` | HTMX fragment: the show & tell section (sign-up form, queue place, status, question form, the asker's questions) |
| POST | `/user/showtell/question/{id}` | Ask the presenter on stage a question (through the asker's persona) — live during show & tell, needs no meeting, earns nothing |
| GET | `/user/slides/{id}` | HTMX fragment: the Slide Decks section (the speakers' PDFs, every phase) |
| GET | `/slides/{id}` | A Slide Deck's PDF, inline (public; `/slides/{id}/download` serves it as a download) |
| GET | `/recap` | Public event recap (stats, top connectors, persona gallery, show & tell list) — available only when closed, personas only |
| GET | `/bigscreen` | Projector view with D3 graph (phase-aware: meeting edges + connection leaderboard from the social on; presenter mode at show & tell; closing view with champion, stats and recap QR at closed) |
| GET | `/bigscreen/graph-data` | JSON for D3 (polled 5s) |
| GET | `/admin` | Host control panel (phase, counts, transition buttons) |
| GET | `/admin/slides` | Slide Decks page: upload form (PDF, title, speaker; max 100 MB) and the uploaded decks |
| POST | `/admin/slides` | Upload a Slide Deck (multipart); `/admin/slides/remove` (POST, `id`) takes one down |
| GET | `/admin/export` | Host export pack download (JSON default, `?format=md` for Markdown) — personas and counts only |
| POST | `/admin/phase` | Advance the event to a later phase |
| POST | `/admin/reveal` | Reveal matches (alias for the social phase) |
| POST | `/admin/reset` | Clear all data |
| POST | `/admin/showtell/stage` | Presenter mode: put a sign-up on stage (with the Hype Agent's intro), reveal the presenter's identity, or mark the talk done |
| POST | `/admin/showtell/move` | Reorder the show & tell queue (up/down) |
| POST | `/admin/showtell/remove` | Remove a sign-up from the queue |
| GET | `/admin/showtell-queue` | HTMX fragment: the show & tell queue (personas, status, reorder, remove, on stage, reveal, done) |
| POST | `/admin/showtell/question` | Mark an audience question asked (the Big Screen's current question), answered, or dismissed |
| GET | `/admin/showtell-questions` | HTMX fragment: the audience question queue (asker personas, status, actions) |
| GET | `/admin/showtell-stream` | SSE stream for the host console's queues |
| GET | `/data/meetings` | Debug: meetings (JSON) |
| GET | `/data/leaderboard` | Debug: connection ranking derived from completed meetings (JSON) |
| GET | `/data/channels` | Debug: per-participant opted-in contact-channel counts (JSON, counts only) |
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

**contact_channels:**
```sql
participant_id, channel ('github'|'email'|'linkedin'|'instagram'|'mastodon'|'other'),
value, enabled (0/1, off by default)
-- PRIMARY KEY (participant_id, channel); entered once in the settings section,
-- a value is revealed only under the mutual rule (opted in + completed meeting)
```

**recaps:**
```sql
participant_id (PRIMARY KEY), token (UUID, UNIQUE), created_at
-- one unguessable recap token per participant; the recap page persists across restarts
```

**showtell_signups:**
```sql
id, participant_id (UNIQUE, REFERENCES participants), title, description,
repo_url, status ('queued'|'on_stage'|'presented'), identity_revealed (0/1),
hype, position, created_at
-- one sign-up per participant; sign-up opens with the social; the host queue
-- reorders by position; the identity reveal (the host's explicit action) is
-- the only way a handle reaches the Big Screen's presenter card
```

**showtell_questions:**
```sql
id, participant_id (REFERENCES participants), signup_id (REFERENCES showtell_signups),
text, status ('queued'|'asked'|'answered'|'dismissed'), created_at
-- one row per audience question, bound to the on-stage sign-up at submission;
-- asked is the current question on the Big Screen (one at a time, personas
-- only); asking needs no meeting and earns nothing
```

**slide_decks:**
```sql
id, title, speaker, filename, stored_name (UNIQUE), size, created_at
-- a meet-up speaker's slides, uploaded by the Host; the PDF itself lives on the
-- data volume (slides/ next to the database) under stored_name, never the
-- uploaded filename; Reset deletes the rows and then sweeps the files
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
├── questions.go         # 6 fixed questions + extra questions + constants
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
    ├── wait.html         # Waiting room: persona, answers, wait status, mission, meet, show & tell + More drawer
    ├── match.html        # Match page: phase header, phase content, More drawer (dossier, settings, share QR, recap)
    ├── screen.html       # D3.js big screen visualization
    ├── admin.html        # Host controls
    ├── data.html         # Debug dashboard
    └── fragments.html    # HTMX partials
```
