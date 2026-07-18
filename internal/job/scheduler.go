package job

import (
	"log/slog"
	"time"
)

const (
	schedulerTickRate  = 15 * time.Second
	schedulerBatchSize = 20
	// MinScheduleIntervalS is the smallest allowed schedule interval; the API
	// enforces it at creation time.
	MinScheduleIntervalS = 60
)

// StartScheduler launches the schedule loop. Call after Start; it shuts down
// with the manager (ctx cancellation + wg).
func (m *Manager) StartScheduler() {
	m.wg.Add(1)
	go m.schedulerLoop()
}

func (m *Manager) schedulerLoop() {
	defer m.wg.Done()
	slog.Info("Scheduler started", "tick", schedulerTickRate.String())

	ticker := time.NewTicker(schedulerTickRate)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			slog.Info("Scheduler shutting down")
			return
		case <-ticker.C:
			m.runDueSchedules(time.Now())
		}
	}
}

// runDueSchedules submits a job for every enabled schedule whose next_run_at
// has passed. It takes now as a parameter so tests can drive it directly.
//
// next_run_at always advances — even when the tick is skipped or the submit
// fails — so a schedule can never hot-loop on every tick; the worst case is
// one dropped datapoint per interval, which is visible in the
// loadstar_scheduler_runs_total{result="skipped"|"error"} counters.
func (m *Manager) runDueSchedules(now time.Time) {
	due, err := m.store.GetDueSchedules(m.ctx, now, schedulerBatchSize)
	if err != nil {
		slog.Error("Scheduler failed to query due schedules", "error", err)
		return
	}

	for _, sc := range due {
		next := now.Add(time.Duration(sc.IntervalSeconds) * time.Second)

		// Don't pile a new job onto a schedule whose previous job is still
		// queued or running (target slower than the interval).
		active, err := m.store.CountActiveJobsForSchedule(m.ctx, sc.ID)
		if err != nil {
			slog.Error("Scheduler failed to count active jobs", "schedule_id", sc.ID, "error", err)
			continue
		}
		if active > 0 {
			slog.Warn("Scheduler skipping interval: previous job still active",
				"schedule_id", sc.ID, "url", sc.URL, "active_jobs", active)
			m.metrics.CounterInc("loadstar_scheduler_runs_total", "result", "skipped")
			if err := m.store.MarkScheduleRun(m.ctx, sc.ID, now, next); err != nil {
				// The schedule stays due and will be retried next tick.
				slog.Error("Scheduler failed to advance skipped schedule", "schedule_id", sc.ID, "error", err)
			}
			continue
		}

		_, err = m.SubmitJob(m.ctx, SubmitOptions{
			URL:        sc.URL,
			Tiers:      sc.Tiers,
			Runs:       sc.Runs,
			WebhookURL: sc.WebhookURL,
			Budget:     sc.Budget,
			ScheduleID: sc.ID,
			Profile:    sc.Profile,
		})
		if err != nil {
			slog.Error("Scheduler failed to submit job", "schedule_id", sc.ID, "url", sc.URL, "error", err)
			m.metrics.CounterInc("loadstar_scheduler_runs_total", "result", "error")
		} else {
			m.metrics.CounterInc("loadstar_scheduler_runs_total", "result", "submitted")
		}
		if err := m.store.MarkScheduleRun(m.ctx, sc.ID, now, next); err != nil {
			slog.Error("Scheduler failed to advance schedule", "schedule_id", sc.ID, "error", err)
		}
	}
}
