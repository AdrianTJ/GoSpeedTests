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
	"github.com/AdrianTJ/gospeedtest/internal/store/sqlite"
)

func TestJobManager(t *testing.T) {
	// Setup store
	tmpDir, _ := os.MkdirTemp("", "job-manager-test")
	defer os.RemoveAll(tmpDir)
	dbPath := filepath.Join(tmpDir, "test.db")
	s, _ := sqlite.NewStore(dbPath)
	defer s.Close()

	// Setup manager
	m := NewManager(s, 2, 10)
	m.Start()
	defer m.Stop()

	// Setup test server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer ts.Close()

	ctx := context.Background()

	// Submit job
	job, err := m.Submit(ctx, ts.URL, []string{"network"}, 1, "")
	if err != nil {
		t.Fatalf("failed to submit job: %v", err)
	}

	// Wait for job to complete (polling)
	var finalJob *store.Job
	for i := 0; i < 50; i++ {
		finalJob, _ = s.GetJob(ctx, job.ID)
		if finalJob.Status == store.StatusCompleted {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if finalJob.Status != store.StatusCompleted {
		t.Errorf("job expected to be COMPLETED, got %s", finalJob.Status)
	}

	// Verify results
	results, err := s.GetResultsByJobID(ctx, job.ID)
	if err != nil {
		t.Fatalf("failed to get results: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
	if results[0].Network.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", results[0].Network.StatusCode)
	}

	// Test Webhook
	webhookReceived := make(chan bool, 1)
	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webhookReceived <- true
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookServer.Close()

	_, err = m.Submit(ctx, ts.URL, []string{"network"}, 1, webhookServer.URL)
	if err != nil {
		t.Fatalf("failed to submit job with webhook: %v", err)
	}

	select {
	case <-webhookReceived:
		// Success
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for webhook")
	}
}
