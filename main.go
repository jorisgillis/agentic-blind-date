package main

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func loadDotEnv() {
	data, err := os.ReadFile(".env")
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
	loadDotEnv()

	if os.Getenv("MISTRAL_API_KEY") == "" {
		log.Println("WARNING: MISTRAL_API_KEY not set — LLM calls will fail")
	}
	if os.Getenv("GITHUB_TOKEN") == "" {
		log.Println("WARNING: GITHUB_TOKEN not set — GitHub API limited to 60 req/hr")
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "blind_date.db"
	}
	db, err := initDB(dbPath)
	if err != nil {
		log.Fatal("DB init:", err)
	}
	defer db.Close()

	github := &GitHubClient{token: os.Getenv("GITHUB_TOKEN")}
	mistral := &MistralClient{
		apiKey:     os.Getenv("MISTRAL_API_KEY"),
		model:      "mistral-medium-latest",
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}

	h := newHandler(db, github, mistral)

	addr := "0.0.0.0:8080"
	log.Println("Starting Agentic Blind Date on http://" + addr)
	log.Fatal(http.ListenAndServe(addr, buildMux(h)))
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
	mux.HandleFunc("GET /user/match/{id}", h.Match)
	mux.HandleFunc("GET /user/explore/{myId}/{otherId}", h.Explore)

	mux.HandleFunc("GET /bigscreen", h.Screen)
	mux.HandleFunc("GET /bigscreen/state", h.ScreenState)
	mux.HandleFunc("GET /bigscreen/graph-data", h.GraphData)

	mux.HandleFunc("GET /data", h.DataIndex)
	mux.HandleFunc("GET /data/participants", h.DataParticipants)
	mux.HandleFunc("GET /data/participant/{id}", h.DataParticipant)
	mux.HandleFunc("DELETE /data/participant/{id}", h.DeleteParticipant)
	mux.HandleFunc("GET /data/activity", h.DataActivity)
	mux.HandleFunc("GET /data/state", h.DataState)

	mux.HandleFunc("GET /admin", h.Admin)
	mux.HandleFunc("POST /admin/reveal", h.TriggerReveal)
	mux.HandleFunc("POST /admin/reset", h.Reset)
	mux.HandleFunc("POST /admin/rematch", h.Rematch)

	return mux
}
