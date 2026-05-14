# 0001: Three-Phase Matching Algorithm

## Status
Accepted

## Context
The Agentic Blind Date application needs to match participants based on their GitHub profiles and interview answers. With N participants, a naive approach would require O(N²) LLM calls for pairwise scoring, which is prohibitively expensive and slow (32-82 minutes for 50 participants at 800ms-2s per call).

We need a matching algorithm that:
1. Produces high-quality matches based on rich profile data
2. Is performant enough for interactive use during an event
3. Is cost-effective given LLM API pricing
4. Adapts as new participants join (continuous matching)

## Decision
We will use a **three-phase matching algorithm** that balances quality, performance, and cost:

### Phase 1: Heuristic Filtering
For each participant, compute a fast heuristic score against all other participants and select the top-5 candidates. This reduces the candidate pool from O(N²) to O(N×5).

The heuristic score (`pairScore`) considers:
- Shared languages: +3 per match (strong signal of technical compatibility)
- Shared interview answers: +1 per match (alignment on preferences)
- Shared GitHub topics: +2 per match (shared technical interests)
- Shared project types: +2 if same (similar work domains)
- GitHub follow relationships: +5 if mutual, +3 if one-way (existing social connection)
- Shared dev environments: +1 per match (similar tooling preferences)

### Phase 2: LLM Scoring
For each candidate pair from Phase 1, invoke the Mistral LLM to compute a rich compatibility score. The LLM receives:
- Both participants' GitHub profile summaries
- Both participants' interview answers
- Both participants' interests (languages, tools, domains)
- GitHub follow relationship status

The LLM returns:
- Score: 0-100 compatibility rating
- Reason: A funny one-liner
- Green flags: Positive aspects
- Red flags: Potential concerns (humorous)
- Icebreakers: Conversation starters

### Phase 3: Greedy Assignment
Sort all scored candidate pairs by LLM score (descending) and assign pairs greedily:
1. Take the highest-scoring pair where both participants are unmatched
2. Mark both as matched
3. Repeat until no more pairs can be formed

For odd numbers of participants, the last unmatched participant remains unmatched (future: consider forming triples).

### Continuous Matching
Instead of batch matching, we use **continuous matching** that runs as each participant becomes ready:
1. When a new participant reaches `ready` state, find their top-5 candidates from the existing pool
2. Score these pairs with LLM
3. If the best candidate is already matched, break their existing match (if it's weaker than the new potential match)
4. Form the new match

This allows the system to adapt as new participants join and ensures participants don't have to wait for everyone to finish onboarding.

## Consequences

### Good
- **Performance**: Reduces LLM calls from O(N²) to O(N×5), making it feasible for interactive use
- **Quality**: LLM scoring considers rich profile data beyond what the heuristic can capture
- **Adaptability**: Continuous matching allows the system to respond to new participants dynamically
- **Cost**: Significantly reduces LLM API costs compared to naive pairwise scoring
- **Simplicity**: Greedy algorithm is simple to understand and implement

### Bad
- **Not optimal**: Greedy assignment doesn't guarantee globally optimal matching (but is good enough for this use case)
- **Heuristic misalignment**: If the heuristic filters out pairs that the LLM would score highly, those matches will never be considered
- **Breaking matches**: Continuous matching can break existing matches, which might be confusing for participants

## Alternatives Considered

### Alternative 1: Full LLM Pairwise Scoring
Score every possible pair with LLM, then use maximum weight matching algorithm.
- **Pros**: Guaranteed optimal matching, no heuristic misalignment
- **Cons**: O(N²) LLM calls, prohibitively expensive and slow (32-82 minutes for 50 participants)
- **Decision**: Rejected due to performance and cost

### Alternative 2: Two-Phase with Maximum Weight Matching
Use heuristic for filtering, then apply maximum weight matching (e.g., Hungarian algorithm) on the candidate graph.
- **Pros**: Better global optimality than greedy
- **Cons**: More complex to implement, still has heuristic misalignment issue
- **Decision**: Rejected for now; greedy is good enough for this use case

### Alternative 3: Clustering-Based Matching
Cluster participants by similarity, then match within clusters.
- **Pros**: Could produce interesting group matches
- **Cons**: Complex to implement, may not align with LLM scoring, harder to explain
- **Decision**: Rejected; may revisit for group matching in future

### Alternative 4: Random Sampling
Randomly sample pairs for LLM scoring instead of heuristic filtering.
- **Pros**: Simple, no heuristic to maintain
- **Cons**: Risk of missing good matches, not deterministic
- **Decision**: Rejected; heuristic provides better signal than random

## Future Considerations

1. **Cache LLM scores** across matching runs to avoid redundant calls
2. **Improve heuristic alignment** with LLM criteria by analyzing historical scoring data
3. **Consider group matching** (triples/quartets) for more social configurations
4. **Add circuit breakers** for LLM API to handle failures gracefully
5. **Implement proper matching algorithms** (stable marriage, maximum weight) if greedy proves insufficient
