package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AdrianTJ/loadstar/internal/job"
	"github.com/AdrianTJ/loadstar/internal/store"
)

func newUITestServer(t *testing.T) http.Handler {
	t.Helper()
	tmpDir, _ := os.MkdirTemp("", "ui-test")
	t.Cleanup(func() { os.RemoveAll(tmpDir) })
	s, err := store.NewStore(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	m := job.NewManager(s, 1, 10, "")
	m.Start()
	t.Cleanup(func() { m.Stop() })
	// API key set: proves the status page itself stays public.
	return NewServer(m, s, "secret", false).Routes()
}

func TestStatusPage_ServedPublicly(t *testing.T) {
	mux := newUITestServer(t)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content type = %q, want text/html", ct)
	}
	if !strings.Contains(w.Body.String(), "Loadstar") {
		t.Error("status page body missing title")
	}
}

func TestStatusPage_DoesNotSwallowOtherPaths(t *testing.T) {
	mux := newUITestServer(t)
	req := httptest.NewRequest("GET", "/no-such-page", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("GET /no-such-page = %d, want 404 (the /{$} route must match exactly /)", w.Code)
	}
}

// TestOpenAPI_ServedFromEmbed pins the cwd-independence fix: the spec must be
// served even when the process working directory has no docs/ folder.
func TestOpenAPI_ServedFromEmbed(t *testing.T) {
	mux := newUITestServer(t)
	t.Chdir(t.TempDir()) // no docs/openapi.yaml here

	req := httptest.NewRequest("GET", "/openapi.yaml", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /openapi.yaml = %d, want 200", w.Code)
	}
	if !strings.HasPrefix(w.Body.String(), "openapi:") {
		t.Errorf("spec body does not look like OpenAPI YAML: %q", w.Body.String()[:40])
	}
}
