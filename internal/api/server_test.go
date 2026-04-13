package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AdrianTJ/gospeedtest/internal/job"
	"github.com/AdrianTJ/gospeedtest/internal/store/sqlite"
)

func TestAPIServer(t *testing.T) {
	// Setup
	tmpDir, _ := os.MkdirTemp("", "api-server-test")
	defer os.RemoveAll(tmpDir)
	dbPath := filepath.Join(tmpDir, "test.db")
	s, _ := sqlite.NewStore(dbPath)
	defer s.Close()

	m := job.NewManager(s, 1, 10)
	m.Start()
	defer m.Stop()

	srv := NewServer(m, s)
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
}
