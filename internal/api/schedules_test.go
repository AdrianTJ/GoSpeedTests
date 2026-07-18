package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AdrianTJ/loadstar/internal/job"
	"github.com/AdrianTJ/loadstar/internal/store"
)

func newScheduleTestServer(t *testing.T, name, apiKey string, insecure bool) http.Handler {
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

	return NewServer(m, s, apiKey, insecure).Routes()
}

func doJSON(t *testing.T, mux http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body != "" {
		rdr = bytes.NewReader([]byte(body))
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func TestScheduleLifecycle(t *testing.T) {
	mux := newScheduleTestServer(t, "api-sched-crud", "", true)

	// Create.
	w := doJSON(t, mux, "POST", "/v1/schedules",
		`{"url":"http://example.com","tiers":["network"],"runs":2,"interval_seconds":120,
		  "budget":{"assertions":{"network.ttfb_ms":{"max":500}}}}`, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d (%s)", w.Code, w.Body.String())
	}
	var sc store.Schedule
	json.Unmarshal(w.Body.Bytes(), &sc)
	if sc.ID == "" || !strings.HasPrefix(sc.ID, "sc_") {
		t.Fatalf("bad schedule id: %q", sc.ID)
	}
	if !sc.Enabled || sc.NextRunAt == nil {
		t.Errorf("new schedule must be enabled with next_run_at set: %+v", sc)
	}

	// List.
	w = doJSON(t, mux, "GET", "/v1/schedules", "", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), sc.ID) {
		t.Errorf("list = %d %s", w.Code, w.Body.String())
	}

	// Get.
	w = doJSON(t, mux, "GET", "/v1/schedules/"+sc.ID, "", nil)
	if w.Code != http.StatusOK {
		t.Errorf("get = %d", w.Code)
	}

	// Disable via PATCH.
	w = doJSON(t, mux, "PATCH", "/v1/schedules/"+sc.ID, `{"enabled":false}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("patch = %d (%s)", w.Code, w.Body.String())
	}
	var patched store.Schedule
	json.Unmarshal(w.Body.Bytes(), &patched)
	if patched.Enabled {
		t.Error("schedule still enabled after PATCH")
	}

	// Delete.
	w = doJSON(t, mux, "DELETE", "/v1/schedules/"+sc.ID, "", nil)
	if w.Code != http.StatusNoContent {
		t.Errorf("delete = %d", w.Code)
	}
	w = doJSON(t, mux, "GET", "/v1/schedules/"+sc.ID, "", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("get after delete = %d, want 404", w.Code)
	}
}

func TestCreateSchedule_Validation(t *testing.T) {
	mux := newScheduleTestServer(t, "api-sched-valid", "", true)

	cases := []struct {
		name string
		body string
	}{
		{"missing url", `{"tiers":["network"],"interval_seconds":60}`},
		{"bad tier", `{"url":"http://example.com","tiers":["bogus"],"interval_seconds":60}`},
		{"interval too small", `{"url":"http://example.com","tiers":["network"],"interval_seconds":30}`},
		{"missing interval", `{"url":"http://example.com","tiers":["network"]}`},
		{"too many runs", `{"url":"http://example.com","tiers":["network"],"interval_seconds":60,"runs":99}`},
		{"invalid budget", `{"url":"http://example.com","tiers":["network"],"interval_seconds":60,"budget":{"assertions":{"nope":{"max":1}}}}`},
		{"private-ip url", `{"url":"http://127.0.0.1/","tiers":["network"],"interval_seconds":60}`},
	}
	for _, c := range cases {
		w := doJSON(t, mux, "POST", "/v1/schedules", c.body, nil)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (%s)", c.name, w.Code, w.Body.String())
		}
	}

	// Bad PATCH bodies.
	w := doJSON(t, mux, "POST", "/v1/schedules",
		`{"url":"http://example.com","tiers":["network"],"interval_seconds":60}`, nil)
	var sc store.Schedule
	json.Unmarshal(w.Body.Bytes(), &sc)
	for _, body := range []string{`{}`, `{"url":"http://other.example"}`, `not json`} {
		w = doJSON(t, mux, "PATCH", "/v1/schedules/"+sc.ID, body, nil)
		if w.Code != http.StatusBadRequest {
			t.Errorf("patch %q: status = %d, want 400", body, w.Code)
		}
	}
}

func TestSchedulesRequireAuth(t *testing.T) {
	mux := newScheduleTestServer(t, "api-sched-auth", "sekrit", false)

	paths := []struct{ method, path string }{
		{"POST", "/v1/schedules"},
		{"GET", "/v1/schedules"},
		{"GET", "/v1/schedules/sc_x"},
		{"PATCH", "/v1/schedules/sc_x"},
		{"DELETE", "/v1/schedules/sc_x"},
		{"GET", "/metrics"},
	}
	for _, p := range paths {
		w := doJSON(t, mux, p.method, p.path, "", nil)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without key = %d, want 401", p.method, p.path, w.Code)
		}
	}
}

func TestMetricsEndpoint(t *testing.T) {
	mux := newScheduleTestServer(t, "api-metrics", "sekrit", false)

	w := doJSON(t, mux, "GET", "/metrics", "", map[string]string{"X-API-Key": "sekrit"})
	if w.Code != http.StatusOK {
		t.Fatalf("metrics = %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("content-type = %q, want text/plain", ct)
	}
	body := w.Body.String()
	for _, want := range []string{"# TYPE loadstar_queue_depth gauge", "loadstar_queue_depth 0"} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics body missing %q:\n%s", want, body)
		}
	}
}
