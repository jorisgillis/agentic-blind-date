# 0017: Structured, Leveled Server Logging

## Status
Accepted

## Context

Every failure in the app was logged with the standard library's unleveled `log.Printf` — 66 call sites across 11 files, all at the same priority, with context baked into the message string. There was no way to raise or lower the verbosity, no structure to filter or aggregate on, and no consistent way to tell which module a line came from. During the 2026-10-11 onboard-page diagnosis, the server's logs went to a terminal that could not be read back, and the only way to see a failure was to reproduce it while watching the process. The app needs a logging system that makes a failure pinpointable after the fact: what failed, in which module, at what severity, with what context.

## Decision

Adopt Go's standard `log/slog` — no third-party dependency. A small `logging.go` module configures and installs the default logger once in `main()`:

- `LOG_LEVEL` — the minimum level: `debug`, `info` (default), `warn`, `error`. Anything unrecognised means `info`.
- `LOG_FORMAT` — `text` (default, for a human at a terminal) or `json` (for containers and log collectors).
- Output goes to stderr, as before; the HTTP server's own error log is bridged into slog (`slog.NewLogLogger`), so handler panics arrive as leveled `error` lines too.
- Every line is tagged with the module writing it: call sites use `logger("onboarding").Error(...)`, which stamps a `component` attribute. The helper reads the default logger at call time, so it always picks up the configured handler.

Levels policy:

- **error** — an operation failed with no automatic recovery: a 500 to a participant, an aborted onboarding flow, a failed host action, a store failure.
- **warn** — the app recovered with a fallback or a degradation: GitHub/LLM fallbacks, cache errors, a fragment or stats section rendered without its data.
- **info** — the lifecycle the host watches: server start, a phase advance (from → to), an event reset.
- **debug** — per-participant detail, off by default: the onboarding pipeline's steps, meeting confirmations, matcher cache and follow-cache hits, mission card generation.

Privacy rules, unchanged in spirit: server logs are host-only. No contact channel values, no API keys or tokens, and no LLM prompts (they carry profile data) are ever logged. Participant ids and GitHub handles may appear, as they already did. The participant-facing activity feed (`LogActivity`, the big screen ticker) is a different thing — a product surface, not a server log — and stays as it is.

## Consequences

### Positive

1. One command answers "what failed": `LOG_LEVEL=error`, or grep `level=ERROR`.
2. `component=` pins a line to its module without reading the message.
3. `LOG_FORMAT=json` drops into any container log pipeline.
4. The levels policy makes severity a decision made once, not 66 times.
5. No new dependency; slog is stdlib since Go 1.21.

### Negative

1. slog call sites are slightly longer than `log.Printf` (message + key-value pairs).
2. `logger(component)` builds a tagged logger per call — negligible at this app's scale.
3. Tests and other binaries that never call `setupLogging` log through slog's default (info level, text) — fine for tests.

## Alternatives Considered

- **zerolog / zap / logrus** — third-party structured loggers. Rejected: a new dependency for what stdlib slog already does; the app has two.
- **Keep the standard `log` package** — unleveled and unstructured; the problem being solved.
- **A logging facade of our own** — reinvents slog's handler/level/attribute machinery for no gain.
- **Writing logs to a file or syslog** — an operations concern, solved by redirecting stderr (`./agentic-blind-date >> app.log 2>&1`) or the container runtime; the app stays stream-agnostic.

## Related

- ADR-0005 (dependency injection): configuration is read through an injected lookup, never the process environment directly, so it stays testable.
- ADR-0008 (quality assurance): the logging module ships with tests; the coverage gate covers it.
- Issue #124
