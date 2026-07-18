package job

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/AdrianTJ/loadstar/internal/budget"
	"github.com/AdrianTJ/loadstar/internal/store"
)

func fptr(v float64) *float64 { return &v }

// waitTerminal polls until the job reaches a terminal status.
func waitTerminal(t *testing.T, s store.Store, id string) *store.Job {
	t.Helper()
	for i := 0; i < 100; i++ {
		j, _ := s.GetJob(context.Background(), id)
		if j != nil && (j.Status == store.StatusCompleted || j.Status == store.StatusPartial || j.Status == store.StatusFailed) {
			return j
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("job never reached a terminal status")
	return nil
}

// TestBudgetEvaluatedAndPersisted runs a network job against a budget that
// must fail (max ttfb 0.001ms) and one that must pass, checking budget_result.
func TestBudgetEvaluatedAndPersisted(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "budget-eval")
	defer os.RemoveAll(tmpDir)
	s, _ := store.NewStore(filepath.Join(tmpDir, "test.db"))
	defer s.Close()

	m := NewManager(s, 1, 10, "")
	m.Start()
	defer m.Stop()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	failing := &budget.Budget{Assertions: map[string]budget.Assertion{
		"network.ttfb_ms": {Max: fptr(0.001)},
	}}
	jb, err := m.SubmitJob(context.Background(), SubmitOptions{
		URL: ts.URL, Tiers: []string{"network"}, Runs: 1, Budget: failing,
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	final := waitTerminal(t, s, jb.ID)

	if final.Status != store.StatusCompleted {
		t.Errorf("job status = %v, want COMPLETED (budget must not affect job status)", final.Status)
	}
	if final.BudgetResult == nil {
		t.Fatal("budget_result not persisted")
	}
	if final.BudgetResult.Status != budget.StatusFail {
		t.Errorf("budget status = %v, want fail", final.BudgetResult.Status)
	}
	if len(final.BudgetResult.Assertions) != 1 || final.BudgetResult.Assertions[0].Actual == nil {
		t.Errorf("assertion detail missing: %+v", final.BudgetResult.Assertions)
	}

	passing := &budget.Budget{Assertions: map[string]budget.Assertion{
		"network.ttfb_ms": {Max: fptr(60000)},
	}}
	jb2, _ := m.SubmitJob(context.Background(), SubmitOptions{
		URL: ts.URL, Tiers: []string{"network"}, Runs: 1, Budget: passing,
	})
	final2 := waitTerminal(t, s, jb2.ID)
	if final2.BudgetResult == nil || final2.BudgetResult.Status != budget.StatusPass {
		t.Errorf("budget status = %+v, want pass", final2.BudgetResult)
	}
}

// TestNoBudgetNoEvaluation pins that jobs without budgets never get a
// budget_result.
func TestNoBudgetNoEvaluation(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "budget-none")
	defer os.RemoveAll(tmpDir)
	s, _ := store.NewStore(filepath.Join(tmpDir, "test.db"))
	defer s.Close()

	m := NewManager(s, 1, 10, "")
	m.Start()
	defer m.Stop()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	jb, _ := m.Submit(context.Background(), ts.URL, []string{"network"}, 1, "")
	final := waitTerminal(t, s, jb.ID)
	if final.BudgetResult != nil {
		t.Errorf("expected nil budget_result, got %+v", final.BudgetResult)
	}
}

// TestBudgetMissingTierMetricFails asserts strict-CI semantics: a budget on a
// metric whose tier was never requested trips the assertion (nil Actual).
func TestBudgetMissingTierMetricFails(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "budget-missing")
	defer os.RemoveAll(tmpDir)
	s, _ := store.NewStore(filepath.Join(tmpDir, "test.db"))
	defer s.Close()

	m := NewManager(s, 1, 10, "")
	m.Start()
	defer m.Stop()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	b := &budget.Budget{Assertions: map[string]budget.Assertion{
		"vitals.lcp_ms": {Max: fptr(2500)}, // vitals tier not requested
	}}
	jb, _ := m.SubmitJob(context.Background(), SubmitOptions{
		URL: ts.URL, Tiers: []string{"network"}, Runs: 1, Budget: b,
	})
	final := waitTerminal(t, s, jb.ID)
	if final.BudgetResult == nil || final.BudgetResult.Status != budget.StatusFail {
		t.Fatalf("budget = %+v, want fail on missing metric", final.BudgetResult)
	}
	if final.BudgetResult.Assertions[0].Actual != nil {
		t.Errorf("Actual = %v, want nil for never-collected metric", *final.BudgetResult.Assertions[0].Actual)
	}
}

// TestWebhookPayloadIncludesBudgetResult verifies the webhook consumer sees
// the budget verdict without a follow-up GET.
func TestWebhookPayloadIncludesBudgetResult(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "budget-webhook")
	defer os.RemoveAll(tmpDir)
	s, _ := store.NewStore(filepath.Join(tmpDir, "test.db"))
	defer s.Close()

	m := NewManager(s, 1, 10, "")
	m.Start()
	defer m.Stop()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	var mu sync.Mutex
	var payload map[string]json.RawMessage
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		json.NewDecoder(r.Body).Decode(&payload)
		w.WriteHeader(http.StatusOK)
	}))
	defer hook.Close()

	b := &budget.Budget{Assertions: map[string]budget.Assertion{
		"network.ttfb_ms": {Max: fptr(0.001)},
	}}
	jb, err := m.SubmitJob(context.Background(), SubmitOptions{
		URL: ts.URL, Tiers: []string{"network"}, Runs: 1, WebhookURL: hook.URL, Budget: b,
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	waitTerminal(t, s, jb.ID)

	// Wait for webhook delivery.
	deadline := time.After(10 * time.Second)
	for {
		mu.Lock()
		got := payload != nil
		mu.Unlock()
		if got {
			break
		}
		select {
		case <-deadline:
			t.Fatal("webhook never delivered")
		case <-time.After(100 * time.Millisecond):
		}
	}

	mu.Lock()
	defer mu.Unlock()
	raw, ok := payload["budget_result"]
	if !ok {
		t.Fatalf("webhook payload missing budget_result: %v", payload)
	}
	var eval budget.Evaluation
	if err := json.Unmarshal(raw, &eval); err != nil {
		t.Fatalf("budget_result unmarshal: %v", err)
	}
	if eval.Status != budget.StatusFail {
		t.Errorf("webhook budget status = %v, want fail", eval.Status)
	}
}
