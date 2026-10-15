# 0020: Shared LLM Concurrency Bound

## Status

Accepted

## Context

The first live event (30 registrations in the first minutes) exposed a thundering herd. Onboarding starts a background goroutine per Participant, and each goroutine calls the LLM: one call to generate the Custom Questions, one to create the Persona, then up to five match assessments through Continuous Matching. Only some modules bounded their calls — the Matcher assesses two pairs at a time and the Mission Agent generates with a worker pool of four — but the onboarding goroutines were unbounded, so a rush of registrations fired dozens of parallel calls into the provider.

The provider rate-limited (HTTP 429), every call failed into retries with back-off, and the retry storm made the queue longer, not shorter. Two visible symptoms followed:

- **Every match scored 42%.** When an LLM match assessment fails, the Matcher falls back to a default assessment — until this change, a constant score of 42 for every failed pair. Under the storm, every assessment failed, so every participant saw the same number presented as their compatibility.
- **Long waits with no feedback.** Participants waited minutes on the onboarding page, which showed only a pulsing "Agent pipeline running...", and many gave up.

The per-module bounds were not wrong — they were simply not shared. Each module bounded its own calls; nothing bounded the whole app's calls against the provider.

## Decision

**One app-wide gate on concurrent LLM calls, applied once at the composition root.** `main` wraps the selected provider with `NewGatedLLM(llm, LLMConcurrency(os.Getenv))`: a semaphore (`gatedLLM`) through which every `Chat` call passes, no matter which module made it — onboarding's question generation and persona creation, the Matcher's assessments, the Mission Agent, the Hype Agent. The bound defaults to **4** and is configurable with `LLM_CONCURRENCY` (a malformed or non-positive value falls back to the default; below 1 is clamped to 1).

The per-module bounds stay: the Matcher's two-at-a-time pool and the Mission Agent's worker pool remain as inner limits, useful when the gate is raised. The gate is the ceiling under them all.

The failed-assessment fallback is fixed alongside, in the Matcher: the fallback score is derived from the PairScore heuristic mapped onto the 0-100 scale (30 for nothing shared, +2 per heuristic point, capped at 100) with a reason that says the score is an estimate — never a constant.

While waiting, the onboarding and wait pages show the Participant's position in the agent queue (how many Participants registered before them are still in an agent-driven step) and a ticking elapsed time, and the host console shows the queue length.

## Consequences

- **The herd becomes a queue.** Thirty registrations now produce at most 4 concurrent provider calls; the rest wait their turn in arrival order instead of failing into a retry storm. Match assessments stop failing wholesale, so the constant-score symptom disappears.
- **Honest fallbacks.** If the provider still fails a call, the pair gets a discriminating heuristic estimate with an honest reason — and because failed assessments are never cached, a later Rematch recomputes them with the LLM.
- **Feedback while waiting.** The queue position and elapsed time turn a silent wait into an explained one; the host sees the same queue on the console.
- **Lower peak throughput, deliberately.** Four concurrent calls is slower in aggregate than an unbounded burst would be on a healthy provider, but the burst was not healthy — it was the failure mode. The bound is tunable per deployment (`LLM_CONCURRENCY`).
- **In-memory only.** The gate holds no state beyond the semaphore; a restart simply resets the queue. Nothing about the bound is persisted, and nothing about a Participant's data changes.

## Alternatives Considered

- **(a) Per-module bounds only (status quo).** Each module limits itself, but the modules run concurrently — onboarding goroutines plus the Matcher's pool plus the Mission Agent's pool still sum past the provider's limit. Rejected: it is the failure mode.
- **(b) Bound only the onboarding goroutines.** Fixes the registration rush, but the Mission Agent's pool can still coincide with a matching run. Rejected: the herd has more than one door.
- **(c) Rely on retries and back-off alone.** The client already retries 429/5xx with exponential back-off; under a herd, synchronized retries make the queue worse (thundering retry herd). Rejected: retries are a safety net, not a queue.
- **(d) Provider-side queueing / a job queue with persistence.** A durable queue would survive restarts and give exact positions, but it is a much larger machine (workers, retries, failure states) for a single-evening app. Rejected on cost; the in-memory gate plus the DB-derived queue position covers the evening.
