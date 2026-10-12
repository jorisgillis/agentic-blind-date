package main

import (
	"log/slog"
	"net/http"
	"os"
	"strings"
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
			os.Setenv(key, val)
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

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "blind_date.db"
	}
	db, err := NewDB(dbPath)
	if err != nil {
		logger("main").Error("DB init", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	github := NewGitHubClient(os.Getenv("GITHUB_TOKEN"))

	matcher := NewMatcher(db, github, llm)
	missions := NewMissions(db, matcher, llm)
	showtell := NewShowTell(db, llm)
	interview := NewInterview(db, llm)
	relations := NewRelationships(db)
	matchmaking := NewMatchmaking(db, matcher, relations, defaultMaxChain)
	onboarding := NewOnboarding(db, github, interview, NewPersonas(llm), matchmaking, missions)

	onboarding.Resume() // pick up onboarding interrupted by a restart

	h := NewHandler(db, onboarding, interview, matcher, relations, matchmaking, missions, showtell, nil)

	addr := "0.0.0.0:8080"
	srv := &http.Server{
		Addr:    addr,
		Handler: buildMux(h),
		// Handler panics and other server errors arrive as leveled slog lines.
		ErrorLog: slog.NewLogLogger(logger("server").Handler(), slog.LevelError),
	}
	logger("main").Info("starting server", "addr", addr)
	if err := srv.ListenAndServe(); err != nil {
		logger("main").Error("server", "err", err)
		os.Exit(1)
	}
}

func buildMux(h *Handler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/user", http.StatusFound)
	})

	mux.HandleFunc("GET /user", h.Landing)
	mux.HandleFunc("POST /user/join", h.Join)
	mux.HandleFunc("GET /user/onboard/{id}", h.Onboard)
	mux.HandleFunc("GET /user/pipeline/{id}", h.PipelineStatus)
	mux.HandleFunc("POST /user/answer/{id}", h.SubmitAnswer)
	mux.HandleFunc("GET /user/wait/{id}", h.Wait)
	mux.HandleFunc("GET /user/wait-status/{id}", h.WaitStatus)
	mux.HandleFunc("GET /user/persona/{id}", h.PersonaCard)
	mux.HandleFunc("GET /user/phase-header/{id}", h.PhaseHeader)
	mux.HandleFunc("GET /user/phase-content/{id}", h.PhaseContent)
	mux.HandleFunc("GET /user/match/{id}", h.Match)
	mux.HandleFunc("GET /user/dossier/{id}", h.Dossier)
	mux.HandleFunc("GET /user/mission/{id}", h.MissionCard)
	mux.HandleFunc("GET /user/m/{id}", h.Meet)
	mux.HandleFunc("POST /user/met/{id}", h.ConfirmMeeting)
	mux.HandleFunc("GET /user/meet-status/{id}", h.MeetStatus)
	mux.HandleFunc("GET /user/match-stream/{id}", h.MatchStream)
	mux.HandleFunc("GET /user/settings/{id}", h.Settings)
	mux.HandleFunc("POST /user/settings/{id}", h.SaveSettings)
	mux.HandleFunc("GET /user/recap-link/{id}", h.RecapLink)
	mux.HandleFunc("GET /user/recap-content/{id}", h.RecapContent)
	mux.HandleFunc("GET /user/recap/{token}", h.Recap)
	mux.HandleFunc("POST /user/showtell/signup/{id}", h.ShowTellSignup)
	mux.HandleFunc("GET /user/showtell/{id}", h.ShowTellStatus)
	mux.HandleFunc("POST /user/showtell/question/{id}", h.ShowTellQuestion)

	mux.HandleFunc("GET /recap", h.EventRecap)
	mux.HandleFunc("GET /qr", h.QR)

	mux.HandleFunc("GET /bigscreen", h.Screen)
	mux.HandleFunc("GET /bigscreen/graph-data", h.GraphData)
	mux.HandleFunc("GET /bigscreen/stream", h.ScreenStream)

	mux.HandleFunc("GET /user/pipeline-stream/{id}", h.PipelineStream)
	mux.HandleFunc("GET /user/wait-stream/{id}", h.WaitStream)

	mux.HandleFunc("GET /data", h.DataIndex)
	mux.HandleFunc("GET /data/participants", h.DataParticipants)
	mux.HandleFunc("GET /data/participant/{id}", h.DataParticipant)
	mux.HandleFunc("DELETE /data/participant/{id}", h.DeleteParticipant)
	mux.HandleFunc("GET /data/activity", h.DataActivity)
	mux.HandleFunc("GET /data/state", h.DataState)
	mux.HandleFunc("GET /data/meetings", h.DataMeetings)
	mux.HandleFunc("GET /data/leaderboard", h.DataLeaderboard)
	mux.HandleFunc("GET /data/channels", h.DataChannels)
	mux.HandleFunc("GET /data/missioncard/{id}", h.DataMissionCard)

	mux.HandleFunc("GET /admin", h.Admin)
	mux.HandleFunc("POST /admin/phase", h.AdvancePhase)
	mux.HandleFunc("POST /admin/reveal", h.TriggerReveal)
	mux.HandleFunc("POST /admin/reset", h.Reset)
	mux.HandleFunc("POST /admin/rematch", h.Rematch)
	mux.HandleFunc("POST /admin/showtell/stage", h.ShowTellStage)
	mux.HandleFunc("POST /admin/showtell/move", h.ShowTellMove)
	mux.HandleFunc("POST /admin/showtell/remove", h.ShowTellRemove)
	mux.HandleFunc("POST /admin/showtell/question", h.ShowTellQuestionAction)
	mux.HandleFunc("GET /admin/showtell-queue", h.ShowTellQueue)
	mux.HandleFunc("GET /admin/showtell-questions", h.ShowTellQuestions)
	mux.HandleFunc("GET /admin/showtell-stream", h.ShowTellStream)
	mux.HandleFunc("GET /admin/export", h.AdminExport)

	return mux
}
