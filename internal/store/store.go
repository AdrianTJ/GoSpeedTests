package store

import (
	"context"
	"time"

	"github.com/AdrianTJ/gospeedtest/internal/collector/network"
)

// JobStatus represents the current state of a test job.
type JobStatus string

const (
	StatusPending   JobStatus = "PENDING"
	StatusRunning   JobStatus = "RUNNING"
	StatusCompleted JobStatus = "COMPLETED"
	StatusFailed    JobStatus = "FAILED"
	StatusTimeout   JobStatus = "TIMEOUT"
)

// Job represents a single test request for a URL.
type Job struct {
	ID          string            `json:"id"`
	URL         string            `json:"url"`
	Status      JobStatus         `json:"status"`
	Tiers       []string          `json:"tiers"`
	Runs        int               `json:"runs"`
	TimeoutS    int               `json:"timeout_s"`
	Tags        map[string]string `json:"tags"`
	WebhookURL  string            `json:"webhook_url,omitempty"`
	Error       *string           `json:"error,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	StartedAt   *time.Time        `json:"started_at,omitempty"`
	CompletedAt *time.Time        `json:"completed_at,omitempty"`
}

// Result represents the metrics collected for a single run of a job.
type Result struct {
	ID          string          `json:"id"`
	JobID       string          `json:"job_id"`
	RunIndex    int             `json:"run_index"`
	Network     *network.Result `json:"network,omitempty"`
	Browser     interface{}     `json:"browser,omitempty"` // Placeholder for browser metrics
	Vitals      interface{}     `json:"vitals,omitempty"`  // Placeholder for Core Web Vitals
	CollectedAt time.Time       `json:"collected_at"`
}

// Store defines the interface for persisting jobs and results.
type Store interface {
	// Job operations
	CreateJob(ctx context.Context, job *Job) error
	GetJob(ctx context.Context, id string) (*Job, error)
	UpdateJobStatus(ctx context.Context, id string, status JobStatus, errStr *string) error
	ListJobs(ctx context.Context, limit int) ([]Job, error)

	// Result operations
	SaveResult(ctx context.Context, result *Result) error
	GetResultsByJobID(ctx context.Context, jobID string) ([]Result, error)

	// Analysis
	GetHistory(ctx context.Context, url string) (interface{}, error)

	// Deletion
	DeleteJob(ctx context.Context, id string) error

	// Maintenance
	Close() error
}
