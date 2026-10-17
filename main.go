package main

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// loadDotEnv sets environment variables from a KEY=value file, without
// overriding variables that are already set. A missing file is ignored.
func loadDotEnv(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 1 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
			val = val[1 : len(val)-1]
		}
		if os.Getenv(key) == "" {
			// A key Setenv rejects (an embedded NUL, say) is simply left
			// unset, exactly like a line that is not there.
			_ = os.Setenv(key, val)
		}
	}
}

func main() {
	loadDotEnv(".env")
	setupLogging(os.Getenv, os.Stderr)

	if os.Getenv("GITHUB_TOKEN") == "" {
		logger("main").Warn("GITHUB_TOKEN not set — GitHub API limited to 60 req/hr")
	}

	llm, err := SelectLLM(os.Getenv)
	if err != nil {
		logger("main").Error("selecting the LLM provider", "err", err)
		os.Exit(1)
	}
	// One shared bound on concurrent LLM calls across the whole app, so a
	// rush of registrations queues instead of storming the provider.
	llm = NewGatedLLM(llm, LLMConcurrency(os.Getenv))

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "blind_date.db"
	}
	db, err := NewDB(dbPath)
	if err != nil {
		logger("main").Error("DB init", "err", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }() // process exit is next; the close error cannot matter

	github := NewGitHubClient(os.Getenv("GITHUB_TOKEN"), GitHubBaseURL(os.Getenv))

	matcher := NewMatcher(db, github, llm)
	missions := NewMissions(db, matcher, llm)
	showtell := NewShowTell(db, llm)
	slides := NewSlideDecks(db, filepath.Join(filepath.Dir(dbPath), "slides"))
	if err := slides.Sweep(); err != nil {
		logger("main").Warn("sweeping orphan slide files", "err", err)
	}
	interview := NewInterview(db, llm)
	relations := NewRelationships(db)
	matchmaking := NewMatchmaking(db, matcher, relations, defaultMaxChain)
	onboarding := NewOnboarding(db, github, interview, NewPersonas(llm), matchmaking, missions)

	onboarding.Resume() // pick up onboarding interrupted by a restart

	h := NewHandler(db, onboarding, interview, matcher, relations, matchmaking, missions, showtell, slides, nil)

	addr := "0.0.0.0:8080"
	if port := os.Getenv("PORT"); port != "" {
		addr = "0.0.0.0:" + port
	}
	srv := &http.Server{
		Addr:    addr,
		Handler: buildMux(h),
		// A stalled client must not hold a connection open forever (G112).
		ReadHeaderTimeout: 10 * time.Second,
		// Handler panics and other server errors arrive as leveled slog lines.
		ErrorLog: slog.NewLogLogger(logger("server").Handler(), slog.LevelError),
	}
	logger("main").Info("starting server", "addr", addr)
	if err := srv.ListenAndServe(); err != nil {
		logger("main").Error("server", "err", err)
		os.Exit(1)
	}
}

// buildMux registers every route in the census table (ADR-0022) — the table
// in routes.go is the single registration site; a route cannot exist outside
// it, and the census test fails on any drift between the two.
func buildMux(h *Handler) http.Handler {
	mux := http.NewServeMux()
	for _, rt := range routes {
		mux.HandleFunc(rt.Pattern, func(w http.ResponseWriter, r *http.Request) {
			rt.Handler(h, w, r)
		})
	}
	return mux
}
