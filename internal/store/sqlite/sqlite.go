package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/AdrianTJ/gospeedtest/internal/store"
	"github.com/AdrianTJ/gospeedtest/internal/store/migrations"
)

type sqliteStore struct {
	db *sql.DB
}

// NewStore initializes a new SQLite store and creates the schema.
func NewStore(dsn string) (store.Store, error) {
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
	}

	return migrations.Run(context.Background(), s.db, "sqlite", m)
}

func (s *sqliteStore) CreateJob(ctx context.Context, job *store.Job) error {
	tiersJSON, _ := json.Marshal(job.Tiers)
	tagsJSON, _ := json.Marshal(job.Tags)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO jobs (id, url, status, tiers, runs, timeout_s, tags, webhook_url, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.URL, job.Status, string(tiersJSON), job.Runs, job.TimeoutS, string(tagsJSON), job.WebhookURL, job.CreatedAt)
	return err
}

func (s *sqliteStore) GetJob(ctx context.Context, id string) (*store.Job, error) {
	var job store.Job
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

func (s *sqliteStore) UpdateJobStatus(ctx context.Context, id string, status store.JobStatus, errStr *string) error {
	now := time.Now()
	var err error
	if status == store.StatusRunning {
		_, err = s.db.ExecContext(ctx, "UPDATE jobs SET status = ?, started_at = ? WHERE id = ?", status, now, id)
	} else if status == store.StatusCompleted || status == store.StatusFailed || status == store.StatusTimeout {
		_, err = s.db.ExecContext(ctx, "UPDATE jobs SET status = ?, completed_at = ?, error = ? WHERE id = ?", status, now, errStr, id)
	} else {
		_, err = s.db.ExecContext(ctx, "UPDATE jobs SET status = ? WHERE id = ?", status, id)
	}
	return err
}

func (s *sqliteStore) ListJobs(ctx context.Context, limit int) ([]store.Job, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, url, status, tiers, runs, timeout_s, tags, webhook_url, error, created_at, started_at, completed_at
		 FROM jobs ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []store.Job
	for rows.Next() {
		var job store.Job
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

func (s *sqliteStore) SaveResult(ctx context.Context, result *store.Result) error {
	networkJSON, _ := json.Marshal(result.Network)
	browserJSON, _ := json.Marshal(result.Browser)
	vitalsJSON, _ := json.Marshal(result.Vitals)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO results (id, job_id, run_index, network, browser, vitals, collected_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		result.ID, result.JobID, result.RunIndex, string(networkJSON), string(browserJSON), string(vitalsJSON), result.CollectedAt)
	return err
}

func (s *sqliteStore) GetResultsByJobID(ctx context.Context, jobID string) ([]store.Result, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, job_id, run_index, network, browser, vitals, collected_at
		 FROM results WHERE job_id = ? ORDER BY run_index ASC`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []store.Result
	for rows.Next() {
		var res store.Result
		var networkJSON, browserJSON, vitalsJSON sql.NullString

		err := rows.Scan(&res.ID, &res.JobID, &res.RunIndex, &networkJSON, &browserJSON, &vitalsJSON, &res.CollectedAt)
		if err != nil {
			return nil, err
		}

		if networkJSON.Valid {
			_ = json.Unmarshal([]byte(networkJSON.String), &res.Network)
		}
		// Browser and Vitals placeholders
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

func (s *sqliteStore) Close() error {
	return s.db.Close()
}
