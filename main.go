package main

import (
	"log"
	"net/http"
	"os"
	"strings"
)

func loadDotEnv() {
	data, err := os.ReadFile(".env")
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
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

	db, err := initDB("blind_date.db")
	if err != nil {
		log.Fatal("DB init:", err)
	}
	defer db.Close()

	github := &GitHubClient{token: os.Getenv("GITHUB_TOKEN")}
	mistral := &MistralClient{
		apiKey: os.Getenv("MISTRAL_API_KEY"),
		model:  "mistral-small-latest",
	}

	h := newHandler(db, github, mistral)

	mux := http.NewServeMux()

	// Root redirect to user landing
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/user", http.StatusFound)
	})

	// --- /user — participant flow ---
	mux.HandleFunc("GET /user", h.Landing)
	mux.HandleFunc("POST /user/join", h.Join)
	mux.HandleFunc("GET /user/onboard/{id}", h.Onboard)
	mux.HandleFunc("GET /user/pipeline/{id}", h.PipelineStatus)
	mux.HandleFunc("POST /user/answer/{id}", h.SubmitAnswer)
	mux.HandleFunc("GET /user/wait/{id}", h.Wait)
	mux.HandleFunc("GET /user/wait-status/{id}", h.WaitStatus)
	mux.HandleFunc("GET /user/match/{id}", h.Match)

	// --- /bigscreen — projector view ---
	mux.HandleFunc("GET /bigscreen", h.Screen)
	mux.HandleFunc("GET /bigscreen/state", h.ScreenState)
	mux.HandleFunc("GET /bigscreen/graph-data", h.GraphData)

	// --- /data — debug / testing (open for now) ---
	mux.HandleFunc("GET /data", h.DataIndex)
	mux.HandleFunc("GET /data/participants", h.DataParticipants)
	mux.HandleFunc("GET /data/participant/{id}", h.DataParticipant)
	mux.HandleFunc("GET /data/activity", h.DataActivity)
	mux.HandleFunc("GET /data/state", h.DataState)

	// --- /admin — host panel ---
	mux.HandleFunc("GET /admin", h.Admin)
	mux.HandleFunc("POST /admin/reveal", h.TriggerReveal)
	mux.HandleFunc("POST /admin/reset", h.Reset)

	addr := ":8080"
	log.Println("Starting Agentic Blind Date on http://localhost" + addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
