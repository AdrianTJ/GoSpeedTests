package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/lib/pq"
	"github.com/AdrianTJ/gospeedtest/internal/store"
)

type pgStore struct {
	db *sql.DB
}

// NewStore initializes a new Postgres store and creates the schema.
func NewStore(dsn string) (store.Store, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres db: %w", err)
	}

	// Verify connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	s := &pgStore{db: db}
	if err := s.initSchema(); err != nil {
		db.Close()
		return nil, err
	}

	return s, nil
}

func (s *pgStore) initSchema() error {
	const schema = `
	CREATE TABLE IF NOT EXISTS jobs (
		id           TEXT        PRIMARY KEY,
		url          TEXT        NOT NULL,
		status       TEXT        NOT NULL DEFAULT 'PENDING',
		tiers        JSONB       NOT NULL,
		runs         INTEGER     NOT NULL DEFAULT 1,
		timeout_s    INTEGER     NOT NULL DEFAULT 60,
		tags         JSONB,
		webhook_url  TEXT,
		error        TEXT,
		created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		started_at   TIMESTAMPTZ,
		completed_at TIMESTAMPTZ
	);

	CREATE TABLE IF NOT EXISTS results (
		id           TEXT        PRIMARY KEY,
		job_id       TEXT        NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
		run_index    INTEGER     NOT NULL DEFAULT 1,
		network      JSONB,
		browser      JSONB,
		vitals       JSONB,
		collected_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_results_job_id  ON results(job_id);
	CREATE INDEX IF NOT EXISTS idx_jobs_url_status ON jobs(url, status);
	`
	_, err := s.db.Exec(schema)
	return err
}

func (s *pgStore) CreateJob(ctx context.Context, job *store.Job) error {
	tiersJSON, _ := json.Marshal(job.Tiers)
	tagsJSON, _ := json.Marshal(job.Tags)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO jobs (id, url, status, tiers, runs, timeout_s, tags, webhook_url, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		job.ID, job.URL, job.Status, tiersJSON, job.Runs, job.TimeoutS, tagsJSON, job.WebhookURL, job.CreatedAt)
	return err
}

func (s *pgStore) GetJob(ctx context.Context, id string) (*store.Job, error) {
	var job store.Job
	var tiersRaw, tagsRaw []byte
	var startedAt, completedAt sql.NullTime

	err := s.db.QueryRowContext(ctx,
		`SELECT id, url, status, tiers, runs, timeout_s, tags, webhook_url, error, created_at, started_at, completed_at
		 FROM jobs WHERE id = $1`, id).Scan(
		&job.ID, &job.URL, &job.Status, &tiersRaw, &job.Runs, &job.TimeoutS, &tagsRaw, &job.WebhookURL, &job.Error,
		&job.CreatedAt, &startedAt, &completedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	_ = json.Unmarshal(tiersRaw, &job.Tiers)
	_ = json.Unmarshal(tagsRaw, &job.Tags)
	if startedAt.Valid {
		job.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		job.CompletedAt = &completedAt.Time
	}

	return &job, nil
}

func (s *pgStore) UpdateJobStatus(ctx context.Context, id string, status store.JobStatus, errStr *string) error {
	now := time.Now()
	var err error
	if status == store.StatusRunning {
		_, err = s.db.ExecContext(ctx, "UPDATE jobs SET status = $1, started_at = $2 WHERE id = $3", status, now, id)
	} else if status == store.StatusCompleted || status == store.StatusFailed || status == store.StatusTimeout {
		_, err = s.db.ExecContext(ctx, "UPDATE jobs SET status = $1, completed_at = $2, error = $3 WHERE id = $4", status, now, errStr, id)
	} else {
		_, err = s.db.ExecContext(ctx, "UPDATE jobs SET status = $1 WHERE id = $2", status, id)
	}
	return err
}

func (s *pgStore) ListJobs(ctx context.Context, limit int) ([]store.Job, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, url, status, tiers, runs, timeout_s, tags, webhook_url, error, created_at, started_at, completed_at
		 FROM jobs ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []store.Job
	for rows.Next() {
		var job store.Job
		var tiersRaw, tagsRaw []byte
		var startedAt, completedAt sql.NullTime

		err := rows.Scan(
			&job.ID, &job.URL, &job.Status, &tiersRaw, &job.Runs, &job.TimeoutS, &tagsRaw, &job.WebhookURL, &job.Error,
			&job.CreatedAt, &startedAt, &completedAt)
		if err != nil {
			return nil, err
		}

		_ = json.Unmarshal(tiersRaw, &job.Tiers)
		_ = json.Unmarshal(tagsRaw, &job.Tags)
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

func (s *pgStore) SaveResult(ctx context.Context, result *store.Result) error {
	networkJSON, _ := json.Marshal(result.Network)
	browserJSON, _ := json.Marshal(result.Browser)
	vitalsJSON, _ := json.Marshal(result.Vitals)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO results (id, job_id, run_index, network, browser, vitals, collected_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		result.ID, result.JobID, result.RunIndex, networkJSON, browserJSON, vitalsJSON, result.CollectedAt)
	return err
}

func (s *pgStore) GetResultsByJobID(ctx context.Context, jobID string) ([]store.Result, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, job_id, run_index, network, browser, vitals, collected_at
		 FROM results WHERE job_id = $1 ORDER BY run_index ASC`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []store.Result
	for rows.Next() {
		var res store.Result
		var networkRaw, browserRaw, vitalsRaw []byte

		err := rows.Scan(&res.ID, &res.JobID, &res.RunIndex, &networkRaw, &browserRaw, &vitalsRaw, &res.CollectedAt)
		if err != nil {
			return nil, err
		}

		if networkRaw != nil {
			_ = json.Unmarshal(networkRaw, &res.Network)
		}
		if browserRaw != nil {
			_ = json.Unmarshal(browserRaw, &res.Browser)
		}
		if vitalsRaw != nil {
			_ = json.Unmarshal(vitalsRaw, &res.Vitals)
		}

		results = append(results, res)
	}
	return results, nil
}

func (s *pgStore) GetHistory(ctx context.Context, url string) (interface{}, error) {
	query := `
		SELECT 
			COUNT(*) as test_count,
			ROUND(AVG((network->>'ttfb_ms')::numeric), 2) as avg_ttfb_ms,
			ROUND(AVG((network->>'total_ms')::numeric), 2) as avg_total_ms
		FROM results r
		JOIN jobs j ON r.job_id = j.id
		WHERE j.url = $1
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

func (s *pgStore) DeleteJob(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM jobs WHERE id = $1", id)
	return err
}

func (s *pgStore) Close() error {
	return s.db.Close()
}
