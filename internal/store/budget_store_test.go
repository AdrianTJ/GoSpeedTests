package store

import (
	"context"
	"database/sql"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AdrianTJ/loadstar/internal/budget"
	"github.com/AdrianTJ/loadstar/internal/collector/network"
	"github.com/AdrianTJ/loadstar/internal/collector/vitals"
)

func newBudgetTestStore(t *testing.T, name string) Store {
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

func fptr(v float64) *float64 { return &v }

// TestJobBudgetRoundTrip verifies budget and budget_result survive the
// JSON-column round trip through CreateJob/SetJobBudgetResult/GetJob.
func TestJobBudgetRoundTrip(t *testing.T) {
	s := newBudgetTestStore(t, "budget-roundtrip")
	ctx := context.Background()

	b := &budget.Budget{Assertions: map[string]budget.Assertion{
		"network.ttfb_ms": {Max: fptr(500)},
	}}
	if err := s.CreateJob(ctx, &Job{
		ID: "jb_budget", URL: "http://example.com", Status: StatusPending,
		Tiers: []string{"network"}, Runs: 1, Budget: b, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create job: %v", err)
	}

	got, err := s.GetJob(ctx, "jb_budget")
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if got.Budget == nil {
		t.Fatal("budget not persisted")
	}
	a := got.Budget.Assertions["network.ttfb_ms"]
	if a.Max == nil || *a.Max != 500 {
		t.Errorf("budget assertion mismatch: %+v", a)
	}
	if got.BudgetResult != nil {
		t.Errorf("budget_result should be nil before evaluation, got %+v", got.BudgetResult)
	}

	eval := budget.Evaluate(b, map[string]float64{"network.ttfb_ms": 600})
	if err := s.SetJobBudgetResult(ctx, "jb_budget", eval); err != nil {
		t.Fatalf("set budget result: %v", err)
	}

	got, _ = s.GetJob(ctx, "jb_budget")
	if got.BudgetResult == nil {
		t.Fatal("budget_result not persisted")
	}
	if got.BudgetResult.Status != budget.StatusFail {
		t.Errorf("budget_result.status = %v, want fail", got.BudgetResult.Status)
	}
	if len(got.BudgetResult.Assertions) != 1 || got.BudgetResult.Assertions[0].Actual == nil {
		t.Errorf("assertion detail lost in round trip: %+v", got.BudgetResult.Assertions)
	}
}

// TestJobWithoutBudgetHasNullColumns pins that jobs without budgets store SQL
// NULL (not the string "null") and read back as nil pointers.
func TestJobWithoutBudgetHasNullColumns(t *testing.T) {
	s := newBudgetTestStore(t, "budget-null")
	ctx := context.Background()

	if err := s.CreateJob(ctx, &Job{
		ID: "jb_nobudget", URL: "http://example.com", Status: StatusPending,
		Tiers: []string{"network"}, Runs: 1, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create job: %v", err)
	}
	got, err := s.GetJob(ctx, "jb_nobudget")
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if got.Budget != nil || got.BudgetResult != nil {
		t.Errorf("expected nil budget fields, got %+v / %+v", got.Budget, got.BudgetResult)
	}
}

// TestGetHistory_Percentiles seeds runs with known TTFB values and checks the
// new percentile metrics plus legacy avg fields.
func TestGetHistory_Percentiles(t *testing.T) {
	s := newBudgetTestStore(t, "history-pct")
	ctx := context.Background()
	url := "http://example.com/pct"

	if err := s.CreateJob(ctx, &Job{
		ID: "jb_hist", URL: url, Status: StatusCompleted,
		Tiers: []string{"network"}, Runs: 4, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create job: %v", err)
	}
	// TTFB 100,200,300,400 -> avg 250, p50 250, p75 325, p95 385
	for i, ttfb := range []float64{100, 200, 300, 400} {
		if err := s.SaveResult(ctx, &Result{
			ID: "res_hist_" + string(rune('a'+i)), JobID: "jb_hist", RunIndex: i + 1,
			Network:     &network.Result{TTFBMS: ttfb, TotalMS: ttfb * 2},
			Vitals:      &vitals.Result{LCP: 1000 + ttfb, FCP: 500},
			CollectedAt: time.Now(),
		}); err != nil {
			t.Fatalf("save result %d: %v", i, err)
		}
	}

	h, err := s.GetHistory(ctx, url)
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	if h.TestCount != 4 {
		t.Errorf("test_count = %d, want 4", h.TestCount)
	}

	ttfb, ok := h.Metrics["network.ttfb_ms"]
	if !ok {
		t.Fatalf("metrics missing network.ttfb_ms: %v", h.Metrics)
	}
	check := func(name string, got, want float64) {
		if math.Abs(got-want) > 1e-6 {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	check("ttfb.avg", ttfb.Avg, 250)
	check("ttfb.p50", ttfb.P50, 250)
	check("ttfb.p75", ttfb.P75, 325)
	check("ttfb.p95", ttfb.P95, 385)
	if ttfb.Count != 4 {
		t.Errorf("ttfb.count = %d, want 4", ttfb.Count)
	}

	// Vitals collected too — must appear under its metric key.
	if _, ok := h.Metrics["vitals.lcp_ms"]; !ok {
		t.Errorf("metrics missing vitals.lcp_ms: %v", h.Metrics)
	}

	// Legacy fields mirror the network averages.
	check("avg_ttfb_ms", h.AvgTTFBMS, 250)
	check("avg_total_ms", h.AvgTotalMS, 500)
}

// TestGetHistory_EmptyURL returns an empty (not nil) history for unknown URLs.
func TestGetHistory_UnknownURL(t *testing.T) {
	s := newBudgetTestStore(t, "history-unknown")
	h, err := s.GetHistory(context.Background(), "http://never-tested.example")
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	if h.TestCount != 0 || len(h.Metrics) != 0 {
		t.Errorf("expected empty history, got %+v", h)
	}
}

// TestMigrationV5_UpgradesV4Database hand-builds a genuine v4-era database
// (schema through migration 4, version rows 1-4, one existing job), then
// opens it with the current store: v5 must apply and the old row must read
// back with nil budget fields.
func TestMigrationV5_UpgradesV4Database(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "migrate-v5")
	defer os.RemoveAll(tmpDir)
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	v4Schema := `
	CREATE TABLE jobs (
		id TEXT PRIMARY KEY, url TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'PENDING',
		tiers TEXT NOT NULL, runs INTEGER NOT NULL DEFAULT 1, timeout_s INTEGER NOT NULL DEFAULT 60,
		tags TEXT, error TEXT, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		started_at DATETIME, completed_at DATETIME, webhook_url TEXT
	);
	CREATE TABLE results (
		id TEXT PRIMARY KEY, job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
		run_index INTEGER NOT NULL DEFAULT 1, network TEXT, browser TEXT, vitals TEXT,
		collected_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, lighthouse TEXT
	);
	CREATE TABLE webhook_deliveries (
		id TEXT PRIMARY KEY, job_id TEXT NOT NULL, url TEXT NOT NULL, payload BLOB NOT NULL,
		attempts INTEGER NOT NULL DEFAULT 0, last_attempt DATETIME, next_attempt DATETIME,
		status TEXT NOT NULL DEFAULT 'PENDING', created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY);
	INSERT INTO schema_migrations (version) VALUES (1), (2), (3), (4);
	INSERT INTO jobs (id, url, status, tiers, runs) VALUES ('jb_old', 'http://example.com', 'COMPLETED', '["network"]', 1);
	`
	if _, err := db.Exec(v4Schema); err != nil {
		t.Fatalf("build v4 schema: %v", err)
	}
	db.Close()

	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("open store over v4 db (v5 migration failed): %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	got, err := s.GetJob(ctx, "jb_old")
	if err != nil {
		t.Fatalf("get pre-v5 job after migration: %v", err)
	}
	if got == nil || got.Budget != nil || got.BudgetResult != nil {
		t.Errorf("pre-v5 row misread after migration: %+v", got)
	}

	// And the new columns must be writable.
	eval := &budget.Evaluation{Status: budget.StatusPass}
	if err := s.SetJobBudgetResult(ctx, "jb_old", eval); err != nil {
		t.Fatalf("set budget result on migrated db: %v", err)
	}
	got, _ = s.GetJob(ctx, "jb_old")
	if got.BudgetResult == nil || got.BudgetResult.Status != budget.StatusPass {
		t.Errorf("budget_result write on migrated db failed: %+v", got.BudgetResult)
	}
}
