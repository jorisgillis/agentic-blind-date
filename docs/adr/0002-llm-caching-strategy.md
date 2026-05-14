# 0002: LLM Response Caching Strategy

## Status
Accepted

## Context
The matching algorithm requires LLM scoring for candidate pairs. With N participants and top-5 candidates each, this requires ~N×5 LLM calls per matching run. For 50 participants, this is ~250 LLM calls per run. If matching is re-run (e.g., after a new participant joins, or manually triggered), the same pairs may be scored multiple times.

LLM calls are:
- Expensive (API costs)
- Slow (800ms-2s latency)
- Non-deterministic (though we use temperature=0.7, results can vary)

We need a caching strategy that:
1. Avoids redundant LLM calls for the same pairs
2. Persists across application restarts
3. Is invalidated when appropriate (e.g., event reset)
4. Is transparent to the matching algorithm

## Decision
We will implement a **two-level caching strategy** with the following design:

### Cache Structure

**In-Memory Cache**
- Type: `map[string]*matchResult` (Go map)
- Scope: Single application instance
- Lifetime: Duration of process
- Purpose: Fast access during a matching run

**Persistent Cache**
- Type: SQLite table `llm_cache`
- Scope: Cross-restart persistence
- Lifetime: Until explicit invalidation
- Purpose: Survival across application restarts

### Cache Key
- Format: `{aID}:{bID}` where aID < bID (sorted to ensure consistency)
- Example: `abc123:def456` (not `def456:abc123`)
- This ensures the same pair always has the same key regardless of order

### Cache Operations

**Get**
1. Check in-memory cache first
2. If miss, check SQLite cache
3. If SQLite hit, populate in-memory cache and return
4. If both miss, return cache miss

**Put**
1. Store in in-memory cache
2. Store in SQLite cache (best-effort, non-blocking)

**Clear**
1. Clear in-memory cache
2. Delete all entries from SQLite cache
3. Used when event is reset

### Cache Invalidation
- **Explicit**: On event reset (`Reset()` function)
- **Implicit**: None (cache entries don't expire)
- **Rationale**: For a single event, scores remain valid. For multi-event support (future), we would add event ID to cache key.

### Schema
```sql
CREATE TABLE llm_cache (
    pair_key TEXT PRIMARY KEY,  -- "aID:bID" format, sorted
    score INTEGER NOT NULL,
    reason TEXT NOT NULL,
    red_flags TEXT NOT NULL,    -- JSON array
    green_flags TEXT NOT NULL,  -- JSON array
    icebreakers TEXT NOT NULL,  -- JSON array
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
)
```

### Fallback Behavior
- Cache operations are **best-effort**
- If cache read fails, proceed with LLM call
- If cache write fails, continue (don't block matching)
- If LLM call fails, use `defaultMatchResult()` as before

## Consequences

### Good
- **Performance**: Eliminates redundant LLM calls across matching runs
- **Cost**: Reduces LLM API costs significantly
- **Resilience**: Persists across restarts, so matching can resume quickly
- **Transparency**: Matching algorithm doesn't need to change; cache is invisible
- **Simplicity**: Simple key-value interface, easy to understand

### Bad
- **Memory usage**: In-memory cache grows with number of pairs (but bounded by number of participants)
- **Stale data**: If participant profiles change, cached scores may be outdated (but profiles are immutable during an event)
- **Complexity**: Two-level cache adds some complexity to the codebase

## Alternatives Considered

### Alternative 1: In-Memory Only
Cache only in memory, no persistence.
- **Pros**: Simpler implementation
- **Cons**: Cache lost on restart, less resilient
- **Decision**: Rejected; persistence is valuable for event continuity

### Alternative 2: SQLite Only
Cache only in SQLite, no in-memory.
- **Pros**: Persistent, simpler code
- **Cons**: Slower access (database queries vs map lookups)
- **Decision**: Rejected; in-memory cache provides better performance

### Alternative 3: External Cache (Redis)
Use Redis or similar for caching.
- **Pros**: More scalable, shared across instances
- **Cons**: Additional infrastructure dependency, overkill for single-instance deployment
- **Decision**: Rejected; SQLite is sufficient for current scale

### Alternative 4: TTL-Based Invalidation
Add time-to-live to cache entries.
- **Pros**: Automatic invalidation of stale data
- **Cons**: Unnecessary complexity; profiles don't change during events
- **Decision**: Rejected; explicit invalidation on event reset is sufficient

### Alternative 5: No Caching
Don't cache at all, always call LLM.
- **Pros**: Simplest implementation, always fresh data
- **Cons**: Expensive, slow, doesn't scale
- **Decision**: Rejected; caching provides significant benefits

## Future Considerations

1. **Event-scoped caching**: Add event ID to cache key for multi-event support
2. **Cache statistics**: Track hit/miss rates for monitoring
3. **Cache warming**: Pre-populate cache for known participant pairs
4. **Selective caching**: Only cache scores above a certain threshold (but adds complexity)
5. **Distributed caching**: For multi-instance deployments, consider Redis or similar
