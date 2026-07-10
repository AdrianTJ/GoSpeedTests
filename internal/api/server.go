package api

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/AdrianTJ/gospeedtest/internal/job"
	"github.com/AdrianTJ/gospeedtest/internal/store"
	"github.com/AdrianTJ/gospeedtest/internal/tier"
	"github.com/AdrianTJ/gospeedtest/internal/validator"
)

const (
	maxRequestBody      = 1 << 20 // 1 MiB cap on request bodies
	maxTimeoutS         = 600     // upper bound on a caller-supplied per-job timeout
	maxRuns             = 10      // upper bound on the runs parameter
	defaultJobListLimit = 50      // number of jobs GET /v1/jobs returns
)

// validateTiers rejects unknown tier names so a typo does not silently produce
// a COMPLETED job with no results. An empty list is allowed (defaults to network).
func validateTiers(tiers []string) error {
	for _, t := range tiers {
		if !tier.Valid(t) {
			return fmt.Errorf("invalid tier %q (allowed: %s)", t, strings.Join(tier.Supported, ", "))
		}
	}
	return nil
}

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
		if subtle.ConstantTimeCompare([]byte(key), []byte(s.apiKey)) != 1 {
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
		TimeoutS   int      `json:"timeout_s"`
		WebhookURL string   `json:"webhook_url"`
	}

	// Cap the request body so a large payload cannot exhaust memory.
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
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

	if err := validateTiers(req.Tiers); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.WebhookURL != "" {
		if err := validator.ValidateURL(req.WebhookURL); err != nil {
			http.Error(w, "invalid webhook_url: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	if req.Runs <= 0 {
		req.Runs = 1
	}

	if req.Runs > maxRuns {
		http.Error(w, fmt.Sprintf("runs parameter cannot exceed %d", maxRuns), http.StatusBadRequest)
		return
	}

	if req.TimeoutS < 0 || req.TimeoutS > maxTimeoutS {
		http.Error(w, fmt.Sprintf("timeout_s must be between 0 and %d", maxTimeoutS), http.StatusBadRequest)
		return
	}

	job, err := s.manager.SubmitWithTimeout(r.Context(), req.URL, req.Tiers, req.Runs, req.WebhookURL, req.TimeoutS)

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
	id := r.PathValue("id")
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

	results, err := s.store.GetResultsByJobID(r.Context(), id)
	if err != nil {
		slog.Error("Failed to fetch job results", "job_id", id, "error", err)
	}

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
	jobs, err := s.store.ListJobs(r.Context(), defaultJobListLimit)
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
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing job id", http.StatusBadRequest)
		return
	}

	// Refuse to delete a job that is actively running: deleting its row would
	// make the worker's in-flight SaveResult fail a foreign-key constraint and
	// silently drop the run's data.
	if job, err := s.store.GetJob(r.Context(), id); err == nil && job != nil && job.Status == store.StatusRunning {
		http.Error(w, "job is running; cannot delete until it finishes", http.StatusConflict)
		return
	}

	// Cancel it first if it's still pending.
	_ = s.manager.CancelJob(r.Context(), id)

	if err := s.store.DeleteJob(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
