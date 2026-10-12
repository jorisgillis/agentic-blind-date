package main

import (
	"io"
	"log/slog"
	"strings"
)

// Server logs are leveled and structured (log/slog, ADR-0017). They are
// configured once in main from the environment and written to a stream:
//
//	LOG_LEVEL   the minimum level: debug | info (default) | warn | error
//	LOG_FORMAT  the output format: text (default) | json
//
// Every line is tagged with the module writing it, so a failure pins to its
// component: logger("onboarding").Error(...). Call sites never log contact
// channel values, API keys or LLM prompts (they carry profile data);
// participant ids and GitHub handles may appear — server logs are host-only.

// setupLogging installs the configured logger as slog's default, so every
// logger(component) call site logs through it. main calls this once, before
// anything can log.
func setupLogging(getenv func(string) string, w io.Writer) {
	slog.SetDefault(slog.New(newLogHandler(getenv("LOG_FORMAT"), parseLogLevel(getenv("LOG_LEVEL")), w)))
}

// parseLogLevel maps a LOG_LEVEL value to a slog.Level; anything unrecognised
// — including the empty string — means info, the default.
func parseLogLevel(v string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// newLogHandler builds the handler every log line goes through: JSON for
// machines (containers, log collectors), text for a human at a terminal.
func newLogHandler(format string, level slog.Level, w io.Writer) slog.Handler {
	opts := &slog.HandlerOptions{Level: level}
	if strings.EqualFold(strings.TrimSpace(format), "json") {
		return slog.NewJSONHandler(w, opts)
	}
	return slog.NewTextHandler(w, opts)
}

// logger returns the default logger tagged with the module writing the line.
// It reads the default at call time, so it always picks up setupLogging.
func logger(component string) *slog.Logger {
	return slog.Default().With("component", component)
}
