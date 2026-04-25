package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/AdrianTJ/gospeedtest/internal/collector/network"
	"github.com/AdrianTJ/gospeedtest/internal/store/migrations"
	_ "github.com/mattn/go-sqlite3"
)

// JobStatus represents the current state of a test job.
type JobStatus string

const (
	StatusPending   JobStatus = "PENDING"
	StatusRunning   JobStatus = "RUNNING"
	StatusCompleted JobStatus = "COMPLETED"
	StatusPartial   JobStatus = "PARTIAL"
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
	Browser     interface{}     `json:"browser,omitempty"`
	Vitals      interface{}     `json:"vitals,omitempty"`
	CollectedAt time.Time       `json:"collected_at"`
}

// WebhookDelivery tracks the state of a webhook notification.
type WebhookDelivery struct {
	ID          string    `json:"id"`
	JobID       string    `json:"job_id"`
	URL         string    `json:"url"`
	Payload     []byte    `json:"payload"`
	Attempts    int       `json:"attempts"`
	LastAttempt *time.Time `json:"last_attempt,omitempty"`
	NextAttempt *time.Time `json:"next_attempt,omitempty"`
	Status      string    `json:"status"` // PENDING, SUCCESS, FAILED
	CreatedAt   time.Time `json:"created_at"`
}

// Store defines the interface for persisting jobs and results.
type Store interface {
	CreateJob(ctx context.Context, job *Job) error
	GetJob(ctx context.Context, id string) (*Job, error)
	UpdateJobStatus(ctx context.Context, id string, status JobStatus, errStr *string) error
	ListJobs(ctx context.Context, limit int) ([]Job, error)
	SaveResult(ctx context.Context, result *Result) error
	GetResultsByJobID(ctx context.Context, jobID string) ([]Result, error)
	GetHistory(ctx context.Context, url string) (interface{}, error)
	DeleteJob(ctx context.Context, id string) error

	// Webhooks
	EnqueueWebhook(ctx context.Context, delivery *WebhookDelivery) error
	GetPendingWebhooks(ctx context.Context, limit int) ([]WebhookDelivery, error)
	UpdateWebhookStatus(ctx context.Context, id string, status string, attempts int, lastAttempt *time.Time, nextAttempt *time.Time) error

	Close() error
}

type sqliteStore struct {
	db *sql.DB
}

// NewStore initializes a new SQLite store and creates the schema.
func NewStore(dsn string) (Store, error) {
	// Enable WAL mode for better concurrency
	db, err := sql.Open("sqlite3", dsn+"?_journal_mode=WAL&_synchronous=NORMAL")
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite db: %w", err)
	}

	s := &sqliteStore{db: db}
	if err := s.initSchema(); err != nil {
		db.Close()
		return nil, err
	}

	return s, nil
}

func (s *sqliteStore) initSchema() error {
	m := []migrations.Migration{
		{
			Version: 1,
			SQL: `
			CREATE TABLE IF NOT EXISTS jobs (
				id           TEXT        PRIMARY KEY,
				url          TEXT        NOT NULL,
				status       TEXT        NOT NULL DEFAULT 'PENDING',
				tiers        TEXT        NOT NULL,
				runs         INTEGER     NOT NULL DEFAULT 1,
				timeout_s    INTEGER     NOT NULL DEFAULT 60,
				tags         TEXT,
				error        TEXT,
				created_at   DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
				started_at   DATETIME,
				completed_at DATETIME
			);
			CREATE TABLE IF NOT EXISTS results (
				id           TEXT        PRIMARY KEY,
				job_id       TEXT        NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
				run_index    INTEGER     NOT NULL DEFAULT 1,
				network      TEXT,
				browser      TEXT,
				vitals       TEXT,
				collected_at DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP
			);
			CREATE INDEX IF NOT EXISTS idx_results_job_id  ON results(job_id);
			CREATE INDEX IF NOT EXISTS idx_jobs_url_status ON jobs(url, status);
			`,
		},
		{
			Version: 2,
			SQL:     `ALTER TABLE jobs ADD COLUMN webhook_url TEXT`,
		},
		{
			Version: 3,
			SQL: `
			CREATE TABLE IF NOT EXISTS webhook_deliveries (
				id           TEXT        PRIMARY KEY,
				job_id       TEXT        NOT NULL,
				url          TEXT        NOT NULL,
				payload      BLOB        NOT NULL,
				attempts     INTEGER     NOT NULL DEFAULT 0,
				last_attempt DATETIME,
				next_attempt DATETIME,
				status       TEXT        NOT NULL DEFAULT 'PENDING',
				created_at   DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP
			);
			CREATE INDEX IF NOT EXISTS idx_webhook_status_next ON webhook_deliveries(status, next_attempt);
			`,
		},
	}

	return migrations.Run(context.Background(), s.db, m)
}

func (s *sqliteStore) CreateJob(ctx context.Context, job *Job) error {
	tiersJSON, _ := json.Marshal(job.Tiers)
	tagsJSON, _ := json.Marshal(job.Tags)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO jobs (id, url, status, tiers, runs, timeout_s, tags, webhook_url, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.URL, job.Status, string(tiersJSON), job.Runs, job.TimeoutS, string(tagsJSON), job.WebhookURL, job.CreatedAt)
	return err
}

func (s *sqliteStore) GetJob(ctx context.Context, id string) (*Job, error) {
	var job Job
	var tiersJSON, tagsJSON sql.NullString
	var startedAt, completedAt sql.NullTime

	err := s.db.QueryRowContext(ctx,
		`SELECT id, url, status, tiers, runs, timeout_s, tags, webhook_url, error, created_at, started_at, completed_at
		 FROM jobs WHERE id = ?`, id).Scan(
		&job.ID, &job.URL, &job.Status, &tiersJSON, &job.Runs, &job.TimeoutS, &tagsJSON, &job.WebhookURL, &job.Error,
		&job.CreatedAt, &startedAt, &completedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if tiersJSON.Valid {
		_ = json.Unmarshal([]byte(tiersJSON.String), &job.Tiers)
	}
	if tagsJSON.Valid {
		_ = json.Unmarshal([]byte(tagsJSON.String), &job.Tags)
	}
	if startedAt.Valid {
		job.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		job.CompletedAt = &completedAt.Time
	}

	return &job, nil
}

func (s *sqliteStore) UpdateJobStatus(ctx context.Context, id string, status JobStatus, errStr *string) error {
	now := time.Now()
	var err error
	if status == StatusRunning {
		_, err = s.db.ExecContext(ctx, "UPDATE jobs SET status = ?, started_at = ? WHERE id = ?", status, now, id)
	} else if status == StatusCompleted || status == StatusFailed || status == StatusTimeout || status == StatusPartial {
		_, err = s.db.ExecContext(ctx, "UPDATE jobs SET status = ?, completed_at = ?, error = ? WHERE id = ?", status, now, errStr, id)
	} else {
		_, err = s.db.ExecContext(ctx, "UPDATE jobs SET status = ? WHERE id = ?", status, id)
	}
	return err
}

func (s *sqliteStore) ListJobs(ctx context.Context, limit int) ([]Job, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, url, status, tiers, runs, timeout_s, tags, webhook_url, error, created_at, started_at, completed_at
		 FROM jobs ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []Job
	for rows.Next() {
		var job Job
		var tiersJSON, tagsJSON sql.NullString
		var startedAt, completedAt sql.NullTime

		err := rows.Scan(
			&job.ID, &job.URL, &job.Status, &tiersJSON, &job.Runs, &job.TimeoutS, &tagsJSON, &job.WebhookURL, &job.Error,
			&job.CreatedAt, &startedAt, &completedAt)
		if err != nil {
			return nil, err
		}

		if tiersJSON.Valid {
			_ = json.Unmarshal([]byte(tiersJSON.String), &job.Tiers)
		}
		if tagsJSON.Valid {
			_ = json.Unmarshal([]byte(tagsJSON.String), &job.Tags)
		}
		if startedAt.Valid {
			job.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			job.CompletedAt = &completedAt.Time
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (s *sqliteStore) SaveResult(ctx context.Context, result *Result) error {
	networkJSON, _ := json.Marshal(result.Network)
	browserJSON, _ := json.Marshal(result.Browser)
	vitalsJSON, _ := json.Marshal(result.Vitals)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO results (id, job_id, run_index, network, browser, vitals, collected_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		result.ID, result.JobID, result.RunIndex, string(networkJSON), string(browserJSON), string(vitalsJSON), result.CollectedAt)
	return err
}

func (s *sqliteStore) GetResultsByJobID(ctx context.Context, jobID string) ([]Result, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, job_id, run_index, network, browser, vitals, collected_at
		 FROM results WHERE job_id = ? ORDER BY run_index ASC`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []Result
	for rows.Next() {
		var res Result
		var networkJSON, browserJSON, vitalsJSON sql.NullString

		err := rows.Scan(&res.ID, &res.JobID, &res.RunIndex, &networkJSON, &browserJSON, &vitalsJSON, &res.CollectedAt)
		if err != nil {
			return nil, err
		}

		if networkJSON.Valid {
			_ = json.Unmarshal([]byte(networkJSON.String), &res.Network)
		}
		if browserJSON.Valid {
			_ = json.Unmarshal([]byte(browserJSON.String), &res.Browser)
		}
		if vitalsJSON.Valid {
			_ = json.Unmarshal([]byte(vitalsJSON.String), &res.Vitals)
		}

		results = append(results, res)
	}
	return results, nil
}

func (s *sqliteStore) GetHistory(ctx context.Context, url string) (interface{}, error) {
	query := `
		SELECT 
			COUNT(*) as test_count,
			ROUND(AVG(json_extract(r.network, '$.ttfb_ms')), 2) as avg_ttfb_ms,
			ROUND(AVG(json_extract(r.network, '$.total_ms')), 2) as avg_total_ms
		FROM results r
		JOIN jobs j ON r.job_id = j.id
		WHERE j.url = ?
	`
	var count int
	var ttfb, total sql.NullFloat64

	err := s.db.QueryRowContext(ctx, query, url).Scan(&count, &ttfb, &total)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"url":          url,
		"test_count":   count,
		"avg_ttfb_ms":  ttfb.Float64,
		"avg_total_ms": total.Float64,
	}, nil
}

func (s *sqliteStore) DeleteJob(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM jobs WHERE id = ?", id)
	return err
}

func (s *sqliteStore) EnqueueWebhook(ctx context.Context, d *WebhookDelivery) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO webhook_deliveries (id, job_id, url, payload, attempts, last_attempt, next_attempt, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.JobID, d.URL, d.Payload, d.Attempts, d.LastAttempt, d.NextAttempt, d.Status, d.CreatedAt)
	return err
}

func (s *sqliteStore) GetPendingWebhooks(ctx context.Context, limit int) ([]WebhookDelivery, error) {
	now := time.Now()
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, job_id, url, payload, attempts, last_attempt, next_attempt, status, created_at
		 FROM webhook_deliveries 
		 WHERE status = 'PENDING' AND (next_attempt IS NULL OR next_attempt <= ?)
		 ORDER BY created_at ASC LIMIT ?`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deliveries []WebhookDelivery
	for rows.Next() {
		var d WebhookDelivery
		var last, next sql.NullTime
		err := rows.Scan(&d.ID, &d.JobID, &d.URL, &d.Payload, &d.Attempts, &last, &next, &d.Status, &d.CreatedAt)
		if err != nil {
			return nil, err
		}
		if last.Valid {
			d.LastAttempt = &last.Time
		}
		if next.Valid {
			d.NextAttempt = &next.Time
		}
		deliveries = append(deliveries, d)
	}
	return deliveries, nil
}

func (s *sqliteStore) UpdateWebhookStatus(ctx context.Context, id string, status string, attempts int, last *time.Time, next *time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE webhook_deliveries SET status = ?, attempts = ?, last_attempt = ?, next_attempt = ? WHERE id = ?`,
		status, attempts, last, next, id)
	return err
}

func (s *sqliteStore) Close() error {
	return s.db.Close()
}
