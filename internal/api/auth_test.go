package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/AdrianTJ/gospeedtest/internal/job"
	"github.com/AdrianTJ/gospeedtest/internal/store"
)

func TestAPIServer_FailSecureAuth(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "auth-test")
	defer os.RemoveAll(tmpDir)
	s, _ := store.NewStore(filepath.Join(tmpDir, "test.db"))
	defer s.Close()

	m := job.NewManager(s, 1, 10, "")

	apiKey := "top-secret"
	srv := NewServer(m, s, apiKey, false)
	mux := srv.Routes()

	routes := []struct {
		method string
		path   string
	}{
		{"GET", "/v1/jobs"},
		{"POST", "/v1/jobs"},
		{"GET", "/v1/history?url=test"},
		{"GET", "/v1/health"},
		{"GET", "/v1/ready"},
	}

	for _, rt := range routes {
		t.Run(rt.path, func(t *testing.T) {
			req := httptest.NewRequest(rt.method, rt.path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if rt.path == "/v1/health" || rt.path == "/v1/ready" {
				if w.Code != http.StatusOK {
					t.Errorf("%s %s without key: expected 200, got %d", rt.method, rt.path, w.Code)
				}
				return
			}

			if w.Code != http.StatusUnauthorized {
				t.Errorf("%s %s without key: expected 401, got %d", rt.method, rt.path, w.Code)
			}

			req = httptest.NewRequest(rt.method, rt.path, nil)
			req.Header.Set("X-API-Key", "wrong-password")
			w = httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("%s %s with wrong key: expected 401, got %d", rt.method, rt.path, w.Code)
			}

			req = httptest.NewRequest(rt.method, rt.path, nil)
			req.Header.Set("X-API-Key", apiKey)
			w = httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code == http.StatusUnauthorized {
				t.Errorf("%s %s with correct key: expected success, got 401", rt.method, rt.path)
			}
		})
	}
}

func TestAPIServer_MisconfiguredAuth(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "misconfig-auth")
	defer os.RemoveAll(tmpDir)
	s, _ := store.NewStore(filepath.Join(tmpDir, "test.db"))
	defer s.Close()

	m := job.NewManager(s, 1, 10, "")

	// No key AND no insecure flag
	srv := NewServer(m, s, "", false)
	mux := srv.Routes()

	req := httptest.NewRequest("GET", "/v1/jobs", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for misconfigured auth, got %d", w.Code)
	}
}
