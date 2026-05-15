# 0005: Dependency Injection Strategy

## Status
Accepted

## Context

As the Agentic Blind Date application grows, we need a clear strategy for managing dependencies between components. The application currently has several components that depend on each other:

- `Handler` depends on `DB`, `AgentPipeline`
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
func NewGitHubClient(token string) *GitHubClient
func NewMistralClient(apiKey, model string, httpClient *http.Client) *MistralClient

// Business logic
func NewMatcher(github *GitHubClient, mistral *MistralClient) *Matcher
func NewAgentPipeline(db *DB, github *GitHubClient, mistral *MistralClient, matcher *Matcher) *AgentPipeline

// HTTP handlers
func NewHandler(db *DB, github *GitHubClient, mistral *MistralClient) *Handler
```

#### Composition Root

The `main()` function serves as the composition root where all dependencies are wired together:

```go
func main() {
    // Initialize external dependencies
    db, err := NewDB(dbPath)
    if err != nil {
        log.Fatal("DB init:", err)
    }
    defer db.Close()

    github := NewGitHubClient(os.Getenv("GITHUB_TOKEN"))
    mistral := NewMistralClient(
        os.Getenv("MISTRAL_API_KEY"),
        "mistral-medium-latest",
        &http.Client{Timeout: 30 * time.Second},
    )

    // Initialize application components
    matcher := NewMatcher(github, mistral)
    agents := NewAgentPipeline(db, github, mistral, matcher)
    h := NewHandler(db, github, mistral)

    // Start server
    log.Fatal(http.ListenAndServe(addr, buildMux(h)))
}
```

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
