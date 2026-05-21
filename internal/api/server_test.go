package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AdrianTJ/gospeedtest/internal/job"
	"github.com/AdrianTJ/gospeedtest/internal/store"
)


func TestAPIServer(t *testing.T) {
	// Setup
	tmpDir, _ := os.MkdirTemp("", "api-server-test")
	defer os.RemoveAll(tmpDir)
	dbPath := filepath.Join(tmpDir, "test.db")
	s, _ := store.NewStore(dbPath)
	defer s.Close()

	m := job.NewManager(s, 1, 10, "")
	m.Start()
	defer m.Stop()

	srv := NewServer(m, s, "", true) // Use insecure mode for base tests
	mux := srv.Routes()

	// 1. Test POST /v1/jobs
	reqBody := map[string]interface{}{
		"url":   "http://example.com",
		"runs":  1,
		"tiers": []string{"network"},
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/v1/jobs", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected status 202, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	jobID := resp["job_id"].(string)

	if jobID == "" {
		t.Fatal("expected job_id in response")
	}

	// 2. Test GET /v1/jobs/{id}
	// Wait a bit for the worker to pick it up (though we don't strictly need it to finish for GET to work)
	time.Sleep(100 * time.Millisecond)

	req = httptest.NewRequest("GET", "/v1/jobs/"+jobID, nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var getResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &getResp)
	if getResp["job_id"] != jobID {
		t.Errorf("expected job_id %s, got %s", jobID, getResp["job_id"])
	}
	if getResp["url"] != "http://example.com" {
		t.Errorf("expected url http://example.com, got %s", getResp["url"])
	}

	// 3. Test Authentication (with an API key)
	srvWithAuth := NewServer(m, s, "secret-key", false)
	muxWithAuth := srvWithAuth.Routes()

	// 3a. Unauthorized request
	req = httptest.NewRequest("GET", "/v1/jobs/"+jobID, nil)
	w = httptest.NewRecorder()
	muxWithAuth.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", w.Code)
	}

	// 3b. Authorized request
	req = httptest.NewRequest("GET", "/v1/jobs/"+jobID, nil)
	req.Header.Set("X-API-Key", "secret-key")
	w = httptest.NewRecorder()
	muxWithAuth.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK with valid API key, got %d", w.Code)
	}

	// 4. Test Health & Ready
	req = httptest.NewRequest("GET", "/v1/health", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected health 200, got %d", w.Code)
	}

	req = httptest.NewRequest("GET", "/v1/ready", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected ready 200, got %d", w.Code)
	}

	// 5. Test List Jobs
	req = httptest.NewRequest("GET", "/v1/jobs", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected list jobs 200, got %d", w.Code)
	}
	var jobs []store.Job
	if err := json.Unmarshal(w.Body.Bytes(), &jobs); err != nil {
		t.Fatalf("failed to unmarshal jobs list: %v", err)
	}
	if len(jobs) < 1 {
		t.Error("expected at least 1 job in list")
	}

	// 6. Test Edge Cases
	// 6a. POST without URL
	req = httptest.NewRequest("POST", "/v1/jobs", bytes.NewReader([]byte(`{"runs": 1}`)))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for missing URL, got %d", w.Code)
	}

	// 6b. GET History for unknown URL
	req = httptest.NewRequest("GET", "/v1/history?url=https://not-exists.com", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK for empty history, got %d", w.Code)
	}
	var history map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &history)
	if history["test_count"].(float64) != 0 {
		t.Errorf("expected 0 tests in history, got %v", history["test_count"])
	}
}

func TestAPIServer_WebhookSSRF(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "webhook-ssrf-test")
	defer os.RemoveAll(tmpDir)
	s, _ := store.NewStore(filepath.Join(tmpDir, "test.db"))
	defer s.Close()

	m := job.NewManager(s, 1, 10, "")
	m.Start()
	defer m.Stop()

	srv := NewServer(m, s, "", true)
	mux := srv.Routes()

	tests := []struct {
		webhookURL string
		wantCode   int
	}{
		{"http://127.0.0.1", http.StatusBadRequest},
		{"http://localhost", http.StatusBadRequest},
		{"http://169.254.169.254", http.StatusBadRequest},
		{"http://10.0.0.1", http.StatusBadRequest},
		{"https://example.com/webhook", http.StatusAccepted},
		{"", http.StatusAccepted},
	}

	for _, tt := range tests {
		t.Run(tt.webhookURL, func(t *testing.T) {
			reqBody := map[string]interface{}{
				"url":         "http://example.com",
				"runs":        1,
				"webhook_url": tt.webhookURL,
			}
			body, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/v1/jobs", bytes.NewReader(body))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("WebhookURL %q: expected code %d, got %d", tt.webhookURL, tt.wantCode, w.Code)
			}
		})
	}
}

func TestAPIServer_RunsConstraint(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "runs-constraint-test")
	defer os.RemoveAll(tmpDir)
	s, _ := store.NewStore(filepath.Join(tmpDir, "test.db"))
	defer s.Close()

	m := job.NewManager(s, 1, 10, "")
	m.Start()
	defer m.Stop()

	srv := NewServer(m, s, "", true)
	mux := srv.Routes()

	tests := []struct {
		runs     int
		wantCode int
	}{
		{0, http.StatusAccepted},
		{1, http.StatusAccepted},
		{10, http.StatusAccepted},
		{11, http.StatusBadRequest},
		{100, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("runs-%d", tt.runs), func(t *testing.T) {
			reqBody := map[string]interface{}{
				"url":  "http://example.com",
				"runs": tt.runs,
			}
			body, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/v1/jobs", bytes.NewReader(body))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("Runs %d: expected code %d, got %d", tt.runs, tt.wantCode, w.Code)
			}
		})
	}
}

