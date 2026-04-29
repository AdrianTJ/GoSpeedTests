package job

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/AdrianTJ/gospeedtest/internal/store"
)

func TestBrowserConcurrencyStress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	tmpDir, _ := os.MkdirTemp("", "stress-test")
	defer os.RemoveAll(tmpDir)
	s, _ := store.NewStore(filepath.Join(tmpDir, "test.db"))
	defer s.Close()

	// 3 workers, but 10 jobs
	m := NewManager(s, 3, 20, "")
	m.Start()
	defer m.Stop()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html><body><h1>Stress Test</h1></body></html>")
	}))
	defer ts.Close()

	var wg sync.WaitGroup
	jobCount := 10
	wg.Add(jobCount)

	for i := 0; i < jobCount; i++ {
		go func(idx int) {
			defer wg.Done()
			job, err := m.Submit(context.Background(), ts.URL, []string{"network", "browser"}, 1, "")
			if err != nil {
				t.Errorf("failed to submit job %d: %v", idx, err)
				return
			}

			// Poll for completion
			for j := 0; j < 100; j++ {
				finalJob, _ := s.GetJob(context.Background(), job.ID)
				if finalJob.Status == store.StatusCompleted || finalJob.Status == store.StatusFailed {
					if finalJob.Status == store.StatusFailed {
						t.Errorf("job %d failed: %v", idx, finalJob.Error)
					}
					return
				}
				time.Sleep(200 * time.Millisecond)
			}
			t.Errorf("job %d timed out", idx)
		}(i)
	}

	wg.Wait()
}
