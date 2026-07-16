package job

import (
	"log/slog"
	"time"
)

const retentionTickRate = time.Hour

// StartRetention launches the retention loop, purging terminal jobs (and
// their results/webhook deliveries) completed more than days ago. days <= 0
// disables retention entirely (keep forever) — deletion is strictly opt-in so
// an upgrade never silently discards existing data. One purge runs
// immediately at startup, then hourly.
func (m *Manager) StartRetention(days int) {
	if days <= 0 {
		return
	}
	m.wg.Add(1)
	go m.retentionLoop(days)
}

func (m *Manager) retentionLoop(days int) {
	defer m.wg.Done()
	slog.Info("Retention started", "days", days)

	ticker := time.NewTicker(retentionTickRate)
	defer ticker.Stop()

	m.runRetention(days)
	for {
		select {
		case <-m.ctx.Done():
			slog.Info("Retention shutting down")
			return
		case <-ticker.C:
			m.runRetention(days)
		}
	}
}

func (m *Manager) runRetention(days int) {
	cutoff := time.Now().AddDate(0, 0, -days)
	n, err := m.store.PurgeOldJobs(m.ctx, cutoff)
	if err != nil {
		slog.Error("Retention purge failed", "error", err)
		return
	}
	if n > 0 {
		slog.Info("Retention purge", "purged_jobs", n, "cutoff", cutoff.Format(time.RFC3339))
	}
	m.metrics.CounterAdd("gost_retention_purged_total", float64(n), "kind", "jobs")
}
