package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/AdrianTJ/gospeedtest/internal/job"
	"github.com/AdrianTJ/gospeedtest/internal/store"
)

type Server struct {
	manager *job.Manager
	store   store.Store
	apiKey  string
}

func NewServer(m *job.Manager, s store.Store, apiKey string) *Server {
	return &Server{
		manager: m,
		store:   s,
		apiKey:  apiKey,
	}
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.apiKey == "" {
			next.ServeHTTP(w, r)
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
	mux.HandleFunc("GET /v1/health", s.handleHealth)
	mux.HandleFunc("GET /v1/ready", s.handleReady)
	mux.HandleFunc("POST /v1/jobs", s.handleCreateJob)
	mux.HandleFunc("GET /v1/jobs", s.handleListJobs) // Exact match for listing
	mux.HandleFunc("GET /v1/jobs/", s.handleGetJob)  // Prefix match for ID

	return s.authMiddleware(mux)
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

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	// Simple check: can we talk to the store?
	if _, err := s.store.ListJobs(r.Context(), 1); err != nil {
		http.Error(w, "store not ready", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ready"}`))
}

type CreateJobRequest struct {
	URL        string            `json:"url"`
	Tiers      []string          `json:"tiers"`
	Runs       int               `json:"runs"`
	TimeoutS   int               `json:"timeout_s"`
	Tags       map[string]string `json:"tags"`
	WebhookURL string            `json:"webhook_url"`
}

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	var req CreateJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.URL == "" {
		http.Error(w, "url is required", http.StatusBadRequest)
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
	json.NewEncoder(w).Encode(map[string]interface{}{
		"job_id":   job.ID,
		"status":   job.Status,
		"poll_url": "/v1/jobs/" + job.ID,
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

	results, err := s.store.GetResultsByJobID(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
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
