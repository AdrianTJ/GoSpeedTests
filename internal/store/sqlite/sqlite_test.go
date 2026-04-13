package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AdrianTJ/gospeedtest/internal/collector/network"
	"github.com/AdrianTJ/gospeedtest/internal/store"
)

func TestSQLiteStore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gospeedtest-sqlite-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// 1. Test CreateJob
	job := &store.Job{
		ID:        "jb_1",
		URL:       "https://example.com",
		Status:    store.StatusPending,
		Tiers:     []string{"network"},
		Runs:      1,
		TimeoutS:  60,
		Tags:      map[string]string{"env": "test"},
		CreatedAt: time.Now().Truncate(time.Second), // SQLite DATETIME precision
	}

	if err := s.CreateJob(ctx, job); err != nil {
		t.Fatalf("failed to create job: %v", err)
	}

	// 2. Test GetJob
	retrievedJob, err := s.GetJob(ctx, "jb_1")
	if err != nil {
		t.Fatalf("failed to get job: %v", err)
	}
	if retrievedJob == nil {
		t.Fatal("job not found")
	}
	if retrievedJob.URL != job.URL {
		t.Errorf("expected URL %s, got %s", job.URL, retrievedJob.URL)
	}
	if retrievedJob.Tags["env"] != "test" {
		t.Errorf("expected tag env=test, got %s", retrievedJob.Tags["env"])
	}

	// 3. Test UpdateJobStatus
	errStr := "some error"
	if err := s.UpdateJobStatus(ctx, "jb_1", store.StatusFailed, &errStr); err != nil {
		t.Fatalf("failed to update status: %v", err)
	}

	updatedJob, _ := s.GetJob(ctx, "jb_1")
	if updatedJob.Status != store.StatusFailed {
		t.Errorf("expected status FAILED, got %s", updatedJob.Status)
	}
	if updatedJob.Error == nil || *updatedJob.Error != errStr {
		t.Errorf("expected error %s, got %v", errStr, updatedJob.Error)
	}
	if updatedJob.CompletedAt == nil {
		t.Error("expected CompletedAt to be set")
	}

	// 4. Test SaveResult
	res := &store.Result{
		ID:       "res_1",
		JobID:    "jb_1",
		RunIndex: 1,
		Network: &network.Result{
			TotalMS:    100.5,
			StatusCode: 200,
		},
		CollectedAt: time.Now().Truncate(time.Second),
	}

	if err := s.SaveResult(ctx, res); err != nil {
		t.Fatalf("failed to save result: %v", err)
	}

	// 5. Test GetResultsByJobID
	results, err := s.GetResultsByJobID(ctx, "jb_1")
	if err != nil {
		t.Fatalf("failed to get results: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Network.TotalMS != 100.5 {
		t.Errorf("expected TotalMS 100.5, got %v", results[0].Network.TotalMS)
	}

	// 6. Test ListJobs
	jobs, err := s.ListJobs(ctx, 10)
	if err != nil {
		t.Fatalf("failed to list jobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Errorf("expected 1 job in list, got %d", len(jobs))
	}
}
