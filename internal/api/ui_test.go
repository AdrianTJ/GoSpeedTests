package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
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

// TestStatusPage_CSPNonce is part of the regression for the July 2026 audit
// finding SEC-4. The status page holds the operator's API key in localStorage,
// so its inline <style>/<script> are pinned with a per-response nonce rather
// than opened up with 'unsafe-inline'.
func TestStatusPage_CSPNonce(t *testing.T) {
	srv := &Server{}
	rec := httptest.NewRecorder()
	srv.handleUI(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("no Content-Security-Policy on the status page")
	}
	if strings.Contains(csp, "unsafe-inline") {
		t.Errorf("CSP allows unsafe-inline: %s", csp)
	}

	body := rec.Body.String()
	if strings.Contains(body, noncePlaceholder) {
		t.Errorf("nonce placeholder %q was not substituted", noncePlaceholder)
	}

	// The nonce in the CSP must be the one actually on the tags, or the
	// browser blocks the page's own script.
	m := regexp.MustCompile(`'nonce-([^']+)'`).FindStringSubmatch(csp)
	if m == nil {
		t.Fatalf("CSP carries no nonce: %s", csp)
	}
	nonce := m[1]
	for _, tag := range []string{`<style nonce="` + nonce + `">`, `<script nonce="` + nonce + `">`} {
		if !strings.Contains(body, tag) {
			t.Errorf("page is missing %s", tag)
		}
	}
}

// A nonce that repeats across responses is no better than unsafe-inline.
func TestStatusPage_NonceIsPerResponse(t *testing.T) {
	srv := &Server{}
	nonceOf := func() string {
		rec := httptest.NewRecorder()
		srv.handleUI(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		m := regexp.MustCompile(`'nonce-([^']+)'`).FindStringSubmatch(
			rec.Header().Get("Content-Security-Policy"))
		if m == nil {
			t.Fatal("CSP carries no nonce")
		}
		return m[1]
	}
	if a, b := nonceOf(), nonceOf(); a == b {
		t.Errorf("nonce reused across responses: %s", a)
	}
}
