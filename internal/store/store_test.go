package store

import (
	"testing"
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
