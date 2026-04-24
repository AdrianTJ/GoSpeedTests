package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/AdrianTJ/gospeedtest/internal/collector/network"
)

func TestSQLite_WALConcurrency(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "wal-test")
	defer os.RemoveAll(tmpDir)
	dbPath := filepath.Join(tmpDir, "test.db")

	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// 1. Start a heavy write operation in a goroutine
	var wg sync.WaitGroup
	wg.Add(1)
	writeStarted := make(chan bool)
	go func() {
		defer wg.Done()
		writeStarted <- true
		for i := 0; i < 100; i++ {
			s.CreateJob(ctx, &Job{
				ID:        fmt.Sprintf("job_%d", i),
				URL:       "http://example.com",
				Status:    StatusPending,
				CreatedAt: time.Now(),
				Tiers:     []string{"network"},
			})
			// Slight delay to keep the DB busy
			time.Sleep(10 * time.Millisecond)
		}
	}()

	<-writeStarted
	time.Sleep(50 * time.Millisecond) // Ensure writes are happening

	// 2. Attempt multiple concurrent reads
	for i := 0; i < 10; i++ {
		start := time.Now()
		_, err := s.ListJobs(ctx, 10)
		duration := time.Since(start)

		if err != nil {
			t.Errorf("Read %d failed during write: %v", i, err)
		}

		// WAL mode should allow nearly instant reads during writes
		if duration > 500*time.Millisecond {
			t.Errorf("Read %d was too slow (%.2fms), WAL mode might not be effective", i, float64(duration.Milliseconds()))
		}
	}

	wg.Wait()
}

func TestSQLite_ConcurrentResults(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "concur-results")
	defer os.RemoveAll(tmpDir)
	s, _ := NewStore(filepath.Join(tmpDir, "test.db"))
	defer s.Close()

	ctx := context.Background()
	jobID := "test_job"
	s.CreateJob(ctx, &Job{ID: jobID, URL: "http://test.com", CreatedAt: time.Now(), Tiers: []string{"network"}})

	// Save 50 results concurrently
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			s.SaveResult(ctx, &Result{
				ID:          fmt.Sprintf("res_%d", idx),
				JobID:       jobID,
				RunIndex:    idx,
				CollectedAt: time.Now(),
				Network:     &network.Result{TotalMS: 100.0},
			})
		}(i)
	}
	wg.Wait()

	results, err := s.GetResultsByJobID(ctx, jobID)
	if err != nil {
		t.Fatalf("failed to get results: %v", err)
	}
	if len(results) != 50 {
		t.Errorf("expected 50 results, got %d", len(results))
	}
}
