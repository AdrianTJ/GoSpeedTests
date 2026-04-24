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

	"github.com/AdrianTJ/gospeedtest/internal/store"
)

func TestJobManager_PartialSuccess(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "partial-test")
	defer os.RemoveAll(tmpDir)
	s, _ := store.NewStore(filepath.Join(tmpDir, "test.db"))
	defer s.Close()

	m := NewManager(s, 1, 10)
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
	if finalJob.Error == nil || !strings.Contains(*finalJob.Error, "only 1/2 runs succeeded") {
		t.Errorf("expected partial error message, got %v", finalJob.Error)
	}
}
