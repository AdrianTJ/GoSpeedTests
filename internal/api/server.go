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
	mux.HandleFunc("POST /v1/jobs", s.handleCreateJob)
	mux.HandleFunc("GET /v1/jobs/", s.handleGetJob)

	return s.authMiddleware(mux)
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
