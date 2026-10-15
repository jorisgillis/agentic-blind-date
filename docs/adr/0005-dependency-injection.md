# 0005: Dependency Injection Strategy

## Status
Accepted

## Context

As the Agentic Blind Date application grows, we need a clear strategy for managing dependencies between components. The application currently has several components that depend on each other:

- `Handler` depends on `DB`, `AgentPipeline` (since replaced by `Onboarding` and `Matchmaking`)
- `AgentPipeline` depends on `DB`, `GitHubClient`, `MistralClient`, `Matcher`
- `Matcher` depends on `GitHubClient`, `MistralClient`
- `DB` wraps `*sql.DB`

Without a clear dependency injection strategy, the code can become tightly coupled, making it:
- Hard to test (difficult to substitute mock implementations)
- Hard to modify (changes ripple through the codebase)
- Hard to understand (dependencies are implicit)

## Decision

We will use **manual constructor injection** (the idiomatic Go approach) for dependency management. This means:

1. **Explicit constructors**: Each component has a constructor function (prefixed with `New`) that takes all its dependencies as parameters
2. **No frameworks**: We avoid third-party DI frameworks like Wire, dig, or fx
3. **Pass dependencies explicitly**: Dependencies are passed directly to constructors, not set via global variables or setters

### Implementation

#### Constructor Functions

All components will have explicit constructor functions:

```go
// Database layer
func NewDB(path string) (*DB, error)

// External service clients
func NewGitHubClient(token, baseURL string) *GitHubClient
func NewOpenAIClient(baseURL, apiKey, model string, jsonMode bool, httpClient *http.Client, clock Clock) *OpenAIClient

// Provider selection: reads the LLM_* settings through an injected lookup
// (never the process environment directly) and returns a configured LLM.
// NewGatedLLM wraps it in the one shared bound on concurrent LLM calls
// (ADR-0020).
func SelectLLM(lookup func(string) string) (LLM, error)
func NewGatedLLM(llm LLM, limit int) LLM

// Participant store: the only component other modules use to read or write Participants
func NewParticipantStore(db *DB) *ParticipantStore

// Event-state store: the only component other modules use to read or change the Event State
func NewEventStore(db *DB) *EventStore

// Domain modules: upstream services are accepted through the LLM and GitHubAPI seams;
// each builds its own stores from db rather than taking them as parameters
// (Meetings and Board take the event-state store as a parameter — see below)
func NewInterview(db *DB, llm LLM) *Interview
func NewMatcher(db *DB, github GitHubAPI, llm LLM) *Matcher
func NewRelationships(db *DB) *Relationships
func NewPersonas(llm LLM) *Personas
func NewMatchmaking(db *DB, matcher *Matcher, relations *Relationships, maxChain int) *Matchmaking
func NewOnboarding(db *DB, github GitHubAPI, interview *Interview, personas *Personas, matchmaking *Matchmaking, missions *Missions) *Onboarding
func NewMissions(db *DB, matcher *Matcher, llm LLM) *Missions
func NewShowTell(db *DB, llm LLM) *ShowTell
func NewSlideDecks(db *DB, dir string) *SlideDecks
func NewMeetings(db *DB, events *EventStore) *Meetings
func NewContactChannels(db *DB) *ContactChannels
func NewRecaps(db *DB) *Recaps
func NewBoard(db *DB, events *EventStore) *Board
func NewClosing(db *DB, meetings *Meetings, channels *ContactChannels, showtell *ShowTell) *Closing
func NewEvening(db *DB, missions *Missions, meetings *Meetings, slides *SlideDecks, matcher *Matcher) *Evening
func NewScreen(db *DB, matcher *Matcher, meetings *Meetings, showtell *ShowTell, closing *Closing) *Screen

// HTTP handler
func NewHandler(db *DB, onboarding *Onboarding, interview *Interview, matcher *Matcher, relations *Relationships, matchmaking *Matchmaking, missions *Missions, showtell *ShowTell, slides *SlideDecks, streams StreamObserver) *Handler
```

#### Composition Root

The `main()` function serves as the composition root where all dependencies are wired together. It owns the process-wide services (logging, the LLM, the database, GitHub) and the domain modules of the pipeline; the page-facing modules are wired inside `NewHandler`, a second composition point (below):

```go
func main() {
    loadDotEnv(".env")                   // a KEY=value file; never overrides what is set
    setupLogging(os.Getenv, os.Stderr)   // leveled slog (ADR-0017)

    llm, err := SelectLLM(os.Getenv)
    if err != nil {
        logger("main").Error("selecting the LLM provider", "err", err)
        os.Exit(1)
    }
    // One shared bound on concurrent LLM calls across the whole app, so a
    // rush of registrations queues instead of storming the provider (ADR-0020).
    llm = NewGatedLLM(llm, LLMConcurrency(os.Getenv))

    dbPath := os.Getenv("DB_PATH") // "blind_date.db" when unset
    db, err := NewDB(dbPath)
    defer db.Close()
    github := NewGitHubClient(os.Getenv("GITHUB_TOKEN"), GitHubBaseURL(os.Getenv))

    // The domain modules
    matcher := NewMatcher(db, github, llm)
    missions := NewMissions(db, matcher, llm)
    showtell := NewShowTell(db, llm)
    slides := NewSlideDecks(db, filepath.Join(filepath.Dir(dbPath), "slides"))
    slides.Sweep() // remove slide files no row points at
    interview := NewInterview(db, llm)
    relations := NewRelationships(db)
    matchmaking := NewMatchmaking(db, matcher, relations, defaultMaxChain)
    onboarding := NewOnboarding(db, github, interview, NewPersonas(llm), matchmaking, missions)
    onboarding.Resume() // pick up onboarding interrupted by a restart

    h := NewHandler(db, onboarding, interview, matcher, relations, matchmaking, missions, showtell, slides, nil)

    srv := &http.Server{Addr: addr, Handler: buildMux(h),
        ErrorLog: slog.NewLogLogger(logger("server").Handler(), slog.LevelError)}
    if err := srv.ListenAndServe(); err != nil { /* ... */ }
}
```

#### What NewHandler builds itself

`NewHandler` receives the domain modules `main()` built — `onboarding`, `interview`, `matcher`, `relations`, `matchmaking`, `missions`, `showtell`, `slides` — plus the `StreamObserver` (nil in production: nothing observes streams outside tests). Everything else the pages need it builds itself, from the `*DB` and the injected modules:

```go
func NewHandler(db *DB, onboarding *Onboarding, interview *Interview, matcher *Matcher,
    relations *Relationships, matchmaking *Matchmaking, missions *Missions,
    showtell *ShowTell, slides *SlideDecks, streams StreamObserver) *Handler {
    tmpl := template.Must(template.New("").Funcs(funcs).ParseGlob(filepath.Join("templates", "*.html")))
    events := NewEventStore(db)
    meetings := NewMeetings(db, events)
    channels := NewContactChannels(db)
    closing := NewClosing(db, meetings, channels, showtell)
    return &Handler{
        db: db, store: NewParticipantStore(db), events: events, meetings: meetings,
        evening:  NewEvening(db, missions, meetings, slides, matcher),
        channels: channels, recaps: NewRecaps(db), missions: missions, showtell: showtell,
        slides: slides, board: NewBoard(db, events), closing: closing,
        screen:     NewScreen(db, matcher, meetings, showtell, closing),
        onboarding: onboarding, interview: interview, matcher: matcher,
        relations: relations, matchmaking: matchmaking,
        tmpl: tmpl, streams: streams,
    }
}
```

So the handler owns the templates, its Participant store and event-state store for reads, `Meetings`, `ContactChannels`, `Recaps`, the `Board`, the `Evening` module (#148 — the host's phase transitions and reset, and every consequence they fire) and the `Screen` (the Big Screen's payload), with the `Closing` feeding the `Screen`. The module wave (#148–#156) kept the rule this ADR set: every module that needs a store builds it from the `*DB` it already receives rather than taking one as a parameter — `Evening`, `Screen` and `Closing` build their own event-state store (and `Screen` and `Closing` their own `Board`), and `Missions` builds its own `Meetings`; only `Meetings` and `Board` take the event-state store as a parameter, and the handler passes its own instance to both.

#### The card registry

The personal pages' card registry (ADR 0019, `cards.go`) is the one piece of wiring that is not a constructor: a package-level table with one entry per card — its placement on the wait and match page shells, the fragment that renders it, its data function and the signature source its push watches. The page data maps, the phase-content region and the streams' diff key derive from it (#154, #155), so adding a card is one registry entry plus one fragment. The final derivation step — deriving the page shells' own markup from the registry — was stopped at its escape clause because of three Go `html/template` blockers (the decision is recorded on #156), so the page shells' fragment includes and their JavaScript trigger lists stay hand-kept: the registry decides what each page shows and what its stream watches, while each shell still names its own includes and refresh targets.

#### Seams for upstream services

`LLM` (one method: `Chat(system, user)`) and `GitHubAPI` (`FetchProfile`, `CheckMutualFollow`) are the seams for upstream services. Each has two adapters: the production client (`OpenAIClient`, `GitHubClient`) and an in-memory fake used by tests, so the test suite never reaches the real APIs. `OpenAIClient` is one OpenAI-compatible chat completions adapter, configured here for Mistral. The LLM seam composes in `main()`: the provider `SelectLLM` returns is wrapped in `NewGatedLLM`, the one shared bound on concurrent LLM calls (ADR-0020), so every module's `llm` sits behind the same gate. Two small interfaces serve cross-cutting concerns: `Clock` (paces the retry wait; production sleeps for real, tests return at once) and `StreamObserver` (told each time an SSE stream looks at the current state, so a test can wait for a stream to catch up; production wires nil).

#### The Participant store

`ParticipantStore` (`participantstore.go`) is the only component other modules use to read or write Participants: a small, typed interface (`Get`, `All`, `AllByStep`, `GetByHandle`, `GetByMeetCode`, `Create`, `Change`, `StartInterview`) instead of one method per column. `DB` still holds the underlying SQL and scanning for those reads (`GetParticipant`, `GetAllParticipants`, ...) and for `Create`, but only the store calls them now — every module that reads or changes a Participant builds its own store from the `*DB` it already receives: the originals (`Relationships`, `Onboarding`, `Interview`, `Matchmaking`) and, since the module wave, also `Missions`, `Recaps`, `Closing` and `Screen`, with `Handler` doing the same for reads — `NewParticipantStore(db)` is called once per constructor, not threaded through as its own parameter. `DB` keeps the schema, the change feed, the activity log, the LLM cache and the aggregate counts (`ParticipantCount`, `ReadyCount`, `MatchedCount`) that the debug endpoints, the closing stats and the page signatures read; it no longer has per-column Participant setters (`SetProfile`, `SetPersona`, `AdvanceStep`, ... — removed, #53). This was an expand–migrate–contract refactor (#49–#53, see `docs/adr/` commit history): the store was added beside the old methods, every caller migrated across, and the old methods were deleted last.

#### The event-state store

`EventStore` (`eventstate.go`) owns the `event_state` table: `State` (anything unreadable counts as onboarding), `Advance` (the forward-only phase machine, ADR-0010, announcing the change on the change feed, so the streams refresh), `Reveal` (an alias for advancing to the Social) and `Reset`. Spec #51 moved Participant writes onto the Participant store and named the Event State as one of the remaining pieces, suggesting the Participant store as its home; what was built instead — and what this ADR now ratifies, closing the #53 divergence — is a dedicated store (#106): the Event State is not a Participant, so it gets its own typed operations rather than methods on the Participant store. `Reset` is the one operation that spans tables — now every table the event owns: Participants, activity, meetings, mission cards, contact channels, recap tokens, show & tell sign-ups and questions, slide decks, board posts and the Event State — and stays a single transaction on the store, so a failure leaves everything as it was. What the transaction cannot reach is sequenced by the `Evening` module (#148), built inside the handler: the reset sweeps the slide files and clears the LLM matcher cache, and a phase transition fires the ticker line, mission-card generation at the social and the champion at the close. The final store/database boundary is therefore: the Participant store owns Participant SQL, the event-state store owns event_state SQL, and `DB` keeps the schema, the change feed, the activity log and the LLM cache — it no longer carries any raw SQL for the Event State (`EventState`, `Reveal`, `setEventState` and `Reset` — removed, #106). Like the Participant store, the event-state store is built from the `*DB` a module already receives rather than threaded through as its own parameter: `Handler`, `Evening`, `Missions`, `Matchmaking`, `ShowTell` and `Screen` each call `NewEventStore(db)` inside their constructors (`Screen` and `Closing` also build their own `Board` this way, and `ContactChannels` its own `Meetings`); the two exceptions are `Meetings` and `Board`, which take the store as a parameter — the handler passes its own instance to both, and `Missions` shares one store between itself and the `Meetings` it builds.

## Consequences

### Positive

1. **Explicit dependencies**: It's always clear what a component depends on by looking at its constructor
2. **Easy to test**: Dependencies can be easily mocked or stubbed in tests
3. **No magic**: No reflection, no code generation, no runtime DI containers
4. **Compile-time safety**: Missing dependencies are caught at compile time
5. **Idiomatic Go**: Follows Go community best practices and conventions
6. **No external dependencies**: Doesn't require any third-party libraries
7. **Easy to understand**: New developers can quickly grasp the dependency flow

### Negative

1. **Boilerplate**: Constructor calls can become verbose for components with many dependencies
2. **Manual wiring**: Changes to dependencies may require updates in multiple places
3. **No automatic lifecycle**: Unlike frameworks like fx, we don't get automatic start/stop management

### Mitigations

For the negative consequences:

1. **Boilerplate**: Keep components focused with single responsibilities (Single Responsibility Principle)
2. **Manual wiring**: For complex applications, consider grouping related dependencies into structs
3. **Lifecycle**: Implement explicit `Start()` and `Stop()` methods on components that need lifecycle management

## Alternatives Considered

### 1. Google Wire (Compile-time DI)

**Pros**: Compile-time safety, code generation, good for complex dependency graphs
**Cons**: Requires code generation step, generated code can be hard to debug, project is archived (no longer maintained)
**Decision**: Rejected due to added complexity and maintenance status

### 2. Uber dig (Runtime DI with reflection)

**Pros**: No code generation, automatic dependency resolution, actively maintained
**Cons**: Uses reflection (runtime errors), slight runtime overhead, less explicit
**Decision**: Rejected due to runtime overhead and reduced explicitness

### 3. Facebook inject (Runtime DI with struct tags)

**Pros**: Simple API, struct tag-based configuration
**Cons**: Uses reflection, runtime errors for missing dependencies, project is archived
**Decision**: Rejected due to maintenance status and reflection

### 4. Uber fx (Full application framework)

**Pros**: Full application framework with lifecycle management, logging integration, actively maintained
**Cons**: More opinionated, more complex than just DI, uses reflection
**Decision**: Rejected due to complexity and opinionated nature

## Rationale

Manual constructor injection is the most idiomatic approach for Go and aligns with the language's philosophy of simplicity and explicitness. For a project of this size (small to medium web application), the benefits of explicit dependencies far outweigh the minor inconvenience of manual wiring.

The Go community generally prefers this approach, as evidenced by:
- The official Go blog post on Wire noting it's for "larger applications"
- Common advice in Go code reviews to pass dependencies explicitly
- The simplicity and testability of code using this pattern

As the project grows, if the dependency graph becomes unwieldy, we can revisit this decision and consider adopting Wire or a similar tool. However, for now, manual injection provides the best balance of simplicity, explicitness, and maintainability.

## Related

- [Go Blog: Compile-time Dependency Injection with Wire](https://go.dev/blog/wire)
- [Go Code Review Comments: Dependency Injection](https://go.dev/wiki/CodeReviewComments#initialisms)
- Issue #8: Apply dependency injection to reduce coupling
