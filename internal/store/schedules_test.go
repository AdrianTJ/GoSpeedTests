package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AdrianTJ/loadstar/internal/budget"
	"github.com/AdrianTJ/loadstar/internal/collector/network"
)

func newScheduleTestStore(t *testing.T, name string) Store {
	t.Helper()
	tmpDir, _ := os.MkdirTemp("", name)
	t.Cleanup(func() { os.RemoveAll(tmpDir) })
	s, err := NewStore(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestScheduleCRUD(t *testing.T) {
	s := newScheduleTestStore(t, "schedule-crud")
	ctx := context.Background()
	now := time.Now()

	b := &budget.Budget{Assertions: map[string]budget.Assertion{
		"network.ttfb_ms": {Max: fptr(500)},
	}}
	sc := &Schedule{
		ID: "sc_test", URL: "http://example.com", Tiers: []string{"network"},
		Runs: 2, IntervalSeconds: 300, Budget: b, WebhookURL: "http://example.com/hook",
		Enabled: true, CreatedAt: now, NextRunAt: &now,
	}
	if err := s.CreateSchedule(ctx, sc); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := s.GetSchedule(ctx, "sc_test")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil || got.URL != sc.URL || got.Runs != 2 || got.IntervalSeconds != 300 ||
		!got.Enabled || got.WebhookURL != sc.WebhookURL {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if got.Budget == nil || got.Budget.Assertions["network.ttfb_ms"].Max == nil {
		t.Errorf("budget lost in round trip: %+v", got.Budget)
	}
	if got.NextRunAt == nil {
		t.Error("next_run_at lost in round trip")
	}
	if len(got.Tiers) != 1 || got.Tiers[0] != "network" {
		t.Errorf("tiers mismatch: %v", got.Tiers)
	}

	list, err := s.ListSchedules(ctx)
	if err != nil || len(list) != 1 {
		t.Errorf("list = %v (err %v), want 1 entry", list, err)
	}

	if err := s.SetScheduleEnabled(ctx, "sc_test", false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	got, _ = s.GetSchedule(ctx, "sc_test")
	if got.Enabled {
		t.Error("schedule still enabled after SetScheduleEnabled(false)")
	}

	if err := s.DeleteSchedule(ctx, "sc_test"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, _ = s.GetSchedule(ctx, "sc_test")
	if got != nil {
		t.Errorf("schedule still present after delete: %+v", got)
	}
}

func TestGetDueSchedules(t *testing.T) {
	s := newScheduleTestStore(t, "schedule-due")
	ctx := context.Background()
	now := time.Now()
	past := now.Add(-time.Minute)
	future := now.Add(time.Hour)

	mk := func(id string, enabled bool, next *time.Time) {
		if err := s.CreateSchedule(ctx, &Schedule{
			ID: id, URL: "http://example.com", Tiers: []string{"network"},
			Runs: 1, IntervalSeconds: 60, Enabled: enabled, CreatedAt: now, NextRunAt: next,
		}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	mk("sc_due", true, &past)
	mk("sc_exact", true, &now)
	mk("sc_future", true, &future)
	mk("sc_disabled", false, &past)
	mk("sc_nonext", true, nil)

	due, err := s.GetDueSchedules(ctx, now, 10)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	ids := map[string]bool{}
	for _, d := range due {
		ids[d.ID] = true
	}
	if !ids["sc_due"] || !ids["sc_exact"] {
		t.Errorf("expected sc_due and sc_exact due, got %v", ids)
	}
	if ids["sc_future"] || ids["sc_disabled"] || ids["sc_nonext"] {
		t.Errorf("future/disabled/no-next schedules must not be due, got %v", ids)
	}
}

func TestMarkScheduleRun(t *testing.T) {
	s := newScheduleTestStore(t, "schedule-mark")
	ctx := context.Background()
	now := time.Now()

	s.CreateSchedule(ctx, &Schedule{
		ID: "sc_mark", URL: "http://example.com", Tiers: []string{"network"},
		Runs: 1, IntervalSeconds: 60, Enabled: true, CreatedAt: now, NextRunAt: &now,
	})
	next := now.Add(time.Minute)
	if err := s.MarkScheduleRun(ctx, "sc_mark", now, next); err != nil {
		t.Fatalf("mark: %v", err)
	}
	got, _ := s.GetSchedule(ctx, "sc_mark")
	if got.LastRunAt == nil || got.NextRunAt == nil {
		t.Fatalf("timestamps not set: %+v", got)
	}
	if !got.NextRunAt.After(*got.LastRunAt) {
		t.Errorf("next_run_at (%v) should be after last_run_at (%v)", got.NextRunAt, got.LastRunAt)
	}
}

func TestCountActiveJobsForSchedule(t *testing.T) {
	s := newScheduleTestStore(t, "schedule-active")
	ctx := context.Background()

	mkJob := func(id string, status JobStatus, scheduleID string) {
		if err := s.CreateJob(ctx, &Job{
			ID: id, URL: "http://example.com", Status: status,
			Tiers: []string{"network"}, Runs: 1, ScheduleID: scheduleID, CreatedAt: time.Now(),
		}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	mkJob("jb_p", StatusPending, "sc_x")
	mkJob("jb_r", StatusRunning, "sc_x")
	mkJob("jb_c", StatusCompleted, "sc_x")
	mkJob("jb_other", StatusPending, "sc_y")
	mkJob("jb_manual", StatusPending, "")

	n, err := s.CountActiveJobsForSchedule(ctx, "sc_x")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Errorf("active jobs for sc_x = %d, want 2 (PENDING + RUNNING)", n)
	}
}

func TestPurgeOldJobs(t *testing.T) {
	s := newScheduleTestStore(t, "purge")
	ctx := context.Background()

	// Old completed job with a result and a webhook delivery.
	old := &Job{ID: "jb_old", URL: "http://example.com", Status: StatusCompleted,
		Tiers: []string{"network"}, Runs: 1, CreatedAt: time.Now().AddDate(0, 0, -10)}
	s.CreateJob(ctx, old)
	s.UpdateJobStatus(ctx, "jb_old", StatusCompleted, nil) // sets completed_at = now
	s.SaveResult(ctx, &Result{ID: "res_old", JobID: "jb_old", RunIndex: 1,
		Network: &network.Result{TTFBMS: 1}, CollectedAt: time.Now()})
	s.EnqueueWebhook(ctx, &WebhookDelivery{ID: "wh_old", JobID: "jb_old",
		URL: "http://example.com/hook", Payload: []byte("{}"), Status: "SUCCESS", CreatedAt: time.Now()})

	// Recent completed job.
	s.CreateJob(ctx, &Job{ID: "jb_new", URL: "http://example.com", Status: StatusCompleted,
		Tiers: []string{"network"}, Runs: 1, CreatedAt: time.Now()})
	s.UpdateJobStatus(ctx, "jb_new", StatusCompleted, nil)

	// Running job (no completed_at) — must never be purged.
	s.CreateJob(ctx, &Job{ID: "jb_running", URL: "http://example.com", Status: StatusRunning,
		Tiers: []string{"network"}, Runs: 1, CreatedAt: time.Now().AddDate(0, 0, -10)})

	// Purge everything completed before tomorrow: catches jb_old and jb_new
	// (both completed "now"), never jb_running.
	n, err := s.PurgeOldJobs(ctx, time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if n != 2 {
		t.Errorf("purged = %d, want 2", n)
	}

	if j, _ := s.GetJob(ctx, "jb_running"); j == nil {
		t.Error("running job was purged — retention must only touch terminal jobs")
	}
	if j, _ := s.GetJob(ctx, "jb_old"); j != nil {
		t.Error("old job survived purge")
	}
	// Children gone too.
	if results, _ := s.GetResultsByJobID(ctx, "jb_old"); len(results) != 0 {
		t.Errorf("results not cascaded: %v", results)
	}
	if wh, _ := s.GetWebhookByID(ctx, "wh_old"); wh != nil {
		t.Error("webhook delivery not purged")
	}

	// A purge with an old cutoff removes nothing.
	n, err = s.PurgeOldJobs(ctx, time.Now().AddDate(0, 0, -30))
	if err != nil || n != 0 {
		t.Errorf("no-op purge = %d (err %v), want 0", n, err)
	}
}
