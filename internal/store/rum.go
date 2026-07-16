package store

import (
	"context"
	"time"
)

// rumValuesLimit bounds how many recent events GetRUMValues loads; percentile
// math happens in Go, so the row set must be capped.
const rumValuesLimit = 10000

func (s *sqliteStore) SaveRUMEvent(ctx context.Context, e *RUMEvent) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO rum_events (id, url, metric, value, user_agent, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		e.ID, e.URL, e.Metric, e.Value, e.UserAgent, e.CreatedAt)
	return err
}

// GetRUMValues returns the per-metric values recorded for url since the given
// time, newest first, capped at rumValuesLimit rows.
func (s *sqliteStore) GetRUMValues(ctx context.Context, url string, since time.Time) (map[string][]float64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT metric, value FROM rum_events
		 WHERE url = ? AND created_at >= ?
		 ORDER BY created_at DESC LIMIT ?`, url, since, rumValuesLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	values := make(map[string][]float64)
	for rows.Next() {
		var metric string
		var value float64
		if err := rows.Scan(&metric, &value); err != nil {
			return nil, err
		}
		values[metric] = append(values[metric], value)
	}
	return values, rows.Err()
}

func (s *sqliteStore) PurgeOldRUMEvents(ctx context.Context, olderThan time.Time) (int, error) {
	res, err := s.db.ExecContext(ctx,
		"DELETE FROM rum_events WHERE created_at < ?", olderThan)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
