package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/AdrianTJ/gospeedtest/internal/job"
	"github.com/AdrianTJ/gospeedtest/internal/store"
)

func TestSSRFPrevention(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "ssrf-test")
	defer os.RemoveAll(tmpDir)
	dbPath := filepath.Join(tmpDir, "test.db")
	s, _ := store.NewStore(dbPath)
	defer s.Close()

	m := job.NewManager(s, 1, 10, "")
	m.Start()
	defer m.Stop()

	srv := NewServer(m, s, "", true)
	mux := srv.Routes()

	tests := []struct {
		url          string
		expectedCode int
	}{
		{"https://google.com", http.StatusAccepted},
		{"http://localhost", http.StatusBadRequest},
		{"http://127.0.0.1", http.StatusBadRequest},
		{"http://169.254.169.254", http.StatusBadRequest},
		{"file:///etc/passwd", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			reqBody := map[string]interface{}{
				"url": tt.url,
			}
			body, _ := json.Marshal(reqBody)
			req := httptest.NewRequest("POST", "/v1/jobs", bytes.NewReader(body))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.expectedCode {
				t.Errorf("POST /v1/jobs with URL %s: expected status %d, got %d. Body: %s", tt.url, tt.expectedCode, w.Code, w.Body.String())
			}
		})
	}
}
