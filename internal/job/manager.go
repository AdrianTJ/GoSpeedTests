package job

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/AdrianTJ/loadstar/internal/budget"
	"github.com/AdrianTJ/loadstar/internal/chrome"
	"github.com/AdrianTJ/loadstar/internal/collector/browser"
	"github.com/AdrianTJ/loadstar/internal/collector/lighthouse"
	"github.com/AdrianTJ/loadstar/internal/collector/network"
	"github.com/AdrianTJ/loadstar/internal/collector/vitals"
	"github.com/AdrianTJ/loadstar/internal/metrics"
	"github.com/AdrianTJ/loadstar/internal/profile"
	"github.com/AdrianTJ/loadstar/internal/store"
	"github.com/AdrianTJ/loadstar/internal/validator"
	"github.com/google/uuid"
)

const (
	maxWebhookRetries = 5
	webhookBatchSize  = 10
	webhookTickRate   = 5 * time.Second
	webhookChanBuffer = 100 // in-memory notify buffer for immediate delivery
	defaultTimeoutS   = 60  // fallback per-job timeout in seconds
)

// Manager handles job orchestration and the worker pool.
type Manager struct {
	store           store.Store
	chrome          *chrome.Manager
	chromeOnce      sync.Once
	jobQueue        chan *store.Job
	webhookChan     chan string // deliveryID
	workerCount     int
	googleAPIKey    string
	defaultTimeoutS int
	wg              sync.WaitGroup
	ctx             context.Context
	cancel          context.CancelFunc
	mu              sync.Mutex
	pendingJobs     map[string]struct{}
	metrics         *metrics.Registry
}

// NewManager creates a new job manager. Chrome is started lazily on the first
// browser/vitals job, so a network- or lighthouse-only deployment never spawns
// a browser (and the manager is constructible in tests without Chrome).
func NewManager(s store.Store, workerCount int, queueDepth int, googleAPIKey string) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		store:           s,
		jobQueue:        make(chan *store.Job, queueDepth),
		webhookChan:     make(chan string, webhookChanBuffer),
		workerCount:     workerCount,
		googleAPIKey:    googleAPIKey,
		defaultTimeoutS: defaultTimeoutS,
		ctx:             ctx,
		cancel:          cancel,
		pendingJobs:     make(map[string]struct{}),
		metrics:         metrics.NewRegistry(),
	}
	m.metrics.Help("loadstar_jobs_total", "Jobs processed, by final status.")
	m.metrics.Help("loadstar_job_duration_seconds", "Wall-clock job processing time.")
	m.metrics.Help("loadstar_queue_depth", "Jobs currently buffered in the queue.")
	m.metrics.Help("loadstar_webhook_deliveries_total", "Webhook delivery attempts, by result.")
	m.metrics.Help("loadstar_scheduler_runs_total", "Scheduler decisions, by result.")
	m.metrics.Help("loadstar_retention_purged_total", "Rows removed by retention, by kind.")
	m.metrics.Help("loadstar_last_metric_ms", "Latest metric values for scheduled URLs.")
	m.metrics.GaugeFunc("loadstar_queue_depth", func() float64 { return float64(len(m.jobQueue)) })
	return m
}

// Metrics exposes the manager's registry for the /metrics endpoint.
func (m *Manager) Metrics() *metrics.Registry {
	return m.metrics
}

// browserManager lazily starts the shared headless Chrome on first use.
func (m *Manager) browserManager() *chrome.Manager {
	m.chromeOnce.Do(func() { m.chrome = chrome.NewManager() })
	return m.chrome
}

// SetDefaultTimeout overrides the default per-job timeout (seconds) applied when
// a submission does not specify one. Values <= 0 are ignored.
func (m *Manager) SetDefaultTimeout(seconds int) {
	if seconds > 0 {
		m.defaultTimeoutS = seconds
	}
}

// Start launches the worker pool and webhook retry loop.
func (m *Manager) Start() {
	for i := 0; i < m.workerCount; i++ {
		m.wg.Add(1)
		go m.worker(i)
	}
	m.wg.Add(1)
	go m.webhookWorker()
}

// Stop gracefully shuts down the worker pool.
//
// The queue channels are deliberately not closed: workers and the webhook
// loop exit via ctx cancellation, and leaving the channels open means a
// Submit racing with Stop can never send on a closed channel (which would
// panic). Any job left buffered at shutdown simply stays PENDING in the store.
func (m *Manager) Stop() {
	m.cancel()
	m.wg.Wait()
	if m.chrome != nil { // may be nil if no browser/vitals job ever ran
		m.chrome.Close()
	}
}

// SubmitOptions describes a job submission. Zero values fall back to
// manager defaults where sensible (TimeoutS).
type SubmitOptions struct {
	URL        string
	Tiers      []string
	Runs       int
	TimeoutS   int
	WebhookURL string
	Budget     *budget.Budget
	ScheduleID string
	Profile    string // throttling profile name; "" or "none" = unthrottled
}

// Submit enqueues a new job for execution using the manager's default timeout.
func (m *Manager) Submit(ctx context.Context, url string, tiers []string, runs int, webhookURL string) (*store.Job, error) {
	return m.SubmitJob(ctx, SubmitOptions{URL: url, Tiers: tiers, Runs: runs, WebhookURL: webhookURL})
}

// SubmitWithTimeout enqueues a new job with a specific per-run timeout (seconds).
// A timeoutS <= 0 falls back to the manager default.
func (m *Manager) SubmitWithTimeout(ctx context.Context, url string, tiers []string, runs int, webhookURL string, timeoutS int) (*store.Job, error) {
	return m.SubmitJob(ctx, SubmitOptions{URL: url, Tiers: tiers, Runs: runs, WebhookURL: webhookURL, TimeoutS: timeoutS})
}

// SubmitJob enqueues a new job described by opts.
func (m *Manager) SubmitJob(ctx context.Context, opts SubmitOptions) (*store.Job, error) {
	timeoutS := opts.TimeoutS
	if timeoutS <= 0 {
		timeoutS = m.defaultTimeoutS
	}
	if timeoutS <= 0 {
		timeoutS = defaultTimeoutS
	}
	job := &store.Job{
		ID:         "jb_" + uuid.New().String()[:8],
		URL:        opts.URL,
		Status:     store.StatusPending,
		Tiers:      opts.Tiers,
		Runs:       opts.Runs,
		TimeoutS:   timeoutS,
		WebhookURL: opts.WebhookURL,
		Budget:     opts.Budget,
		ScheduleID: opts.ScheduleID,
		Profile:    opts.Profile,
		CreatedAt:  time.Now(),
	}

	if err := m.store.CreateJob(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to create job in store: %w", err)
	}

	// enqueueFailed cleans up the just-created state so a rejected submission
	// leaves no orphaned PENDING row that no worker will ever process.
	enqueueFailed := func(reason string) (*store.Job, error) {
		m.mu.Lock()
		delete(m.pendingJobs, job.ID)
		m.mu.Unlock()
		if derr := m.store.DeleteJob(ctx, job.ID); derr != nil {
			slog.Error("Failed to clean up rejected job", "job_id", job.ID, "error", derr)
		}
		return nil, fmt.Errorf("%s", reason)
	}

	m.mu.Lock()
	m.pendingJobs[job.ID] = struct{}{}
	m.mu.Unlock()

	if m.ctx.Err() != nil {
		return enqueueFailed("manager is shutting down")
	}

	select {
	case m.jobQueue <- job:
		return job, nil
	default:
		return enqueueFailed("job queue is full")
	}
}

// CancelJob attempts to cancel a pending job.
func (m *Manager) CancelJob(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.pendingJobs[id]; !ok {
		// Job is already running or finished
		return fmt.Errorf("job cannot be cancelled (already running or finished)")
	}

	delete(m.pendingJobs, id)
	return m.store.DeleteJob(ctx, id)
}

func (m *Manager) worker(id int) {
	defer m.wg.Done()
	slog.Info("Worker started", "worker_id", id)

	for {
		select {
		case <-m.ctx.Done():
			slog.Info("Worker shutting down", "worker_id", id)
			return
		case job, ok := <-m.jobQueue:
			if !ok {
				return
			}

			// Check if job was cancelled while in queue
			m.mu.Lock()
			_, pending := m.pendingJobs[job.ID]
			delete(m.pendingJobs, job.ID)
			m.mu.Unlock()

			if !pending {
				slog.Info("Worker skipping cancelled job", "job_id", job.ID)
				continue
			}

			// Wrap in anonymous function for panic recovery per-job
			func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("Worker panicked while processing job", "worker_id", id, "job_id", job.ID, "recover", r)
						errStr := fmt.Sprintf("internal worker panic: %v", r)
						if err := m.store.UpdateJobStatus(m.ctx, job.ID, store.StatusFailed, &errStr); err != nil {
							// Job may be left RUNNING in the store; recovery on
							// next daemon start will fail it.
							slog.Error("Failed to mark panicked job FAILED", "job_id", job.ID, "error", err)
						}
					}
				}()
				m.processJob(job)
			}()
		}
	}
}

func (m *Manager) processJob(job *store.Job) {
	slog.Info("Worker processing job", "job_id", job.ID, "url", job.URL)
	jobStart := time.Now()

	// Update status to RUNNING
	if err := m.store.UpdateJobStatus(m.ctx, job.ID, store.StatusRunning, nil); err != nil {
		slog.Error("Failed to update job status to RUNNING", "job_id", job.ID, "error", err)
		return
	}

	hasTier := func(name string) bool {
		if len(job.Tiers) == 0 {
			return name == "network" // Default to network only if none specified
		}
		for _, t := range job.Tiers {
			if t == "all" || t == name {
				return true
			}
		}
		return false
	}

	timeout := time.Duration(job.TimeoutS) * time.Second
	if timeout <= 0 {
		timeout = defaultTimeoutS * time.Second
	}

	// A run is "clean" when every attempted tier succeeded, "total-fail" when
	// every attempted tier failed, and "partial" otherwise. Tracking per-tier
	// outcomes (rather than failing the whole run on the first tier error) means
	// a job with, say, a failing Lighthouse tier but a successful network tier
	// is reported as PARTIAL with its usable results preserved — not FAILED.
	cleanRuns := 0
	failedRuns := 0
	failedTiers := map[string]bool{}
	var lastErr error
	var runMetrics []map[string]float64 // per-run flattened metrics for budget evaluation
	var lastMetrics map[string]float64  // most recent run's metrics, for the /metrics gauges
	for i := 1; i <= job.Runs; i++ {
		resultRecord := &store.Result{
			ID:          "res_" + uuid.New().String()[:8],
			JobID:       job.ID,
			RunIndex:    i,
			CollectedAt: time.Now(),
		}

		// Bound each run so a hung target cannot occupy a worker forever.
		runCtx, runCancel := context.WithTimeout(m.ctx, timeout)

		tiersRun := 0
		tiersFailed := 0
		fail := func(tier string, err error) {
			tiersFailed++
			failedTiers[tier] = true
			lastErr = err
		}

		// 1. Network Tier
		if hasTier("network") {
			tiersRun++
			netRes, err := network.Collect(runCtx, job.URL)
			// Keep the measured timing even when the target returned an HTTP
			// error status (Collect returns a populated result alongside the
			// error for status >= 400) — the metrics are still useful.
			if netRes != nil {
				resultRecord.Network = netRes
			}
			if err != nil {
				fail("network", err)
			}
		}

		// The profile was validated at submission; an unknown name here (e.g.
		// a row written by a newer version) falls back to unthrottled.
		prof, _ := profile.Get(job.Profile)

		// 2. Browser Tier
		if hasTier("browser") {
			tiersRun++
			// Create a browser context for this run
			if bCtx, bCancel, err := m.browserManager().NewContext(runCtx); err != nil {
				fail("browser", err)
			} else {
				browserRes, err := collectThrottled(bCtx, job.URL, prof, browser.Collect)
				bCancel()
				if err != nil {
					fail("browser", err)
				} else {
					resultRecord.Browser = browserRes
				}
			}
		}

		// 3. Vitals Tier
		if hasTier("vitals") {
			tiersRun++
			// Create a browser context for this run
			if vCtx, vCancel, err := m.browserManager().NewContext(runCtx); err != nil {
				fail("vitals", err)
			} else {
				vitalsRes, err := collectThrottled(vCtx, job.URL, prof, vitals.Collect)
				vCancel()
				if err != nil {
					fail("vitals", err)
				} else {
					resultRecord.Vitals = vitalsRes
				}
			}
		}

		// 4. Lighthouse Tier
		if hasTier("lighthouse") {
			tiersRun++
			lhRes, err := lighthouse.Collect(runCtx, job.URL, m.googleAPIKey)
			if err != nil {
				fail("lighthouse", err)
			} else {
				resultRecord.Lighthouse = lhRes
			}
		}

		runCancel()

		switch {
		case tiersRun == 0 || tiersFailed == 0:
			cleanRuns++
		case tiersFailed == tiersRun:
			failedRuns++
		}

		if err := m.store.SaveResult(m.ctx, resultRecord); err != nil {
			slog.Error("Failed to save result", "job_id", job.ID, "run_index", i, "error", err)
		}

		lastMetrics = budget.Flatten(resultRecord.Network, resultRecord.Browser, resultRecord.Vitals, resultRecord.Lighthouse)
		if job.Budget != nil {
			runMetrics = append(runMetrics, lastMetrics)
		}
	}

	status, errStr := deriveStatus(job.Runs, cleanRuns, failedRuns, failedTiers, lastErr)

	if err := m.store.UpdateJobStatus(m.ctx, job.ID, status, errStr); err != nil {
		slog.Error("Failed to update job status", "job_id", job.ID, "status", status, "error", err)
	}

	m.metrics.CounterInc("loadstar_jobs_total", "status", string(status))
	m.metrics.Observe("loadstar_job_duration_seconds", time.Since(jobStart).Seconds())
	// Latest-value gauges only for scheduled jobs: schedules are user-curated,
	// so the url label cardinality stays bounded.
	if job.ScheduleID != "" && lastMetrics != nil {
		for _, g := range []struct{ key, name string }{
			{"network.ttfb_ms", "ttfb"},
			{"network.total_ms", "total"},
			{"browser.page_load_ms", "page_load"},
			{"vitals.lcp_ms", "lcp"},
		} {
			if v, ok := lastMetrics[g.key]; ok {
				m.metrics.GaugeSet("loadstar_last_metric_ms", v, "url", job.URL, "metric", g.name)
			}
		}
	}

	// Budgets are judged on the median across runs so a single noisy run does
	// not decide the verdict. Persist before sendWebhook: the webhook re-reads
	// the job, so the payload picks up the evaluation.
	if job.Budget != nil {
		eval := budget.Evaluate(job.Budget, budget.Aggregate(runMetrics))
		if err := m.store.SetJobBudgetResult(m.ctx, job.ID, eval); err != nil {
			slog.Error("Failed to persist budget result", "job_id", job.ID, "error", err)
		} else {
			slog.Info("Budget evaluated", "job_id", job.ID, "budget_status", eval.Status)
		}
	}

	if job.WebhookURL != "" {
		m.sendWebhook(job.ID)
	}
}

// collectThrottled applies the throttling profile to the tab (no-op for
// "none") and then runs the collector on it.
func collectThrottled[T any](ctx context.Context, url string, prof profile.Profile, collect func(context.Context, string) (*T, error)) (*T, error) {
	if err := profile.Apply(ctx, prof); err != nil {
		return nil, err
	}
	return collect(ctx, url)
}

// deriveStatus maps per-run tier outcomes to a final job status: COMPLETED when
// every run was clean, FAILED when every run had all its tiers fail, and PARTIAL
// otherwise (some tiers/runs succeeded and some failed).
func deriveStatus(totalRuns, cleanRuns, failedRuns int, failedTiers map[string]bool, lastErr error) (store.JobStatus, *string) {
	switch {
	case totalRuns > 0 && failedRuns == totalRuns:
		if lastErr != nil {
			s := lastErr.Error()
			return store.StatusFailed, &s
		}
		return store.StatusFailed, nil
	case cleanRuns < totalRuns:
		s := fmt.Sprintf("partial: %d/%d runs completed cleanly; failing tiers: %v; last error: %v",
			cleanRuns, totalRuns, sortedKeys(failedTiers), lastErr)
		return store.StatusPartial, &s
	default:
		return store.StatusCompleted, nil
	}
}

// sortedKeys returns the map keys in deterministic order (for stable messages).
func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (m *Manager) sendWebhook(jobID string) {
	// 1. Get job and results to build payload
	job, err := m.store.GetJob(m.ctx, jobID)
	if err != nil || job == nil || job.WebhookURL == "" {
		return
	}

	results, _ := m.store.GetResultsByJobID(m.ctx, jobID)
	payload := map[string]interface{}{
		"job_id":  job.ID,
		"status":  job.Status,
		"url":     job.URL,
		"results": results,
	}
	if job.BudgetResult != nil {
		payload["budget_result"] = job.BudgetResult
	}
	body, _ := json.Marshal(payload)

	// 2. Persist initial delivery record
	delivery := &store.WebhookDelivery{
		ID:        "wh_" + uuid.New().String()[:8],
		JobID:     job.ID,
		URL:       job.WebhookURL,
		Payload:   body,
		Status:    "PENDING",
		CreatedAt: time.Now(),
	}

	if err := m.store.EnqueueWebhook(m.ctx, delivery); err != nil {
		slog.Error("Failed to enqueue webhook", "job_id", jobID, "error", err)
		return
	}

	// 3. Notify worker to attempt delivery
	select {
	case m.webhookChan <- delivery.ID:
	default:
		// Channel full, background tick will pick it up
	}
}

func (m *Manager) webhookWorker() {
	defer m.wg.Done()
	slog.Info("Webhook worker started")

	ticker := time.NewTicker(webhookTickRate)
	defer ticker.Stop()

	// SSRF-hardened client shared across delivery attempts: blocks private-IP
	// connects (DNS-rebinding defense) and re-validates redirects at each hop.
	client := validator.NewSafeClient(10 * time.Second)

	for {
		select {
		case <-m.ctx.Done():
			slog.Info("Webhook worker shutting down")
			return
		case <-ticker.C:
			// Regular sweep for pending deliveries
			m.processPendingWebhooks(client)
		case deliveryID := <-m.webhookChan:
			// Immediate attempt for specific delivery
			m.attemptWebhook(client, deliveryID)
		}
	}
}

func (m *Manager) processPendingWebhooks(client *http.Client) {
	// Simple limit to prevent starvation
	deliveries, err := m.store.GetPendingWebhooks(m.ctx, webhookBatchSize)
	if err != nil {
		slog.Error("Failed to fetch pending webhooks", "error", err)
		return
	}

	for _, d := range deliveries {
		m.sendOneWebhook(client, d)
	}
}

func (m *Manager) attemptWebhook(client *http.Client, deliveryID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	d, err := m.store.GetWebhookByID(ctx, deliveryID)
	if err != nil || d == nil {
		return
	}
	// Only deliver if it is still pending; the periodic sweep may have
	// already handled it.
	if d.Status != "PENDING" {
		return
	}
	m.sendOneWebhook(client, *d)
}

func (m *Manager) sendOneWebhook(client *http.Client, d store.WebhookDelivery) {
	now := time.Now()
	d.Attempts++
	d.LastAttempt = &now

	resp, err := client.Post(d.URL, "application/json", bytes.NewBuffer(d.Payload))

	success := err == nil && resp != nil && resp.StatusCode >= 200 && resp.StatusCode < 300
	if resp != nil {
		defer resp.Body.Close()
	}

	if success {
		slog.Info("Webhook delivered", "job_id", d.JobID, "delivery_id", d.ID, "attempts", d.Attempts)
		m.metrics.CounterInc("loadstar_webhook_deliveries_total", "result", "success")
		m.setWebhookStatus(d, "SUCCESS", nil)
		return
	}

	// Handle failure
	if d.Attempts >= maxWebhookRetries {
		slog.Error("Webhook failed permanently", "job_id", d.JobID, "delivery_id", d.ID, "attempts", d.Attempts, "error", err)
		m.metrics.CounterInc("loadstar_webhook_deliveries_total", "result", "failed")
		m.setWebhookStatus(d, "FAILED", nil)
		return
	}
	m.metrics.CounterInc("loadstar_webhook_deliveries_total", "result", "retry")

	// Calculate exponential backoff (e.g., 2, 4, 8, 16, 32 seconds)
	backoff := time.Duration(math.Pow(2, float64(d.Attempts))) * time.Second
	nextAttempt := now.Add(backoff)

	slog.Warn("Webhook failed, scheduling retry", "job_id", d.JobID, "delivery_id", d.ID, "attempts", d.Attempts, "next_attempt", nextAttempt, "error", err)
	m.setWebhookStatus(d, "PENDING", &nextAttempt)
}

// setWebhookStatus persists a delivery-state change, logging (rather than
// dropping) a store failure — the periodic sweep re-reads state from the
// store, so an unpersisted transition would otherwise be invisible.
func (m *Manager) setWebhookStatus(d store.WebhookDelivery, status string, nextAttempt *time.Time) {
	if err := m.store.UpdateWebhookStatus(m.ctx, d.ID, status, d.Attempts, d.LastAttempt, nextAttempt); err != nil {
		slog.Error("Failed to persist webhook delivery status",
			"delivery_id", d.ID, "status", status, "error", err)
	}
}
