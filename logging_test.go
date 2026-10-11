package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestParseLogLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"":        slog.LevelInfo,
		"info":    slog.LevelInfo,
		"INFO":    slog.LevelInfo,
		" debug ": slog.LevelDebug,
		"warn":    slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
		"bogus":   slog.LevelInfo,
	}
	for in, want := range cases {
		if got := parseLogLevel(in); got != want {
			t.Errorf("parseLogLevel(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestNewLogHandler_TextFormat(t *testing.T) {
	var buf bytes.Buffer
	l := slog.New(newLogHandler("text", slog.LevelInfo, &buf))

	l.Info("hello", "component", "onboarding")

	line := buf.String()
	for _, want := range []string{"level=INFO", "msg=hello", "component=onboarding"} {
		if !strings.Contains(line, want) {
			t.Errorf("text log %q missing %q", line, want)
		}
	}
}

func TestNewLogHandler_JSONFormat(t *testing.T) {
	var buf bytes.Buffer
	l := slog.New(newLogHandler("json", slog.LevelInfo, &buf))

	l.Error("boom", "err", "disk full")

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("json log %q is not JSON: %v", buf.String(), err)
	}
	if rec["level"] != "ERROR" || rec["msg"] != "boom" || rec["err"] != "disk full" {
		t.Errorf("json log fields wrong: %v", rec)
	}
}

func TestNewLogHandler_UnknownFormatMeansText(t *testing.T) {
	var buf bytes.Buffer
	l := slog.New(newLogHandler("xml", slog.LevelInfo, &buf))

	l.Info("hello")

	if strings.HasPrefix(strings.TrimSpace(buf.String()), "{") {
		t.Errorf("an unknown format should mean text, got %q", buf.String())
	}
}

func TestNewLogHandler_LevelFiltersLowerLevels(t *testing.T) {
	var buf bytes.Buffer
	l := slog.New(newLogHandler("text", slog.LevelWarn, &buf))

	l.Debug("quiet")
	l.Info("quiet")
	if buf.Len() != 0 {
		t.Errorf("debug and info should be filtered at warn, got %q", buf.String())
	}

	l.Warn("loud")
	if !strings.Contains(buf.String(), "loud") {
		t.Errorf("warn should pass at warn level, got %q", buf.String())
	}
}

func TestSetupLogging_InstallsTheConfiguredDefault(t *testing.T) {
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })

	var buf bytes.Buffer
	setupLogging(func(key string) string {
		return map[string]string{"LOG_LEVEL": "debug", "LOG_FORMAT": "json"}[key]
	}, &buf)

	slog.Debug("visible at debug")

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("default logger output %q is not JSON: %v", buf.String(), err)
	}
	if rec["msg"] != "visible at debug" {
		t.Errorf("want the debug line, got %v", rec)
	}
}

func TestLogger_TagsTheComponent(t *testing.T) {
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })

	var buf bytes.Buffer
	slog.SetDefault(slog.New(newLogHandler("text", slog.LevelInfo, &buf)))

	logger("onboarding").Error("participant not found", "participant", "p1")

	line := buf.String()
	for _, want := range []string{"level=ERROR", "component=onboarding", "participant=p1"} {
		if !strings.Contains(line, want) {
			t.Errorf("log line %q missing %q", line, want)
		}
	}
}
