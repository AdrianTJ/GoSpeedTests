package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AdrianTJ/loadstar/internal/collector/network"
)

func TestJobStatus(t *testing.T) {
	// Simple validation of status constants
	statuses := []JobStatus{
		StatusPending,
		StatusRunning,
		StatusCompleted,
		StatusFailed,
		StatusTimeout,
	}

	for _, s := range statuses {
		if s == "" {
			t.Error("JobStatus should not be empty")
		}
	}
}

// TestDeleteJobRemovesChildren guards against orphaned rows: deleting a job
// must also remove its results (via ON DELETE CASCADE, which requires the
// foreign_keys pragma) and its webhook deliveries (removed explicitly, as that
// table has no foreign key).
func TestDeleteJobRemovesChildren(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "delete-cascade")
	defer os.RemoveAll(tmpDir)

	s, err := NewStore(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	jobID := "jb_delete"

	if err := s.CreateJob(ctx, &Job{
		ID: jobID, URL: "http://example.com", Status: StatusCompleted,
		Tiers: []string{"network"}, WebhookURL: "http://example.com/hook",
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := s.SaveResult(ctx, &Result{
		ID: "res_delete", JobID: jobID, RunIndex: 1,
		Network: &network.Result{TotalMS: 42.0}, CollectedAt: time.Now(),
	}); err != nil {
		t.Fatalf("save result: %v", err)
	}
	if err := s.EnqueueWebhook(ctx, &WebhookDelivery{
		ID: "wh_delete", JobID: jobID, URL: "http://example.com/hook",
		Payload: []byte("{}"), Status: "PENDING", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("enqueue webhook: %v", err)
	}

	if err := s.DeleteJob(ctx, jobID); err != nil {
		t.Fatalf("delete job: %v", err)
	}

	results, err := s.GetResultsByJobID(ctx, jobID)
	if err != nil {
		t.Fatalf("get results: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected results to cascade-delete, got %d orphaned rows", len(results))
	}

	// webhook_deliveries has no FK, so verify the explicit cleanup ran.
	db := s.(*sqliteStore).db
	var whCount int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM webhook_deliveries WHERE job_id = ?", jobID).Scan(&whCount); err != nil && err != sql.ErrNoRows {
		t.Fatalf("count webhooks: %v", err)
	}
	if whCount != 0 {
		t.Errorf("expected webhook deliveries to be removed, got %d orphaned rows", whCount)
	}
}

// TestRecoverInterruptedJobs verifies stale RUNNING/PENDING jobs are failed on
// startup while terminal jobs are left untouched.
func TestRecoverInterruptedJobs(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "recover")
	defer os.RemoveAll(tmpDir)
	s, err := NewStore(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	mk := func(id string, st JobStatus) {
		if err := s.CreateJob(ctx, &Job{ID: id, URL: "http://x", Status: st, Tiers: []string{"network"}, CreatedAt: time.Now()}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	mk("jb_run", StatusRunning)
	mk("jb_pend", StatusPending)
	mk("jb_done", StatusCompleted)

	n, err := s.RecoverInterruptedJobs(ctx)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 recovered, got %d", n)
	}
	for _, id := range []string{"jb_run", "jb_pend"} {
		j, _ := s.GetJob(ctx, id)
		if j.Status != StatusFailed {
			t.Errorf("%s: expected FAILED, got %s", id, j.Status)
		}
		if j.Error == nil {
			t.Errorf("%s: expected an error message", id)
		}
	}
	done, _ := s.GetJob(ctx, "jb_done")
	if done.Status != StatusCompleted {
		t.Errorf("terminal job should be untouched, got %s", done.Status)
	}
}

func TestGetJob_NotFound(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "getjob-nf")
	defer os.RemoveAll(tmpDir)
	s, _ := NewStore(filepath.Join(tmpDir, "test.db"))
	defer s.Close()
	j, err := s.GetJob(context.Background(), "does-not-exist")
	if err != nil {
		t.Fatalf("expected nil error for missing job, got %v", err)
	}
	if j != nil {
		t.Errorf("expected nil job for missing id, got %+v", j)
	}
}

func TestListJobs_OrderAndLimit(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "listjobs")
	defer os.RemoveAll(tmpDir)
	s, _ := NewStore(filepath.Join(tmpDir, "test.db"))
	defer s.Close()
	ctx := context.Background()

	base := time.Now()
	for i := 0; i < 5; i++ {
		s.CreateJob(ctx, &Job{
			ID: "jb_" + string(rune('a'+i)), URL: "http://x", Status: StatusPending,
			Tiers: []string{"network"}, CreatedAt: base.Add(time.Duration(i) * time.Second),
		})
	}
	jobs, err := s.ListJobs(ctx, 3)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(jobs) != 3 {
		t.Fatalf("expected limit 3, got %d", len(jobs))
	}
	// Newest first: the last created (jb_e) must lead.
	if jobs[0].ID != "jb_e" {
		t.Errorf("expected newest job first (jb_e), got %s", jobs[0].ID)
	}
}

func TestGetHistory_EmptyAndPopulated(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "history")
	defer os.RemoveAll(tmpDir)
	s, _ := NewStore(filepath.Join(tmpDir, "test.db"))
	defer s.Close()
	ctx := context.Background()

	h, err := s.GetHistory(ctx, "http://none.example")
	if err != nil {
		t.Fatalf("history(empty): %v", err)
	}
	if h.TestCount != 0 {
		t.Errorf("expected 0 tests for unknown url, got %d", h.TestCount)
	}

	url := "http://hist.example"
	s.CreateJob(ctx, &Job{ID: "jb_h", URL: url, Status: StatusCompleted, Tiers: []string{"network"}, CreatedAt: time.Now()})
	for i := 0; i < 2; i++ {
		s.SaveResult(ctx, &Result{
			ID: "res_" + string(rune('a'+i)), JobID: "jb_h", RunIndex: i,
			Network: &network.Result{TTFBMS: 10, TotalMS: 20}, CollectedAt: time.Now(),
		})
	}
	h, err = s.GetHistory(ctx, url)
	if err != nil {
		t.Fatalf("history(populated): %v", err)
	}
	if h.TestCount != 2 {
		t.Errorf("expected 2 tests, got %d", h.TestCount)
	}
	if h.AvgTTFBMS != 10 || h.AvgTotalMS != 20 {
		t.Errorf("unexpected averages: ttfb=%v total=%v", h.AvgTTFBMS, h.AvgTotalMS)
	}
}
