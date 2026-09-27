# Agentic Blind Date

A web app for tech meetups, built as a live demo of agentic AI. Participants
register with their GitHub handle, answer a short interview, and are matched
with their most compatible tech soulmate — revealed on a big screen.

The app is inspired by the TV show [Blind
Date](https://en.wikipedia.org/wiki/Blind_date): matching is based on
technical preferences and GitHub profiles rather than romance.

The admin chooses one of two **Event Formats** before the first Participant
registers (or after a Reset):

- **Classic flow** — each Participant goes through the interview on their own
  phone, and is matched into the pool as soon as they're ready (Continuous
  Matching).
- **Game Show** — the Big Screen is the stage and phones are controllers. A
  Host runs the room through Rounds from a host console, everyone is paired
  at once, and the Reveal plays pair by pair.

Both formats share registration, Personas, matching, Themes and the Reveal.

## Features

### Onboarding

Each Participant runs through a pipeline, visible in real time on their phone:

1. **Fetching GitHub** — pulls public profile data (languages, topics, top
   repos, follow graph), skipped for Non-GitHub Users
2. **Interviewing** — 11 questions total: 6 Fixed Questions everyone answers,
   plus 5 personalised ones (LLM-generated Custom Questions from the GitHub
   profile, or manual Extra Questions for Non-GitHub Users — also used to
   top up Custom Questions when the LLM returns fewer than 5 or fails)
3. **Creating persona** — an LLM-generated anonymous identity (a name, a
   tagline, a colour and an emoji), with a fallback named after the
   Participant's main language if the LLM fails
4. **Ready** — handed to Continuous Matching (Classic flow) or background
   assessment (Game Show, see below)

In a Game Show, the interview only asks the 5 personalised questions (the
**Lobby**); the 6 Fixed Questions become the show's **Rounds** instead.

### Matching

A three-phase algorithm: each Participant's top-5 candidates by a heuristic
score (shared languages, GitHub topics, project type, dev environment,
identical interview answers) become candidate Pairs; every candidate Pair
gets an LLM compatibility assessment (score, reason, red/green flags,
icebreakers), cached so no Pair is ever scored twice; Pairs are then taken
greedily by descending LLM score. A failed LLM call falls back to a default
assessment rather than blocking onboarding.

- **Classic flow:** Continuous Matching pairs a Participant the moment
  they're ready, breaking and re-forming a weaker existing Match if a better
  candidate arrives.
- **Game Show:** no one is paired before the matchmaking segment. Instead,
  becoming ready during the Lobby or a Round triggers background,
  throttled assessment of that Participant's candidate Pairs, so the cache
  is already warm by the time the Host starts matchmaking — which then
  pairs everyone at once, reading only the cache, making no new LLM calls.
  An odd Participant out becomes the **wildcard**, paired by Continuous
  Matching with the next arrival, in the Afterparty.

### The Game Show

- **Host console** (`/admin/host`) — a phone-friendly control panel: start
  the show, open the next Round (with a countdown, default 15s), close a
  Round early, start matchmaking, start the Reveal, skip to the next pair.
- **Rounds** — one Fixed Question at a time, answered by everyone on their
  phone: a swipe card for two-option questions, colour answer pads for
  3–4 options, free text otherwise. A closed Round shows a callout on the
  Big Screen: the majority answer and a minority "hot take".
- **Matchmaking segment** — a drumroll on the Big Screen while everyone is
  paired at once.
- **The Reveal** — plays pair by pair, a few seconds each, advancing on its
  own or with the Host's skip. Each pair's Personas light up on the Big
  Screen and both phones pop to the Match's colour with "Stand up!"; no
  handle or name appears before a pair's turn. The wildcard gets their own
  message instead.
- **The Afterparty** — after the Reveal, arrivals (and the wildcard) are
  matched by Continuous Matching and see their Match right away, same as
  the Classic flow.

### The Big Screen

`/bigscreen` is designed to run on a projector, pushed live over SSE (no
polling):

- A D3 force-directed graph of Persona nodes; dashed edges preview top-3
  potential connections before the Reveal, solid edges are confirmed Matches
- A QR code so latecomers can scan and join
- A live, persona-safe activity ticker — never a real name or handle before
  the Reveal
- In a Game Show: the current segment, Round countdown, round callout,
  matchmaking drumroll and the Reveal's current pair, all Theme-animated

### The match page

Once a Participant's Match is revealed (immediately in the Classic flow,
pair-by-pair in a Game Show):

- Their Match's Persona card, compatibility score and reason
- Green flags, red flags and tailored icebreakers
- "Find them in the room" — the Match's colour and emoji
- An "Others at the event" grid for broader networking (withheld during a
  Game Show's Reveal, until every pair has had its turn)

Any two ready Participants can also **Explore** their compatibility with
each other on demand, without needing to be matched.

### Themes

Every page styles itself from one of three **Themes** — Classic (the
default), Neon and Paper — named design tokens for colour, typography,
shape, motion and sound. A Participant picks their own on their phone,
remembered on the device; the admin picks the Big Screen's, which switches
live. Persona colours never change between Themes. Motion collapses under
`prefers-reduced-motion`; sound is a synthesised tone per Theme (Classic is
silent), off by default and always optional.

### Routes

| Path | Who | What |
|---|---|---|
| `GET /user` | Participant | Register |
| `GET /user/onboard/{id}` | Participant | Pipeline progress |
| `GET /user/wait/{id}` | Participant | Waiting room (Classic flow) |
| `GET /user/show/{id}` | Participant | Show status, Round answer pad (Game Show) |
| `GET /user/match/{id}` | Participant | Revealed Match |
| `GET /user/explore/{myId}/{otherId}` | Participant | On-demand compatibility with anyone else ready |
| `GET /bigscreen` | Projector | Live graph, ticker, Game Show stage |
| `GET /admin` | Host | Event Format, Big Screen Theme, Reveal, Reset, Rematch |
| `GET /admin/host` | Host | Game Show host console |
| `GET /data` | Debug | Inspect all Participants, activity log, Event State |

(`-stream`/`-status` variants push or poll updates for the pages above; see
`main.go` for the full route table.)

## Running

### Prerequisites

- Go 1.26+
- A C compiler (CGO is required for `github.com/mattn/go-sqlite3`)
- An LLM provider key ([Mistral AI](https://console.mistral.ai/) by default;
  see "Choosing an LLM provider" below)
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
./agentic-blind-date
# → Starting Agentic Blind Date on http://0.0.0.0:8080
```

The server loads `.env` automatically — no `export` step needed. Real
environment variables set before launch take precedence over `.env` values.

The database (`blind_date.db`, or `DB_PATH` if set) is created automatically
on first run.

### Environment variables

| Variable | Required | Description |
|---|---|---|
| `DB_PATH` | No | SQLite file path (default `blind_date.db`) |
| `LLM_PROVIDER` | No | `mistral` (default), `scaleway` or `ollama` |
| `LLM_API_KEY` | Depends on provider | See "Choosing an LLM provider" below |
| `LLM_BASE_URL` | No | Overrides the provider's default base URL |
| `LLM_MODEL` | No | Overrides the provider's default model |
| `LLM_TIMEOUT` | No | Overrides the provider's default timeout (Go duration, e.g. `45s`) |
| `LLM_JSON_MODE` | No | `true` (default) or `false` |
| `MISTRAL_API_KEY` | Legacy | Still honoured as the key when `LLM_PROVIDER` is mistral (or unset) and `LLM_API_KEY` is unset |
| `GITHUB_TOKEN` | No | GitHub PAT — increases rate limit from 60 to 5000 req/hr |

### Choosing an LLM provider

The organiser picks the provider for the whole deployment with `LLM_PROVIDER`.
Each provider works out of the box with sensible defaults; any of
`LLM_API_KEY`, `LLM_BASE_URL`, `LLM_MODEL`, `LLM_TIMEOUT` and `LLM_JSON_MODE`
overrides them.

| Provider | Base URL default | Key | Default model | Default timeout |
|---|---|---|---|---|
| `mistral` (default) | `https://api.mistral.ai/v1` | required (`LLM_API_KEY` or legacy `MISTRAL_API_KEY`) | `mistral-medium-latest` | 30s |
| `scaleway` | `https://api.scaleway.ai/v1` | required — the secret key of a Scaleway IAM API key | `mistral-small-3.2-24b-instruct-2506` | 30s |
| `ollama` | `http://localhost:11434/v1` | not needed | none — set `LLM_MODEL` yourself | 180s |

An unknown `LLM_PROVIDER` or a missing required key or model stops the app at
startup with a clear message, before the event.

#### Running against Ollama

Ollama has no universal default model, so pull one first and name it in
`LLM_MODEL`:

```bash
ollama pull llama3.1:8b
LLM_PROVIDER=ollama LLM_MODEL=llama3.1:8b ./agentic-blind-date
```

The 180s default timeout covers the first request, which can be slow while
the model loads. A model that hasn't been pulled fails with a clear "try
pulling it first" log message instead of being retried.

### Docker

```bash
docker build -t agentic-blind-date .
docker run -p 8080:8080 --env-file .env -v "$(pwd)/data:/data" -e DB_PATH=/data/blind_date.db agentic-blind-date
```

or `docker-compose up`. The image is a multi-stage Alpine build (CGO enabled
for SQLite3) running as a non-root user.

## Developing

### Tech stack

- **Go** (standard library `net/http`, no web framework) — one Go module,
  no other backend dependency besides `google/uuid` and
  `mattn/go-sqlite3`
- **SQLite3** via `mattn/go-sqlite3`, WAL mode (requires CGO)
- **HTMX + Server-Sent Events** — pages update by change-feed push (a
  `Watcher`-driven heartbeat plus catch-up signal), never by polling
- **D3.js** — the Big Screen's force-directed graph
- **Tailwind v4's browser build** — self-hosted and pinned, not the Play CDN
  (`docs/adr/0009-self-hosted-front-end-assets.md`); every front-end asset
  in `static/` is vendored at a pinned, checksummed version, so the app
  makes no third-party requests and works on an offline venue network
- **One OpenAI-compatible chat-completions adapter** behind a one-method LLM
  seam, configured for Mistral, Scaleway or Ollama by `LLM_PROVIDER`

### Project layout

```
main.go              composition root: wiring, routes, .env loading

db.go                SQLite schema/connection, Participant type, non-Participant queries
changes.go           the change feed SSE streams subscribe to
participantstore.go  the only way other modules read or write Participants
steps.go             Pipeline Step type and guarded transitions

github.go            GitHub public API client (the GitHubAPI seam)
llm.go               OpenAI-compatible chat completions adapter (the LLM seam)
llmprovider.go       provider selection from LLM_* settings
structured.go        AskStructured: one shared way to get parsed JSON from the LLM, with retries

questions.go         Fixed and Extra Question definitions
interview.go         Interview module: next question, submit answer, question set per format
persona.go           Persona module: name/tagline generation, anonymous fallback
matcher.go           Matcher: heuristic PairScore, LLM ScorePair, MatchPool/MatchNewcomer
relationships.go     Relationship module: the Key Invariant, pairing/unpairing
matchmaking.go       Matchmaking: Continuous Matching, Rematch, Game Show background assessment + matchmaking segment
onboarding.go        Onboarding: registration through ready

eventstate.go        Event State (before/after the Reveal) and Reveal
eventformat.go       Event Format (Classic flow / Game Show), locked once Participants exist
eventadmin.go        Event administration: Reveal, Reset, Rematch
show.go              Show module: Game Show segments, Rounds, countdowns, the Reveal

watcher.go           the SSE streams' heartbeat clock and catch-up signal
activity.go          Activity module: persona-safe ticker lines
bigscreen.go         Big Screen module: one Snapshot per change
navigation.go        the one destination() rule for every page and stream
views.go             typed page view structs

theme.go             the Theme catalogue (Classic, Neon, Paper): tokens and sound cues
theme_render.go      renders the catalogue as CSS, plus motion/reduced-motion CSS
theme_contrast.go    Persona-on-Theme contrast checking

handlers.go          HTTP handlers

templates/
  landing.html, onboard.html, wait.html, match.html, explore.html   Participant pages
  show-status.html, host-console.html                              Game Show pages
  screen.html                                                       /bigscreen
  admin.html, data.html                                             admin and debug
  fragments.html                                                    HTMX partials
  theme-head.html, theme-picker.html, theme-sound.html              shared Theme plumbing

static/    self-hosted, pinned front-end assets (see static/VENDORED.md)
docs/adr/  architecture decisions
docs/manual-checklists/  behaviour only a real browser can verify (motion, sound, gestures)
```

`CONTEXT.md` is the domain glossary (Participant, Persona, Event Format,
Show Segment, Round, Match, Relationship State, ...) — read it before
touching a module you don't recognise.

### Live reload during development

Templates load from disk at startup, so template changes need a server
restart. Use [`air`](https://github.com/air-verse/air) for automatic
rebuilds:

```bash
go install github.com/air-verse/air@latest
air
```

### Testing

```bash
go test -race -coverprofile=coverage.out .
sh scripts/check-coverage.sh
```

`docs/adr/0008-quality-assurance-approach.md` requires 100% test coverage
outside `main()` (the composition root); `scripts/check-coverage.sh` enforces
it from a coverage profile. `scripts/pre-commit.sh` also runs `gofmt`,
`go vet`/`staticcheck` and the full suite. `scripts/check-static-assets.sh`
verifies every vendored front-end file in `static/` still matches its pinned
checksum (ADR-0009).

A handful of things need a real browser to verify (an animation's feel, a
swipe gesture, sound on a real device) — those are tracked as manual
checklists under `docs/manual-checklists/`.

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

To simulate a run of the Classic flow:

```bash
# Register a participant
curl -i -X POST -d "name=Ada" -d "github=torvalds" http://localhost:8080/user/join
# → 303 to /user/onboard/{id}

# Wait a few seconds for GitHub fetch + persona/question prep, then check state
curl http://localhost:8080/data/participants

# Submit answers (replace {id} with the UUID from above; repeat until ready)
curl -i -X POST -d "answer=Tabs" http://localhost:8080/user/answer/{id}

# Trigger the reveal (after ≥2 participants are ready)
curl -X POST http://localhost:8080/admin/reveal

# Reset for a new run
curl -X POST http://localhost:8080/admin/reset
```

For a Game Show, set the Event Format first (`POST /admin/event-format`
with `format=game_show`, before anyone registers) and drive the Host
console's `POST /admin/host/advance` between Rounds instead of `/admin/reveal`.

### Database

SQLite3, WAL mode, four tables:

- **`participants`** — one row per person: GitHub handle, persona, Q&A
  answers, pipeline step, Match result, wildcard flag
- **`event_state`** — key/value store: the Event State (before/after
  Reveal), the Event Format, the Big Screen Theme, the Game Show's segment
- **`activity_log`** — append-only log of persona-safe events, shown on the
  Big Screen ticker
- **`llm_cache`** — every Pair's LLM assessment, keyed so it's never scored
  twice

To inspect directly:

```bash
sqlite3 blind_date.db "SELECT github_handle, persona_name, pipeline_step, compat_score FROM participants"
```

### How matching works

**Heuristic (`Matcher.PairScore`)**: +3 per shared GitHub language, +2 per
shared GitHub topic, +2 for a matching project type, +1 per shared dev
environment, +1 per identical interview answer. `RankCandidates` ranks every
other Participant against one by this score; the top 5 become candidate
Pairs (`candidatePairs`), capping LLM calls regardless of event size.

**LLM assessment (`Matcher.ScorePair`/`assess`)**: each candidate Pair gets
one LLM call (two running at a time), producing a score, a one-line reason,
red/green flags and icebreakers. Results are cached both in memory and in
`llm_cache`, so a Pair is never assessed twice; a failed call is never
cached, so the next call retries, and callers fall back to a default
assessment rather than blocking.

**Assignment**: `MatchPool` sorts assessed Pairs by score, descending, and
greedily assigns each Participant to their best still-available partner —
used by the admin's Rematch. `MatchNewcomer` does the same for one
Participant becoming ready (Continuous Matching), including breaking and
re-forming a weaker existing Match.

**In a Game Show**, `AssessCandidates` runs the same LLM assessment in the
background as each Participant becomes ready, populating the cache without
pairing anyone; `MatchPoolFromCache` then pairs everyone at the matchmaking
segment reading only that cache — falling back to the default assessment,
never calling the LLM live, so the segment makes no new calls.

## License

MIT. See [LICENSE](LICENSE).
