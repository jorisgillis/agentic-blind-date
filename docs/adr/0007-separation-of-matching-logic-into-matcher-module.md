# 0007: Separation of Matching Logic into Matcher Module

## Status
Accepted

## Context

As the Agentic Blind Date application grew, the matching logic became tightly coupled with the agent pipeline and handlers. The matching-related functions were scattered across the codebase, making them:
- Hard to test in isolation
- Hard to understand as a cohesive system
- Hard to modify without affecting other components
- Tightly coupled to external dependencies (GitHub, Mistral clients)

The matching logic included:
- `pairScore()` - Heuristic scoring between two participants
- `Top5Candidates()` - Finding top candidates for a participant
- `CollectCandidatePairs()` - Generating all candidate pairs
- `GreedyMatch()` - Greedy matching algorithm
- `GenerateMatch()` - Full matching workflow

These functions had implicit dependencies on each other and on external services, making the code difficult to maintain and test.

## Decision

We extracted all matching logic into a dedicated `Matcher` module (matcher.go) with the following design:

### Module Structure
```go
type Matcher struct { /* db, github GitHubAPI, mistral LLM, in-memory cache */ }

func NewMatcher(db *DB, github GitHubAPI, mistral LLM) *Matcher
```

### Interface
The Matcher is a deep module: callers see five operations, and the three-phase
algorithm (ADR-0001), the Match prompt, reply validation and the two-level cache
(ADR-0002) all sit behind them.

1. **MatchPool(participants) []Match**: runs the three-phase algorithm and returns disjoint Matches
2. **MatchNewcomer(newcomer, others) *Match**: Continuous Matching; prefers unmatched Participants, otherwise takes over a Match the newcomer beats
3. **ScorePair(a, b) (*matchResult, error)**: the cached LLM assessment of one Pair (used by Explore)
4. **PairScore(a, b) int**: the heuristic score (used by the Big Screen's top connections)
5. **ClearCache()**: forgets every cached assessment (event reset)

Candidate selection and greedy assignment are internal (`topCandidates`,
`candidatePairs`, `greedyMatch`) and are tested through `MatchPool`.

### Integration
- `Matcher` is created in the composition root (main.go)
- `Matcher` is injected into `Matchmaking` and `Handler` via their constructors
- `Matchmaking` serialises Rematch and Continuous Matching and stores the Matches the Matcher returns through the Relationship module; it holds no matching logic

> Updated after the deepening in the architecture review (2026-09): the original
> extraction left the algorithm, cache and a duplicate Match prompt in
> `AgentPipeline`. Tests use in-memory fakes for the `LLM` and `GitHubAPI` seams (ADR-0005).

## Consequences

### Good
- **Separation of Concerns**: Matching logic is now isolated from other components
- **Testability**: Matcher is tested through its interface with fake `LLM` and `GitHubAPI` adapters
- **Clarity**: The matching algorithm is now a cohesive, understandable module
- **Reusability**: Matcher can be used by other components without going through AgentPipeline
- **Explicit Dependencies**: Dependencies on GitHub and Mistral clients are explicit

### Bad
- **Learning Curve**: Developers need to understand the new module structure
- **Refactoring Effort**: Required changes to existing code that used matching functions

## Alternatives Considered

### Alternative 1: Keep Logic in AgentPipeline
Leave matching logic inside AgentPipeline as methods.
- **Pros**: Less refactoring, simpler structure
- **Cons**: AgentPipeline becomes a god object, harder to test matching in isolation
- **Decision**: Rejected; separation improves maintainability

### Alternative 2: Extract to Separate Package
Create a separate Go package for matching logic.
- **Pros**: Stronger encapsulation, clearer boundaries
- **Cons**: Overkill for current size, adds import complexity
- **Decision**: Rejected; single package is sufficient for now

### Alternative 3: Use Interface for Matcher
Define a Matcher interface and use it for dependency injection.
- **Pros**: More flexible, easier to mock
- **Cons**: More boilerplate, interface may evolve frequently
- **Decision**: Rejected; concrete type is simpler for current needs

## Future Considerations

1. **Interface Extraction**: If we need multiple matching strategies, consider defining a Matcher interface
2. **Algorithm Swapping**: The current greedy algorithm could be replaced with more sophisticated matching (stable marriage, maximum weight)
3. **Performance Optimization**: Profile and optimize matching for large participant counts
4. **Distributed Matching**: For very large events, consider distributed matching across multiple instances
