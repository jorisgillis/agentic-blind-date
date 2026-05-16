# 0006: Fallback Questions for LLM Failures

## Status
Accepted

## Context

The Agentic Blind Date application relies on Mistral LLM for two key features:
1. **Custom Question Generation**: Generating personalized interview questions based on a participant's GitHub profile
2. **Persona Generation**: Creating fun, anonymous personas for participants

When the Mistral API fails (e.g., 401 authentication error, rate limiting, network issues), the application needs to gracefully degrade to maintain a functional user experience. Without fallbacks, participants would be stuck or receive errors.

Currently, the application has two fallback mechanisms:
- For GitHub users when custom question generation fails: fall back to `ExtraQuestions`
- For persona generation failures: fall back to `generateFallbackPersonaFromCompleteProfile`

These fallbacks ensure that:
- All participants can complete the onboarding flow
- Non-GitHub users and GitHub users with LLM failures receive consistent question sets
- Personas are always generated, even when LLM calls fail

## Decision

We will maintain and document the current fallback strategy:

### Custom Question Generation Fallback
When `generateCustomQuestions` fails for a GitHub user:
1. Log the error with context
2. Return `ExtraQuestions` from questions.go
3. Continue with the interview flow using these fallback questions

This ensures GitHub users who experience LLM failures get the same questions as non-GitHub users, maintaining consistency.

### Persona Generation Fallback
When `generatePersonaFromCompleteProfile` fails:
1. Log the error with context
2. Call `generateFallbackPersonaFromCompleteProfile` with the same complete profile
3. Use the fallback persona for matching

The fallback persona uses deterministic logic based on available profile data:
- If ExtraAnswers has Languages: use first language for persona name
- If InterviewAnswers has fixed_1 (language): use that for persona name
- Otherwise: use "The Mysterious Coder"

## Consequences

### Good
- **Resilience**: Application continues to function during LLM outages
- **Consistency**: All participants without LLM-generated content receive the same experience
- **Graceful Degradation**: Users don't see errors, just a slightly less personalized experience
- **Observability**: Errors are logged for debugging

### Bad
- **Less Personalization**: Fallback questions and personas are less tailored to the individual
- **Potential Redundancy**: GitHub users may be asked about languages twice (in FixedQuestions and ExtraQuestions)
- **Silent Failures**: Users aren't notified when fallbacks are used (by design)

## Alternatives Considered

### Alternative 1: Show Error to User
Display an error message when LLM calls fail and ask the user to retry.
- **Pros**: Users are aware of the issue
- **Cons**: Poor user experience, breaks the flow
- **Decision**: Rejected; graceful degradation is more important

### Alternative 2: Use Hardcoded Fallback Questions
Keep the current hardcoded questions in agents.go instead of using ExtraQuestions.
- **Pros**: Simpler, no dependency on questions.go
- **Cons**: Inconsistent with non-GitHub user experience, harder to maintain
- **Decision**: Rejected; using ExtraQuestions provides consistency

### Alternative 3: Retry LLM Calls
Automatically retry failed LLM calls with exponential backoff.
- **Pros**: May succeed on retry
- **Cons**: Adds latency, may still fail, complex to implement
- **Decision**: Rejected for now; may revisit if LLM reliability improves

### Alternative 4: Cache Previous LLM Responses
Cache successful LLM responses and reuse them when LLM fails.
- **Pros**: Better than nothing
- **Cons**: Stale data, doesn't help first-time failures
- **Decision**: Rejected; covered by ADR 0002 (LLM Caching Strategy) for match scores, not applicable here

## Future Considerations

1. **User Notification**: Consider showing a subtle indicator when fallbacks are used (e.g., "We're experiencing high demand, using standard questions")
2. **Metrics**: Track fallback usage to monitor LLM reliability
3. **Improved Fallbacks**: Enhance fallback persona generation with more sophisticated deterministic logic
4. **Partial Fallbacks**: For partial LLM failures, use LLM for what works and fall back for the rest
