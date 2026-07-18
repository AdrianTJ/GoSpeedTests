package job

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AdrianTJ/loadstar/internal/store"
)

func newSchedulerFixture(t *testing.T, name string) (*Manager, store.Store) {
	t.Helper()
	tmpDir, _ := os.MkdirTemp("", name)
	t.Cleanup(func() { os.RemoveAll(tmpDir) })
	s, err := store.NewStore(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	m := NewManager(s, 1, 10, "")
	m.Start()
	t.Cleanup(func() { m.Stop() })
	return m, s
}

func mkSchedule(t *testing.T, s store.Store, id string, nextRun time.Time, url string) {
	t.Helper()
	if err := s.CreateSchedule(context.Background(), &store.Schedule{
		ID: id, URL: url, Tiers: []string{"network"}, Runs: 1,
		IntervalSeconds: 60, Enabled: true, CreatedAt: time.Now(), NextRunAt: &nextRun,
	}); err != nil {
		t.Fatalf("create schedule: %v", err)
	}
}

// TestRunDueSchedules_SubmitsAndAdvances drives the scheduler directly (no
// ticker) and verifies a due schedule spawns a job tagged with its ID and
// next_run_at moves forward.
func TestRunDueSchedules_SubmitsAndAdvances(t *testing.T) {
	t.Setenv("LOADSTAR_ALLOW_PRIVATE_IPS", "true")
	m, s := newSchedulerFixture(t, "sched-submit")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	now := time.Now()
	mkSchedule(t, s, "sc_due", now.Add(-time.Second), ts.URL)

	m.runDueSchedules(now)

	// A job tagged with the schedule must exist.
	jobs, _ := s.ListJobs(context.Background(), 10)
	var scheduled *store.Job
	for i := range jobs {
		if jobs[i].ScheduleID == "sc_due" {
			scheduled = &jobs[i]
			break
		}
	}
	if scheduled == nil {
		t.Fatalf("no job with schedule_id=sc_due; jobs: %+v", jobs)
	}

	// next_run_at advanced by the interval.
	sc, _ := s.GetSchedule(context.Background(), "sc_due")
	if sc.LastRunAt == nil || sc.NextRunAt == nil {
		t.Fatalf("timestamps not set: %+v", sc)
	}
	if got := sc.NextRunAt.Sub(*sc.LastRunAt); got != 60*time.Second {
		t.Errorf("next-last = %v, want 60s", got)
	}

	// Running again immediately must do nothing (not due anymore).
	before, _ := s.ListJobs(context.Background(), 50)
	m.runDueSchedules(now.Add(time.Second))
	after, _ := s.ListJobs(context.Background(), 50)
	if len(after) != len(before) {
		t.Errorf("non-due schedule spawned a job: %d -> %d", len(before), len(after))
	}
}

// TestRunDueSchedules_SkipsWhenActive pins the no-pileup rule: if the previous
// scheduled job is still PENDING/RUNNING, the interval is skipped but
// next_run_at still advances (so the schedule can't hot-loop every tick).
func TestRunDueSchedules_SkipsWhenActive(t *testing.T) {
	m, s := newSchedulerFixture(t, "sched-skip")
	ctx := context.Background()

	now := time.Now()
	mkSchedule(t, s, "sc_busy", now.Add(-time.Second), "http://example.com")

	// Plant an active job for this schedule directly in the store.
	if err := s.CreateJob(ctx, &store.Job{
		ID: "jb_active", URL: "http://example.com", Status: store.StatusRunning,
		Tiers: []string{"network"}, Runs: 1, ScheduleID: "sc_busy", CreatedAt: now,
	}); err != nil {
		t.Fatalf("plant job: %v", err)
	}

	m.runDueSchedules(now)

	// No new job spawned.
	jobs, _ := s.ListJobs(ctx, 10)
	count := 0
	for _, j := range jobs {
		if j.ScheduleID == "sc_busy" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("jobs for sc_busy = %d, want 1 (skip while active)", count)
	}

	// next_run_at advanced anyway.
	sc, _ := s.GetSchedule(ctx, "sc_busy")
	if sc.NextRunAt == nil || !sc.NextRunAt.After(now) {
		t.Errorf("next_run_at did not advance on skip: %+v", sc.NextRunAt)
	}
}

// TestRunDueSchedules_QueueFullAdvances pins that a failed submission (queue
// full) still advances the schedule rather than retrying every tick.
func TestRunDueSchedules_QueueFullAdvances(t *testing.T) {
	t.Setenv("LOADSTAR_ALLOW_PRIVATE_IPS", "true")
	tmpDir, _ := os.MkdirTemp("", "sched-full")
	t.Cleanup(func() { os.RemoveAll(tmpDir) })
	s, _ := store.NewStore(filepath.Join(tmpDir, "test.db"))
	t.Cleanup(func() { s.Close() })

	// Queue depth 1 and NO workers started: the queue jams after one submit.
	m := NewManager(s, 1, 1, "")
	t.Cleanup(func() { m.Stop() })

	ctx := context.Background()
	now := time.Now()

	// Jam the queue.
	if _, err := m.Submit(ctx, "http://example.com", []string{"network"}, 1, ""); err != nil {
		t.Fatalf("first submit should fill the queue: %v", err)
	}

	mkSchedule(t, s, "sc_full", now.Add(-time.Second), "http://example.com")
	m.runDueSchedules(now)

	sc, _ := s.GetSchedule(ctx, "sc_full")
	if sc.NextRunAt == nil || !sc.NextRunAt.After(now) {
		t.Errorf("next_run_at did not advance after failed submit: %+v", sc.NextRunAt)
	}
}
