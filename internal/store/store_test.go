package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AdrianTJ/gospeedtest/internal/collector/network"
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
