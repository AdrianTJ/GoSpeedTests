package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/AdrianTJ/gospeedtest/internal/job"
	"github.com/AdrianTJ/gospeedtest/internal/store"
	"github.com/AdrianTJ/gospeedtest/internal/validator"
)

type Server struct {
	manager       *job.Manager
	store         store.Store
	apiKey        string
	allowInsecure bool
}

func NewServer(m *job.Manager, s store.Store, apiKey string, allowInsecure bool) *Server {
	return &Server{
		manager:       m,
		store:         s,
		apiKey:        apiKey,
		allowInsecure: allowInsecure,
	}
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.apiKey == "" {
			if s.allowInsecure {
				next.ServeHTTP(w, r)
				return
			}
			http.Error(w, "server misconfigured: GOST_API_KEY is required for this route (or set GOST_ALLOW_INSECURE=true for local testing)", http.StatusInternalServerError)
			return
		}

		key := r.Header.Get("X-API-Key")
		if key != s.apiKey {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// Public routes
	mux.HandleFunc("GET /v1/health", s.handleHealth)
	mux.HandleFunc("GET /v1/ready", s.handleReady)
	mux.HandleFunc("GET /openapi.yaml", s.handleOpenAPI)
	mux.HandleFunc("GET /docs", s.handleDocs)

	// Protected routes
	mux.Handle("GET /v1/history", s.authMiddleware(http.HandlerFunc(s.handleHistory)))
	mux.Handle("POST /v1/jobs", s.authMiddleware(http.HandlerFunc(s.handleCreateJob)))
	mux.Handle("GET /v1/jobs", s.authMiddleware(http.HandlerFunc(s.handleListJobs)))
	mux.Handle("GET /v1/jobs/{id}", s.authMiddleware(http.HandlerFunc(s.handleGetJob)))
	mux.Handle("DELETE /v1/jobs/{id}", s.authMiddleware(http.HandlerFunc(s.handleDeleteJob)))

	return mux
}

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "docs/openapi.yaml")
}

func (s *Server) handleDocs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>GoSpeedTest API Docs</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui.css" />
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui-bundle.js" crossorigin></script>
  <script>
    window.onload = () => {
      window.ui = SwaggerUIBundle({
        url: '/openapi.yaml',
        dom_id: '#swagger-ui',
      });
    };
  </script>
</body>
</html>
`)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if _, err := s.store.ListJobs(r.Context(), 1); err != nil {
		http.Error(w, "database not ready", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("READY"))
}

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL        string   `json:"url"`
		Tiers      []string `json:"tiers"`
		Runs       int      `json:"runs"`
		WebhookURL string   `json:"webhook_url"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.URL == "" {
		http.Error(w, "url is required", http.StatusBadRequest)
		return
	}

	if err := validator.ValidateURL(req.URL); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Runs <= 0 {
		req.Runs = 1
	}

	job, err := s.manager.Submit(r.Context(), req.URL, req.Tiers, req.Runs, req.WebhookURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"job_id": job.ID,
		"status": string(job.Status),
	})
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/v1/jobs/")
	if id == "" {
		http.Error(w, "missing job id", http.StatusBadRequest)
		return
	}

	job, err := s.store.GetJob(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if job == nil {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}

	results, _ := s.store.GetResultsByJobID(r.Context(), id)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"job_id":       job.ID,
		"status":       job.Status,
		"url":          job.URL,
		"created_at":   job.CreatedAt,
		"completed_at": job.CompletedAt,
		"error":        job.Error,
		"results":      results,
	})
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.store.ListJobs(r.Context(), 50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jobs)
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	url := r.URL.Query().Get("url")
	if url == "" {
		http.Error(w, "missing url parameter", http.StatusBadRequest)
		return
	}

	history, err := s.store.GetHistory(r.Context(), url)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history)
}

func (s *Server) handleDeleteJob(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/v1/jobs/")
	if id == "" {
		http.Error(w, "missing job id", http.StatusBadRequest)
		return
	}

	// First try to cancel it if it's pending
	_ = s.manager.CancelJob(r.Context(), id)

	if err := s.store.DeleteJob(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
