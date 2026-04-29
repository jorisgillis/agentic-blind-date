# Agentic Blind Date

A web app for tech meetups, built as a live demo of agentic AI. Participants register with their GitHub handle, get interviewed by an AI pipeline, and are matched with their most compatible tech soulmate — then revealed all at once on a big screen.

The app is inspired by the TV show [Blind Date](https://en.wikipedia.org/wiki/Blind_date): matching is based on technical preferences and GitHub profiles rather than romance.

## Features

### The agent pipeline

Each participant runs through four AI agents in sequence, visible in real-time on their phone:

1. **GitHub Fetch Agent** — pulls public profile data: languages, top repos, bio, contribution stats
2. **Profile Agent** — uses Mistral to generate a funny anonymous persona ("The Grumpy Kernel King", "The YAML Wrangler") with a color-coded card
3. **Interviewer Agent** — asks 5 fixed opinionated questions (tabs vs spaces, deploy on Friday?, etc.) plus 3 questions tailored to the participant's GitHub profile
4. **Matchmaker Agent** — after the host triggers the reveal, pairs all participants by compatibility and generates a score, explanation, red/green flags, and conversation starters for each pair

### The big screen

`/bigscreen` is designed to run on a projector. It shows:
- Anonymous persona cards populating in real-time as people join
- A live activity ticker at the bottom ("🎭 Crafting persona for @torvalds...")
- A simultaneous flip of all cards at the reveal moment, showing names and compatibility scores

### Routes

| Path | Who | What |
|---|---|---|
| `/user` | Participants | Register, complete Q&A, wait for reveal, see match |
| `/bigscreen` | Projector | Live persona pool + simultaneous reveal |
| `/admin` | Host | Trigger reveal, reset event |
| `/data` | Debug | Inspect all participants, activity log, event state |

## Running

### Prerequisites

- Go 1.21+
- A [Mistral AI](https://console.mistral.ai/) API key
- A GitHub personal access token (optional, but avoids rate limiting)

### Setup

```bash
git clone <repo>
cd agentic-blind-date

# Copy and fill in your keys
cp .env.example .env
# Edit .env with your MISTRAL_API_KEY and GITHUB_TOKEN

go build -o agentic-blind-date .
```

### Start the server

```bash
export $(cat .env | xargs)
./agentic-blind-date
# → Starting Agentic Blind Date on http://localhost:8080
```

The database (`blind_date.db`) is created automatically on first run.

### Environment variables

| Variable | Required | Description |
|---|---|---|
| `MISTRAL_API_KEY` | Yes | Mistral AI API key |
| `GITHUB_TOKEN` | No | GitHub PAT — increases rate limit from 60 to 5000 req/hr |

## Developing

### Tech stack

- **Go** — standard library `net/http`, no web framework
- **HTMX** — all interactivity via server-side HTML fragments, no JavaScript written by hand
- **Tailwind CSS** — via Play CDN, no build step
- **SQLite3** — via `github.com/mattn/go-sqlite3` (requires CGO / a C compiler)
- **Mistral AI** — plain HTTP calls to `api.mistral.ai/v1/chat/completions`

### Project layout

```
main.go          server setup, route registration
db.go            SQLite schema, Participant type, all queries
github.go        GitHub public API client
mistral.go       Mistral chat completion client
agents.go        agent pipeline (RunSetup, RunMatching, greedy matching)
handlers.go      HTTP handlers + /data JSON endpoints
questions.go     fixed Q&A question definitions

templates/
  landing.html           /user — GitHub handle entry
  onboard.html           /user/onboard/{id} — pipeline progress
  wait.html              /user/wait/{id} — waiting room
  match.html             /user/match/{id} — match reveal
  screen.html            /bigscreen — projector view
  admin.html             /admin — host panel
  data.html              /data — debug overview
  fragments.html         HTMX partials (pipeline step, question, wait status, screen state)
```

### Live reload during development

The templates are loaded from disk at startup, so template changes require a server restart. Use [`air`](https://github.com/air-verse/air) for automatic rebuilds:

```bash
go install github.com/air-verse/air@latest
air
```

### Testing with curl

The `/data` endpoints expose the full application state as JSON:

```bash
# Event phase + participant counts
curl http://localhost:8080/data/state

# All participants with full field data
curl http://localhost:8080/data/participants

# Single participant by ID or GitHub handle
curl http://localhost:8080/data/participant/torvalds

# Recent activity log
curl http://localhost:8080/data/activity
```

To simulate a full run:

```bash
# Register a participant
curl -X POST -d "github=torvalds" http://localhost:8080/user/join
# → 303 to /user/onboard/{id}

# Wait ~8s for the pipeline, then check state
curl http://localhost:8080/data/participants

# Submit answers (replace {id} with the UUID from above)
for i in $(seq 1 8); do
  curl -s -D - -o /dev/null -X POST -d "answer=choice_$i" http://localhost:8080/user/answer/{id} \
    | grep -i hx-redirect
done

# Trigger the reveal (after ≥2 participants are ready)
curl -X POST http://localhost:8080/admin/reveal

# Wait ~15s for Mistral matching, then see results
curl http://localhost:8080/data/participants

# Reset for a new run
curl -X POST http://localhost:8080/admin/reset
```

### How matching works

Matching is a two-step process:

**Step 1 — Greedy pairing** (`pairScore` + `greedyMatch` in `agents.go`)

Before calling Mistral, the server scores every possible pair using a simple heuristic:
- +3 points per shared programming language
- +1 point per identical answer to the same question

All O(n²) pairs are scored, then sorted descending. A greedy sweep assigns each participant to their highest-scoring available partner. If the number of participants is odd, one person goes unmatched.

**Step 2 — Mistral scoring** (`generateMatch`)

Each confirmed pair gets a Mistral API call that produces:
- `score` — a 0–100 compatibility percentage
- `reason` — a one-sentence humorous explanation
- `red_flags` / `green_flags` — compatibility highlights
- `icebreakers` — conversation starters tailored to both profiles

**Why two steps?** `pairScore` runs in microseconds for 80 participants and ensures globally decent pairings. Mistral adds the personality, humor, and context that make the reveal moment fun — but is too slow (and expensive) to run for every possible combination.

### Database

SQLite3 with WAL mode. Three tables:

- **`participants`** — one row per person: GitHub handle, persona, Q&A answers, match result
- **`event_state`** — key/value store for the event phase (`onboarding` → `matching` → `revealed`)
- **`activity_log`** — append-only log of agent actions, shown on the big screen ticker

To inspect directly:

```bash
sqlite3 blind_date.db "SELECT github_handle, persona_name, pipeline_step, compat_score FROM participants"
```

### Mistral model

The model is set in `main.go`:

```go
mistral := &MistralClient{
    apiKey: os.Getenv("MISTRAL_API_KEY"),
    model:  "mistral-small-latest",
}
```

Swap to `mistral-large-latest` for more creative personas and match reasoning, at higher cost and latency.
