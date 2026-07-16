package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/AdrianTJ/gospeedtest/internal/budget"
	"github.com/AdrianTJ/gospeedtest/internal/collector/browser"
	"github.com/AdrianTJ/gospeedtest/internal/collector/lighthouse"
	"github.com/AdrianTJ/gospeedtest/internal/collector/network"
	"github.com/AdrianTJ/gospeedtest/internal/collector/vitals"
	"github.com/AdrianTJ/gospeedtest/internal/stats"
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
	ID           string             `json:"id"`
	URL          string             `json:"url"`
	Status       JobStatus          `json:"status"`
	Tiers        []string           `json:"tiers"`
	Runs         int                `json:"runs"`
	TimeoutS     int                `json:"timeout_s"`
	Tags         map[string]string  `json:"tags"`
	WebhookURL   string             `json:"webhook_url,omitempty"`
	Budget       *budget.Budget     `json:"budget,omitempty"`
	BudgetResult *budget.Evaluation `json:"budget_result,omitempty"`
	Error        *string            `json:"error,omitempty"`
	CreatedAt    time.Time          `json:"created_at"`
	StartedAt    *time.Time         `json:"started_at,omitempty"`
	CompletedAt  *time.Time         `json:"completed_at,omitempty"`
}

// Result represents the metrics collected for a single run of a job.
type Result struct {
	ID          string             `json:"id"`
	JobID       string             `json:"job_id"`
	RunIndex    int                `json:"run_index"`
	Network     *network.Result    `json:"network,omitempty"`
	Browser     *browser.Result    `json:"browser,omitempty"`
	Vitals      *vitals.Result     `json:"vitals,omitempty"`
	Lighthouse  *lighthouse.Result `json:"lighthouse,omitempty"`
	CollectedAt time.Time          `json:"collected_at"`
}

// MetricStats holds the descriptive statistics for one metric across runs.
// Web-perf convention scores at the 75th percentile; the average is kept for
// backward compatibility but P75 is the number to alert on.
type MetricStats struct {
	Count int     `json:"count"`
	Avg   float64 `json:"avg"`
	P50   float64 `json:"p50"`
	P75   float64 `json:"p75"`
	P95   float64 `json:"p95"`
}

// History summarizes aggregate metrics for all recorded runs of a URL.
// AvgTTFBMS/AvgTotalMS predate Metrics and are kept for backward
// compatibility; Metrics is keyed by the budget metric names
// (e.g. "network.ttfb_ms", "vitals.lcp_ms").
type History struct {
	URL        string                 `json:"url"`
	TestCount  int                    `json:"test_count"`
	AvgTTFBMS  float64                `json:"avg_ttfb_ms"`
	AvgTotalMS float64                `json:"avg_total_ms"`
	Metrics    map[string]MetricStats `json:"metrics"`
}

// WebhookDelivery tracks the state of a webhook notification.
type WebhookDelivery struct {
	ID          string     `json:"id"`
	JobID       string     `json:"job_id"`
	URL         string     `json:"url"`
	Payload     []byte     `json:"payload"`
	Attempts    int        `json:"attempts"`
	LastAttempt *time.Time `json:"last_attempt,omitempty"`
	NextAttempt *time.Time `json:"next_attempt,omitempty"`
	Status      string     `json:"status"` // PENDING, SUCCESS, FAILED
	CreatedAt   time.Time  `json:"created_at"`
}

// Store defines the interface for persisting jobs and results.
type Store interface {
	CreateJob(ctx context.Context, job *Job) error
	GetJob(ctx context.Context, id string) (*Job, error)
	UpdateJobStatus(ctx context.Context, id string, status JobStatus, errStr *string) error
	ListJobs(ctx context.Context, limit int) ([]Job, error)
	SaveResult(ctx context.Context, result *Result) error
	GetResultsByJobID(ctx context.Context, jobID string) ([]Result, error)
	GetHistory(ctx context.Context, url string) (*History, error)
	SetJobBudgetResult(ctx context.Context, id string, eval *budget.Evaluation) error
	DeleteJob(ctx context.Context, id string) error
	RecoverInterruptedJobs(ctx context.Context) (int, error)

	// Webhooks
	EnqueueWebhook(ctx context.Context, delivery *WebhookDelivery) error
	GetPendingWebhooks(ctx context.Context, limit int) ([]WebhookDelivery, error)
	GetWebhookByID(ctx context.Context, id string) (*WebhookDelivery, error)
	UpdateWebhookStatus(ctx context.Context, id string, status string, attempts int, lastAttempt *time.Time, nextAttempt *time.Time) error

	Close() error
}

type sqliteStore struct {
	db *sql.DB
}

// NewStore initializes a new SQLite store and creates the schema.
func NewStore(dsn string) (Store, error) {
	// Enable WAL mode for better concurrency, and foreign keys so that
	// ON DELETE CASCADE actually fires (SQLite leaves them off by default).
	db, err := sql.Open("sqlite3", dsn+"?_journal_mode=WAL&_synchronous=NORMAL&_foreign_keys=on")
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
		{
			Version: 4,
			SQL:     `ALTER TABLE results ADD COLUMN lighthouse TEXT`,
		},
		{
			Version: 5,
			SQL: `
			ALTER TABLE jobs ADD COLUMN budget TEXT;
			ALTER TABLE jobs ADD COLUMN budget_result TEXT;
			`,
		},
	}

	return migrations.Run(context.Background(), s.db, m)
}

func (s *sqliteStore) CreateJob(ctx context.Context, job *Job) error {
	tiersJSON, _ := json.Marshal(job.Tiers)
	tagsJSON, _ := json.Marshal(job.Tags)
	budgetJSON := marshalNullable(job.Budget)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO jobs (id, url, status, tiers, runs, timeout_s, tags, webhook_url, budget, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.URL, job.Status, string(tiersJSON), job.Runs, job.TimeoutS, string(tagsJSON), job.WebhookURL, budgetJSON, job.CreatedAt)
	return err
}

// marshalNullable JSON-encodes v for a nullable TEXT column: a nil pointer
// becomes SQL NULL rather than the string "null".
func marshalNullable(v any) any {
	data, err := json.Marshal(v)
	if err != nil || string(data) == "null" {
		return nil
	}
	return string(data)
}

// jobColumns is the canonical SELECT column list for a jobs row, kept in sync
// with scanJob's Scan order.
const jobColumns = "id, url, status, tiers, runs, timeout_s, tags, webhook_url, budget, budget_result, error, created_at, started_at, completed_at"

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanJob scans one jobs row into a Job, decoding the JSON/nullable columns.
func scanJob(sc rowScanner) (Job, error) {
	var job Job
	// webhook_url is NullString too: rows written before migration v2 added
	// the column carry SQL NULL, not the empty string.
	var tiersJSON, tagsJSON, webhookURL, budgetJSON, budgetResultJSON sql.NullString
	var startedAt, completedAt sql.NullTime

	if err := sc.Scan(
		&job.ID, &job.URL, &job.Status, &tiersJSON, &job.Runs, &job.TimeoutS, &tagsJSON, &webhookURL,
		&budgetJSON, &budgetResultJSON, &job.Error,
		&job.CreatedAt, &startedAt, &completedAt); err != nil {
		return job, err
	}
	job.WebhookURL = webhookURL.String

	if tiersJSON.Valid {
		_ = json.Unmarshal([]byte(tiersJSON.String), &job.Tiers)
	}
	if tagsJSON.Valid {
		_ = json.Unmarshal([]byte(tagsJSON.String), &job.Tags)
	}
	if budgetJSON.Valid {
		_ = json.Unmarshal([]byte(budgetJSON.String), &job.Budget)
	}
	if budgetResultJSON.Valid {
		_ = json.Unmarshal([]byte(budgetResultJSON.String), &job.BudgetResult)
	}
	if startedAt.Valid {
		job.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		job.CompletedAt = &completedAt.Time
	}
	return job, nil
}

func (s *sqliteStore) GetJob(ctx context.Context, id string) (*Job, error) {
	job, err := scanJob(s.db.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM jobs WHERE id = ?", id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
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
		"SELECT "+jobColumns+" FROM jobs ORDER BY created_at DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *sqliteStore) SaveResult(ctx context.Context, result *Result) error {
	networkJSON, _ := json.Marshal(result.Network)
	browserJSON, _ := json.Marshal(result.Browser)
	vitalsJSON, _ := json.Marshal(result.Vitals)
	lighthouseJSON, _ := json.Marshal(result.Lighthouse)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO results (id, job_id, run_index, network, browser, vitals, lighthouse, collected_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		result.ID, result.JobID, result.RunIndex, string(networkJSON), string(browserJSON), string(vitalsJSON), string(lighthouseJSON), result.CollectedAt)
	return err
}

func (s *sqliteStore) GetResultsByJobID(ctx context.Context, jobID string) ([]Result, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, job_id, run_index, network, browser, vitals, lighthouse, collected_at
		 FROM results WHERE job_id = ? ORDER BY run_index ASC`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []Result
	for rows.Next() {
		var res Result
		var networkJSON, browserJSON, vitalsJSON, lighthouseJSON sql.NullString

		err := rows.Scan(&res.ID, &res.JobID, &res.RunIndex, &networkJSON, &browserJSON, &vitalsJSON, &lighthouseJSON, &res.CollectedAt)
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
		if lighthouseJSON.Valid {
			_ = json.Unmarshal([]byte(lighthouseJSON.String), &res.Lighthouse)
		}

		results = append(results, res)
	}
	return results, nil
}

// historyLimit caps how many recent results GetHistory loads. Percentiles
// are computed in Go (SQLite has no percentile function), so the row set
// must be bounded.
const historyLimit = 1000

func (s *sqliteStore) GetHistory(ctx context.Context, url string) (*History, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.network, r.browser, r.vitals, r.lighthouse
		FROM results r
		JOIN jobs j ON r.job_id = j.id
		WHERE j.url = ?
		ORDER BY r.collected_at DESC
		LIMIT ?`, url, historyLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	values := make(map[string][]float64)
	count := 0
	for rows.Next() {
		var networkJSON, browserJSON, vitalsJSON, lighthouseJSON sql.NullString
		if err := rows.Scan(&networkJSON, &browserJSON, &vitalsJSON, &lighthouseJSON); err != nil {
			return nil, err
		}
		count++

		var n *network.Result
		var br *browser.Result
		var v *vitals.Result
		var lh *lighthouse.Result
		if networkJSON.Valid {
			_ = json.Unmarshal([]byte(networkJSON.String), &n)
		}
		if browserJSON.Valid {
			_ = json.Unmarshal([]byte(browserJSON.String), &br)
		}
		if vitalsJSON.Valid {
			_ = json.Unmarshal([]byte(vitalsJSON.String), &v)
		}
		if lighthouseJSON.Valid {
			_ = json.Unmarshal([]byte(lighthouseJSON.String), &lh)
		}
		for key, val := range budget.Flatten(n, br, v, lh) {
			values[key] = append(values[key], val)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	h := &History{URL: url, TestCount: count, Metrics: make(map[string]MetricStats, len(values))}
	for key, vs := range values {
		h.Metrics[key] = MetricStats{
			Count: len(vs),
			Avg:   stats.Mean(vs),
			P50:   stats.Percentile(vs, 50),
			P75:   stats.Percentile(vs, 75),
			P95:   stats.Percentile(vs, 95),
		}
	}
	// Legacy fields, kept for pre-percentile consumers.
	h.AvgTTFBMS = h.Metrics["network.ttfb_ms"].Avg
	h.AvgTotalMS = h.Metrics["network.total_ms"].Avg
	return h, nil
}

// SetJobBudgetResult persists the budget evaluation for a job after its
// runs complete.
func (s *sqliteStore) SetJobBudgetResult(ctx context.Context, id string, eval *budget.Evaluation) error {
	_, err := s.db.ExecContext(ctx, "UPDATE jobs SET budget_result = ? WHERE id = ?", marshalNullable(eval), id)
	return err
}

// RecoverInterruptedJobs fails any jobs left RUNNING or PENDING by a previous
// process (e.g. after a crash or restart). Without this, interrupted jobs would
// stay RUNNING/PENDING forever since no worker ever revisits them. Returns the
// number of rows recovered.
func (s *sqliteStore) RecoverInterruptedJobs(ctx context.Context) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET status = ?, error = ?, completed_at = ?
		 WHERE status IN (?, ?)`,
		StatusFailed, "interrupted by daemon restart", time.Now(), StatusRunning, StatusPending)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *sqliteStore) DeleteJob(ctx context.Context, id string) error {
	// results cascade via the foreign key, but webhook_deliveries has no FK,
	// so remove its rows explicitly to avoid orphaned deliveries.
	if _, err := s.db.ExecContext(ctx, "DELETE FROM webhook_deliveries WHERE job_id = ?", id); err != nil {
		return err
	}
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

func (s *sqliteStore) GetWebhookByID(ctx context.Context, id string) (*WebhookDelivery, error) {
	var d WebhookDelivery
	var last, next sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT id, job_id, url, payload, attempts, last_attempt, next_attempt, status, created_at
		 FROM webhook_deliveries WHERE id = ?`, id).Scan(
		&d.ID, &d.JobID, &d.URL, &d.Payload, &d.Attempts, &last, &next, &d.Status, &d.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if last.Valid {
		d.LastAttempt = &last.Time
	}
	if next.Valid {
		d.NextAttempt = &next.Time
	}
	return &d, nil
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
