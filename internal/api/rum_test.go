package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AdrianTJ/loadstar/internal/job"
	"github.com/AdrianTJ/loadstar/internal/store"
)

func newRUMTestServer(t *testing.T, name string, origins []string) (*Server, http.Handler, store.Store) {
	t.Helper()
	tmpDir, _ := os.MkdirTemp("", name)
	t.Cleanup(func() { os.RemoveAll(tmpDir) })
	s, err := store.NewStore(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	m := job.NewManager(s, 1, 10, "")
	m.Start()
	t.Cleanup(func() { m.Stop() })

	srv := NewServer(m, s, "", true)
	if origins != nil {
		srv.SetRUMOrigins(origins)
	}
	return srv, srv.Routes(), s
}

func postRUM(mux http.Handler, body string, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/v1/rum", bytes.NewReader([]byte(body)))
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

const goodEvent = `{"url":"https://site.example/page","name":"LCP","value":1234.5}`

func TestRUM_DisabledWithoutOrigins(t *testing.T) {
	_, mux, _ := newRUMTestServer(t, "rum-disabled", nil)
	if w := postRUM(mux, goodEvent, ""); w.Code != http.StatusNotFound {
		t.Errorf("ingest status = %d, want 404 when unconfigured", w.Code)
	}
	// Preflight is hidden too.
	req := httptest.NewRequest("OPTIONS", "/v1/rum", nil)
	req.Header.Set("Origin", "https://site.example")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("preflight status = %d, want 404 when unconfigured", w.Code)
	}
}

func TestRUM_IngestHappyPath(t *testing.T) {
	_, mux, s := newRUMTestServer(t, "rum-happy", []string{"https://site.example"})

	w := postRUM(mux, goodEvent, "https://site.example")
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (%s)", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://site.example" {
		t.Errorf("CORS header = %q", got)
	}

	values, err := s.GetRUMValues(context.Background(), "https://site.example/page", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("get values: %v", err)
	}
	if len(values["LCP"]) != 1 || values["LCP"][0] != 1234.5 {
		t.Errorf("stored values = %v, want [1234.5]", values["LCP"])
	}
}

func TestRUM_NoOriginHeaderAccepted(t *testing.T) {
	// sendBeacon from the same origin (and curl) sends no Origin header; once
	// the endpoint is enabled those must work.
	_, mux, _ := newRUMTestServer(t, "rum-noorigin", []string{"https://site.example"})
	if w := postRUM(mux, goodEvent, ""); w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204 for origin-less beacon", w.Code)
	}
}

func TestRUM_DisallowedOrigin(t *testing.T) {
	_, mux, _ := newRUMTestServer(t, "rum-badorigin", []string{"https://site.example"})
	if w := postRUM(mux, goodEvent, "https://evil.example"); w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestRUM_WildcardOrigin(t *testing.T) {
	_, mux, _ := newRUMTestServer(t, "rum-wildcard", []string{"*"})
	if w := postRUM(mux, goodEvent, "https://anything.example"); w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204 with wildcard", w.Code)
	}
}

func TestRUM_Preflight(t *testing.T) {
	_, mux, _ := newRUMTestServer(t, "rum-preflight", []string{"https://site.example"})
	req := httptest.NewRequest("OPTIONS", "/v1/rum", nil)
	req.Header.Set("Origin", "https://site.example")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", w.Code)
	}
	if !strings.Contains(w.Header().Get("Access-Control-Allow-Methods"), "POST") {
		t.Errorf("preflight missing POST in allow-methods")
	}
}

func TestRUM_ValidationRejections(t *testing.T) {
	_, mux, _ := newRUMTestServer(t, "rum-validation", []string{"*"})
	cases := []struct {
		name string
		body string
	}{
		{"bad metric", `{"url":"https://a.example/","name":"SPEED","value":1}`},
		{"negative value", `{"url":"https://a.example/","name":"LCP","value":-1}`},
		{"huge value", `{"url":"https://a.example/","name":"LCP","value":900000}`},
		{"relative url", `{"url":"/page","name":"LCP","value":1}`},
		{"non-http scheme", `{"url":"ftp://a.example/","name":"LCP","value":1}`},
		{"not json", `LCP=1200`},
	}
	for _, c := range cases {
		if w := postRUM(mux, c.body, ""); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", c.name, w.Code)
		}
	}
}

func TestRUM_OversizeBodyRejected(t *testing.T) {
	_, mux, _ := newRUMTestServer(t, "rum-oversize", []string{"*"})
	big := `{"url":"https://a.example/","name":"LCP","value":1,"pad":"` + strings.Repeat("x", maxRUMBody) + `"}`
	if w := postRUM(mux, big, ""); w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for oversize body", w.Code)
	}
}

func TestRUM_RateLimit(t *testing.T) {
	srv, mux, _ := newRUMTestServer(t, "rum-ratelimit", []string{"*"})
	// Drain the bucket instantly instead of sending rumBurst real requests.
	srv.rumLimiter.mu.Lock()
	srv.rumLimiter.tokens = 0
	srv.rumLimiter.lastRefill = time.Now()
	srv.rumLimiter.mu.Unlock()

	if w := postRUM(mux, goodEvent, ""); w.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429 when bucket empty", w.Code)
	}
}

func TestRUM_SummaryRequiresAuthAndComputesP75(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "rum-summary")
	t.Cleanup(func() { os.RemoveAll(tmpDir) })
	s, _ := store.NewStore(filepath.Join(tmpDir, "test.db"))
	t.Cleanup(func() { s.Close() })
	m := job.NewManager(s, 1, 10, "")
	m.Start()
	t.Cleanup(func() { m.Stop() })

	// Authenticated server this time (apiKey set, not insecure).
	srv := NewServer(m, s, "secret", false)
	srv.SetRUMOrigins([]string{"*"})
	mux := srv.Routes()

	url := "https://site.example/page"
	for i, v := range []float64{100, 200, 300, 400} {
		s.SaveRUMEvent(context.Background(), &store.RUMEvent{
			ID: "rum_" + string(rune('a'+i)), URL: url, Metric: "LCP", Value: v, CreatedAt: time.Now(),
		})
	}

	// No key -> 401.
	req := httptest.NewRequest("GET", "/v1/rum/summary?url="+url, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status without key = %d, want 401", w.Code)
	}

	// With key -> p75 = 325.
	req = httptest.NewRequest("GET", "/v1/rum/summary?url="+url, nil)
	req.Header.Set("X-API-Key", "secret")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status with key = %d (%s)", w.Code, w.Body.String())
	}
	var resp struct {
		WindowHours int `json:"window_hours"`
		Metrics     map[string]struct {
			Count int     `json:"count"`
			P75   float64 `json:"p75"`
		} `json:"metrics"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.WindowHours != 24 {
		t.Errorf("window_hours = %d, want default 24", resp.WindowHours)
	}
	lcp, ok := resp.Metrics["LCP"]
	if !ok || lcp.Count != 4 || lcp.P75 != 325 {
		t.Errorf("LCP stats = %+v, want count=4 p75=325", lcp)
	}
}

func TestCreateJob_ProfileValidation(t *testing.T) {
	_, mux, s := newRUMTestServer(t, "api-profile", nil)

	// Unknown profile -> 400 listing the valid names.
	bad := `{"url":"http://example.com","tiers":["network"],"profile":"5g"}`
	req := httptest.NewRequest("POST", "/v1/jobs", bytes.NewReader([]byte(bad)))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "slow-3g") {
		t.Errorf("status = %d body=%q, want 400 listing profiles", w.Code, w.Body.String())
	}

	// Valid profile -> accepted and persisted on the job row.
	good := `{"url":"http://example.com","tiers":["network"],"profile":"slow-3g"}`
	req = httptest.NewRequest("POST", "/v1/jobs", bytes.NewReader([]byte(good)))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (%s)", w.Code, w.Body.String())
	}
	var created map[string]string
	json.Unmarshal(w.Body.Bytes(), &created)
	jb, err := s.GetJob(context.Background(), created["job_id"])
	if err != nil || jb == nil {
		t.Fatalf("get job: %v", err)
	}
	if jb.Profile != "slow-3g" {
		t.Errorf("job profile = %q, want slow-3g", jb.Profile)
	}
}

func TestCreateSchedule_ProfileValidation(t *testing.T) {
	_, mux, _ := newRUMTestServer(t, "api-sched-profile", nil)

	bad := `{"url":"http://example.com","tiers":["network"],"interval_seconds":60,"profile":"warp"}`
	req := httptest.NewRequest("POST", "/v1/schedules", bytes.NewReader([]byte(bad)))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for bad schedule profile", w.Code)
	}

	good := `{"url":"http://example.com","tiers":["network"],"interval_seconds":60,"profile":"4g"}`
	req = httptest.NewRequest("POST", "/v1/schedules", bytes.NewReader([]byte(good)))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", w.Code, w.Body.String())
	}
	var sc store.Schedule
	json.Unmarshal(w.Body.Bytes(), &sc)
	if sc.Profile != "4g" {
		t.Errorf("schedule profile = %q, want 4g", sc.Profile)
	}
}

func TestRUM_SummaryBadWindow(t *testing.T) {
	_, mux, _ := newRUMTestServer(t, "rum-window", []string{"*"})
	for _, q := range []string{"window_h=0", "window_h=-5", "window_h=99999", "window_h=abc"} {
		req := httptest.NewRequest("GET", "/v1/rum/summary?url=https://a.example/&"+q, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", q, w.Code)
		}
	}
}
