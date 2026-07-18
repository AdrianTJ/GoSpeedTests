package job

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AdrianTJ/loadstar/internal/store"
)

// completed_at is stamped by UpdateJobStatus with time.Now(), so these tests
// steer the cutoff (via negative days) rather than faking clocks.

func TestRunRetention_PurgesTerminalJobs(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "retention")
	t.Cleanup(func() { os.RemoveAll(tmpDir) })
	s, _ := store.NewStore(filepath.Join(tmpDir, "test.db"))
	t.Cleanup(func() { s.Close() })

	m := NewManager(s, 1, 10, "")
	t.Cleanup(func() { m.Stop() })

	ctx := context.Background()
	s.CreateJob(ctx, &store.Job{ID: "jb_done", URL: "http://example.com",
		Status: store.StatusCompleted, Tiers: []string{"network"}, Runs: 1, CreatedAt: time.Now()})
	s.UpdateJobStatus(ctx, "jb_done", store.StatusCompleted, nil)
	s.CreateJob(ctx, &store.Job{ID: "jb_live", URL: "http://example.com",
		Status: store.StatusRunning, Tiers: []string{"network"}, Runs: 1, CreatedAt: time.Now()})

	// days = -1 makes the cutoff time.Now()+1d, so the just-completed job is
	// older than the cutoff and must go; the running job must stay.
	m.runRetention(-1)

	if j, _ := s.GetJob(ctx, "jb_done"); j != nil {
		t.Error("terminal job survived retention")
	}
	if j, _ := s.GetJob(ctx, "jb_live"); j == nil {
		t.Error("running job was purged")
	}
}

func TestStartRetention_DisabledForZeroDays(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "retention-off")
	t.Cleanup(func() { os.RemoveAll(tmpDir) })
	s, _ := store.NewStore(filepath.Join(tmpDir, "test.db"))
	t.Cleanup(func() { s.Close() })

	m := NewManager(s, 1, 10, "")
	m.StartRetention(0)  // must be a no-op: no goroutine, no purge
	m.StartRetention(-5) // ditto

	ctx := context.Background()
	s.CreateJob(ctx, &store.Job{ID: "jb_keep", URL: "http://example.com",
		Status: store.StatusCompleted, Tiers: []string{"network"}, Runs: 1, CreatedAt: time.Now()})
	s.UpdateJobStatus(ctx, "jb_keep", store.StatusCompleted, nil)

	m.Stop() // returns immediately if no retention goroutine leaked

	if j, _ := s.GetJob(ctx, "jb_keep"); j == nil {
		t.Error("retention ran despite days<=0")
	}
}
