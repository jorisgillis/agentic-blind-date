# 0004: Big Screen Shows Top-3 Connections

## Status
Accepted

## Context
The big screen visualization is intended to show a rich network of connections between participants, making the event more engaging and helping participants identify potential matches beyond their assigned pair.

Currently, the implementation shows **top-2 connections** per participant (line 572 in handlers.go: `candidates[:2]`). However, the original design intent (as stated by the project owner) was to show **top-3 connections**.

The big screen graph visualization (`buildGraphPayload` in handlers.go) creates two types of edges:
1. **Matched edges**: Actual matches between participants (solid lines)
2. **Potential edges**: Top-N heuristic connections (dashed lines)

With only top-2 potential connections, the graph is less rich than intended. Showing top-3 creates a more interesting visualization with more connections to explore.

## Decision
We will update the big screen visualization to show **top-3 connections** per participant instead of top-2.

### Implementation
Change the constant in `handlers.go` line 572 from:
```go
candidates = candidates[:2]
```
to:
```go
candidates = candidates[:3]
```

Additionally, we will parameterize the `topNCandidates` function to make it more flexible for future changes.

### Graph Visualization
The big screen will show:
- **Nodes**: All participants as colored circles with emoji symbols
- **Matched edges**: Solid lines between actual matches
- **Potential edges**: Dashed lines between top-3 heuristic connections

This creates a richer, more connected graph that better demonstrates the potential connections in the room.

## Consequences

### Good
- **Richer visualization**: More connections make the graph more interesting to look at
- **Better discovery**: Participants can see more potential matches
- **Matches intent**: Aligns implementation with original design
- **Minimal change**: Simple constant change, low risk

### Bad
- **Visual clutter**: With more edges, the graph may become harder to read
- **Performance**: Slightly more computation (top-3 vs top-2), but negligible

## Alternatives Considered

### Alternative 1: Keep Top-2
Maintain the current top-2 connections.
- **Pros**: Simpler graph, less visual clutter
- **Cons**: Doesn't match original intent, less rich visualization
- **Decision**: Rejected; original intent was top-3

### Alternative 2: Make N Configurable
Allow the number of connections to be configured via environment variable or admin panel.
- **Pros**: Maximum flexibility
- **Cons**: Adds complexity, configuration to manage
- **Decision**: Rejected for now; top-3 is sufficient

### Alternative 3: Dynamic Based on Participant Count
Show more connections when there are more participants, fewer when there are fewer.
- **Pros**: Adapts to event size
- **Cons**: Complex logic, inconsistent experience
- **Decision**: Rejected; top-3 works well for typical event sizes

### Alternative 4: Show All Potential Connections
Show all heuristic connections without limiting to top-N.
- **Pros**: Complete graph
- **Cons**: Too many edges, unreadable visualization
- **Decision**: Rejected; filtering is necessary for readability

## Future Considerations

1. **Edge styling**: Consider different visual treatments for matched vs potential edges (color, thickness, etc.)
2. **Interactive graph**: Allow participants to explore the graph on their devices
3. **Connection strength**: Visualize edge weight based on heuristic score
4. **Group visualization**: For future group matching, visualize triples/quartets
