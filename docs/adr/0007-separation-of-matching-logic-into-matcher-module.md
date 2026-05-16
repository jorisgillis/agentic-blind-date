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
type Matcher struct {
    github  *GitHubClient
    mistral *MistralClient
}

func NewMatcher(github *GitHubClient, mistral *MistralClient) *Matcher
```

### Extracted Functions
1. **PairScore(matcher *Matcher, a, b *Participant) int** - Computes heuristic score between two participants
2. **Top5Candidates(matcher *Matcher, p *Participant, participants []*Participant) []*Participant** - Finds top 5 candidates
3. **CollectCandidatePairs(matcher *Matcher, participants []*Participant) []Pair** - Generates candidate pairs
4. **GreedyMatch(matcher *Matcher, participants []*Participant) map[string]string** - Greedy matching algorithm
5. **GenerateMatch(matcher *Matcher, participants []*Participant) (map[string]MatchResult, error)** - Full matching workflow

### Integration
- `Matcher` is created in the composition root (main.go)
- `Matcher` is injected into `AgentPipeline` via its constructor
- Handlers access matching functionality through `AgentPipeline.matcher`

## Consequences

### Good
- **Separation of Concerns**: Matching logic is now isolated from other components
- **Testability**: Matcher can be tested with mock GitHub and Mistral clients
- **Clarity**: The matching algorithm is now a cohesive, understandable module
- **Reusability**: Matcher can be used by other components without going through AgentPipeline
- **Explicit Dependencies**: Dependencies on GitHub and Mistral clients are explicit

### Bad
- **Indirection**: Some code now needs to access matching through `h.agents.matcher` instead of directly
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
