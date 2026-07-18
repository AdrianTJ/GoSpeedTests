package job

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AdrianTJ/loadstar/internal/collector/lighthouse"
	"github.com/AdrianTJ/loadstar/internal/store"
)

func TestJobManager_PartialSuccess(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "partial-test")
	defer os.RemoveAll(tmpDir)
	s, _ := store.NewStore(filepath.Join(tmpDir, "test.db"))
	defer s.Close()

	m := NewManager(s, 1, 10, "")
	m.Start()
	defer m.Stop()

	// Server that fails every other request
	count := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if count%2 == 0 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Run 2 times. One should succeed, one should fail.
	job, _ := m.Submit(context.Background(), ts.URL, []string{"network"}, 2, "")

	// Wait for completion
	var finalJob *store.Job
	for i := 0; i < 50; i++ {
		finalJob, _ = s.GetJob(context.Background(), job.ID)
		if finalJob.Status == store.StatusPartial || finalJob.Status == store.StatusCompleted || finalJob.Status == store.StatusFailed {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if finalJob.Status != store.StatusPartial {
		t.Errorf("expected status PARTIAL, got %s", finalJob.Status)
	}
	if finalJob.Error == nil || !strings.Contains(*finalJob.Error, "1/2 runs completed cleanly") {
		t.Errorf("expected partial error message, got %v", finalJob.Error)
	}
}

// TestJobManager_PerTierPartial verifies that within a single run, one tier
// failing while another succeeds yields PARTIAL (not FAILED), and the
// successful tier's result is still persisted.
func TestJobManager_PerTierPartial(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "per-tier-partial")
	defer os.RemoveAll(tmpDir)
	s, _ := store.NewStore(filepath.Join(tmpDir, "test.db"))
	defer s.Close()

	m := NewManager(s, 1, 10, "")
	m.Start()
	defer m.Stop()

	// Working target for the network tier.
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	// Failing PSI endpoint for the lighthouse tier.
	psi := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer psi.Close()
	lighthouse.SetEndpoint(psi.URL)
	defer lighthouse.SetEndpoint("https://www.googleapis.com/pagespeedonline/v5/runPagespeed")

	job, _ := m.Submit(context.Background(), target.URL, []string{"network", "lighthouse"}, 1, "")

	var finalJob *store.Job
	for i := 0; i < 50; i++ {
		finalJob, _ = s.GetJob(context.Background(), job.ID)
		if finalJob.Status == store.StatusPartial || finalJob.Status == store.StatusCompleted || finalJob.Status == store.StatusFailed {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if finalJob.Status != store.StatusPartial {
		t.Fatalf("expected PARTIAL (network ok, lighthouse failed), got %s (err=%v)", finalJob.Status, finalJob.Error)
	}
	if finalJob.Error == nil || !strings.Contains(*finalJob.Error, "lighthouse") {
		t.Errorf("expected error to name the failing lighthouse tier, got %v", finalJob.Error)
	}
	// The successful network result must still be persisted.
	results, _ := s.GetResultsByJobID(context.Background(), job.ID)
	if len(results) != 1 || results[0].Network == nil {
		t.Errorf("expected the network result to be saved despite the lighthouse failure, got %d results", len(results))
	}
}
