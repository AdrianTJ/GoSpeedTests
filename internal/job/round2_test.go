package job

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AdrianTJ/gospeedtest/internal/store"
)

// TestSubmit_QueueFullCleansUpRow verifies a rejected (queue-full) submission
// does not leave an orphaned PENDING row behind.
func TestSubmit_QueueFullCleansUpRow(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "queuefull-cleanup")
	defer os.RemoveAll(tmpDir)
	s, _ := store.NewStore(filepath.Join(tmpDir, "test.db"))
	defer s.Close()

	m := NewManager(s, 0, 1, "") // 0 workers, queue depth 1
	ctx := context.Background()

	if _, err := m.Submit(ctx, "http://example.com", []string{"network"}, 1, ""); err != nil {
		t.Fatalf("first submit: %v", err)
	}
	if _, err := m.Submit(ctx, "http://example.com", []string{"network"}, 1, ""); err == nil {
		t.Fatal("expected queue-full error on second submit")
	}
	jobs, _ := s.ListJobs(ctx, 100)
	if len(jobs) != 1 {
		t.Errorf("expected exactly 1 job row after a rejected submit, got %d", len(jobs))
	}
}

// TestProcessJob_KeepsNetworkMetricsOn5xx verifies that a target returning an
// HTTP error status still has its timing metrics persisted (the job fails, but
// the measured data is not discarded).
func TestProcessJob_KeepsNetworkMetricsOn5xx(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "metrics-5xx")
	defer os.RemoveAll(tmpDir)
	s, _ := store.NewStore(filepath.Join(tmpDir, "test.db"))
	defer s.Close()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	m := NewManager(s, 1, 10, "")
	m.Start()
	defer m.Stop()

	job, _ := m.Submit(context.Background(), ts.URL, []string{"network"}, 1, "")
	var final *store.Job
	for i := 0; i < 50; i++ {
		final, _ = s.GetJob(context.Background(), job.ID)
		if final.Status == store.StatusFailed || final.Status == store.StatusCompleted {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if final.Status != store.StatusFailed {
		t.Errorf("expected FAILED for a 500 target, got %s", final.Status)
	}
	results, _ := s.GetResultsByJobID(context.Background(), job.ID)
	if len(results) != 1 || results[0].Network == nil {
		t.Fatalf("expected network metrics to be preserved on 5xx, got %d results", len(results))
	}
	if results[0].Network.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected captured status_code 500, got %d", results[0].Network.StatusCode)
	}
}
