package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

const scheduleColumns = "id, url, tiers, runs, interval_seconds, budget, webhook_url, profile, enabled, created_at, last_run_at, next_run_at"

func scanSchedule(sc rowScanner) (Schedule, error) {
	var s Schedule
	var tiersJSON string
	var budgetJSON, webhookURL, profileName sql.NullString
	var lastRun, nextRun sql.NullTime

	if err := sc.Scan(&s.ID, &s.URL, &tiersJSON, &s.Runs, &s.IntervalSeconds,
		&budgetJSON, &webhookURL, &profileName, &s.Enabled, &s.CreatedAt, &lastRun, &nextRun); err != nil {
		return s, err
	}
	_ = json.Unmarshal([]byte(tiersJSON), &s.Tiers)
	if budgetJSON.Valid {
		_ = json.Unmarshal([]byte(budgetJSON.String), &s.Budget)
	}
	s.WebhookURL = webhookURL.String
	s.Profile = profileName.String
	if lastRun.Valid {
		s.LastRunAt = &lastRun.Time
	}
	if nextRun.Valid {
		s.NextRunAt = &nextRun.Time
	}
	return s, nil
}

func (s *sqliteStore) CreateSchedule(ctx context.Context, sc *Schedule) error {
	tiersJSON, _ := json.Marshal(sc.Tiers)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO schedules (id, url, tiers, runs, interval_seconds, budget, webhook_url, profile, enabled, created_at, last_run_at, next_run_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sc.ID, sc.URL, string(tiersJSON), sc.Runs, sc.IntervalSeconds,
		marshalNullable(sc.Budget), sc.WebhookURL, sc.Profile, sc.Enabled, sc.CreatedAt, sc.LastRunAt, sc.NextRunAt)
	return err
}

func (s *sqliteStore) GetSchedule(ctx context.Context, id string) (*Schedule, error) {
	sc, err := scanSchedule(s.db.QueryRowContext(ctx,
		"SELECT "+scheduleColumns+" FROM schedules WHERE id = ?", id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sc, nil
}

func (s *sqliteStore) ListSchedules(ctx context.Context) ([]Schedule, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+scheduleColumns+" FROM schedules ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schedules []Schedule
	for rows.Next() {
		sc, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, sc)
	}
	return schedules, rows.Err()
}

func (s *sqliteStore) DeleteSchedule(ctx context.Context, id string) error {
	// Historical jobs keep their schedule_id on purpose (no FK) — deleting a
	// schedule must not erase the data it produced.
	_, err := s.db.ExecContext(ctx, "DELETE FROM schedules WHERE id = ?", id)
	return err
}

func (s *sqliteStore) SetScheduleEnabled(ctx context.Context, id string, enabled bool) error {
	_, err := s.db.ExecContext(ctx, "UPDATE schedules SET enabled = ? WHERE id = ?", enabled, id)
	return err
}

func (s *sqliteStore) GetDueSchedules(ctx context.Context, now time.Time, limit int) ([]Schedule, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+scheduleColumns+` FROM schedules
		 WHERE enabled = 1 AND next_run_at IS NOT NULL AND next_run_at <= ?
		 ORDER BY next_run_at ASC LIMIT ?`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var due []Schedule
	for rows.Next() {
		sc, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		due = append(due, sc)
	}
	return due, rows.Err()
}

func (s *sqliteStore) MarkScheduleRun(ctx context.Context, id string, lastRun, nextRun time.Time) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE schedules SET last_run_at = ?, next_run_at = ? WHERE id = ?", lastRun, nextRun, id)
	return err
}

func (s *sqliteStore) CountActiveJobsForSchedule(ctx context.Context, scheduleID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM jobs WHERE schedule_id = ? AND status IN (?, ?)",
		scheduleID, StatusPending, StatusRunning).Scan(&n)
	return n, err
}

// PurgeOldJobs deletes terminal jobs completed before the cutoff, along with
// their results (FK cascade) and webhook deliveries (explicit — no FK).
// RUNNING/PENDING jobs have no completed_at, so they are inherently safe.
func (s *sqliteStore) PurgeOldJobs(ctx context.Context, olderThan time.Time) (int, error) {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM webhook_deliveries WHERE job_id IN
		 (SELECT id FROM jobs WHERE completed_at IS NOT NULL AND completed_at < ?)`, olderThan); err != nil {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx,
		"DELETE FROM jobs WHERE completed_at IS NOT NULL AND completed_at < ?", olderThan)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
