package job

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/AdrianTJ/gospeedtest/internal/chrome"
	"github.com/AdrianTJ/gospeedtest/internal/collector/browser"
	"github.com/AdrianTJ/gospeedtest/internal/collector/network"
	"github.com/AdrianTJ/gospeedtest/internal/collector/vitals"
	"github.com/AdrianTJ/gospeedtest/internal/store"
	"github.com/google/uuid"
)

const (
	maxWebhookRetries = 5
	webhookBatchSize  = 10
	webhookTickRate   = 5 * time.Second
)

// Manager handles job orchestration and the worker pool.
type Manager struct {
	store        store.Store
	chrome       *chrome.Manager
	jobQueue     chan *store.Job
	webhookChan  chan string // deliveryID
	workerCount  int
	wg           sync.WaitGroup
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.Mutex
	pendingJobs  map[string]struct{}
}

// NewManager creates a new job manager.
func NewManager(s store.Store, workerCount int, queueDepth int) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		store:       s,
		chrome:      chrome.NewManager(),
		jobQueue:    make(chan *store.Job, queueDepth),
		webhookChan: make(chan string, 100),
		workerCount: workerCount,
		ctx:         ctx,
		cancel:      cancel,
		pendingJobs: make(map[string]struct{}),
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
func (m *Manager) Stop() {
	m.cancel()
	close(m.jobQueue)
	close(m.webhookChan)
	m.wg.Wait()
	m.chrome.Close()
}

// Submit enqueues a new job for execution.
func (m *Manager) Submit(ctx context.Context, url string, tiers []string, runs int, webhookURL string) (*store.Job, error) {
	job := &store.Job{
		ID:         "jb_" + uuid.New().String()[:8],
		URL:        url,
		Status:     store.StatusPending,
		Tiers:      tiers,
		Runs:       runs,
		TimeoutS:   60, // Default
		WebhookURL: webhookURL,
		CreatedAt:  time.Now(),
	}

	if err := m.store.CreateJob(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to create job in store: %w", err)
	}

	m.mu.Lock()
	m.pendingJobs[job.ID] = struct{}{}
	m.mu.Unlock()

	select {
	case m.jobQueue <- job:
		return job, nil
	default:
		m.mu.Lock()
		delete(m.pendingJobs, job.ID)
		m.mu.Unlock()
		return nil, fmt.Errorf("job queue is full")
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
						m.store.UpdateJobStatus(m.ctx, job.ID, store.StatusFailed, &errStr)
					}
				}()
				m.processJob(job)
			}()
		}
	}
}

func (m *Manager) processJob(job *store.Job) {
	slog.Info("Worker processing job", "job_id", job.ID, "url", job.URL)

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

	successCount := 0
	var lastErr error
	for i := 1; i <= job.Runs; i++ {
		resultRecord := &store.Result{
			ID:          "res_" + uuid.New().String()[:8],
			JobID:       job.ID,
			RunIndex:    i,
			CollectedAt: time.Now(),
		}

		runFailed := false

		// 1. Network Tier
		if hasTier("network") {
			netRes, err := network.Collect(m.ctx, job.URL)
			if err != nil {
				lastErr = err
				runFailed = true
			} else {
				resultRecord.Network = netRes
			}
		}

		// 2. Browser Tier
		if hasTier("browser") {
			// Create a browser context for this run
			bCtx, bCancel := m.chrome.NewContext(m.ctx)
			browserRes, err := browser.Collect(bCtx, job.URL)
			bCancel()
			if err != nil {
				lastErr = err
				runFailed = true
			} else {
				resultRecord.Browser = browserRes
			}
		}

		// 3. Vitals Tier
		if hasTier("vitals") {
			// Create a browser context for this run
			vCtx, vCancel := m.chrome.NewContext(m.ctx)
			vitalsRes, err := vitals.Collect(vCtx, job.URL)
			vCancel()
			if err != nil {
				lastErr = err
				runFailed = true
			} else {
				resultRecord.Vitals = vitalsRes
			}
		}


		if !runFailed {
			successCount++
		}

		if err := m.store.SaveResult(m.ctx, resultRecord); err != nil {
			slog.Error("Failed to save result", "job_id", job.ID, "run_index", i, "error", err)
		}
	}

	status := store.StatusCompleted
	var errStr *string

	if successCount == 0 && job.Runs > 0 {
		status = store.StatusFailed
		if lastErr != nil {
			s := lastErr.Error()
			errStr = &s
		}
	} else if successCount < job.Runs {
		status = store.StatusPartial
		s := fmt.Sprintf("only %d/%d runs succeeded; last error: %v", successCount, job.Runs, lastErr)
		errStr = &s
	}

	if err := m.store.UpdateJobStatus(m.ctx, job.ID, status, errStr); err != nil {
		slog.Error("Failed to update job status", "job_id", job.ID, "status", status, "error", err)
	}

	if job.WebhookURL != "" {
		m.sendWebhook(job.ID)
	}
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

	client := &http.Client{Timeout: 10 * time.Second}

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
	// For immediate attempts, we need to fetch the full delivery first
	// We'll use a temporary context for the DB fetch
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// This is a bit inefficient (extra DB call), but keeps logic clean
	deliveries, err := m.store.GetPendingWebhooks(ctx, 100) // Filter by ID would be better but we only have GetPending
	if err != nil {
		return
	}
	
	for _, d := range deliveries {
		if d.ID == deliveryID {
			m.sendOneWebhook(client, d)
			return
		}
	}
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
		m.store.UpdateWebhookStatus(m.ctx, d.ID, "SUCCESS", d.Attempts, d.LastAttempt, nil)
		return
	}

	// Handle failure
	if d.Attempts >= maxWebhookRetries {
		slog.Error("Webhook failed permanently", "job_id", d.JobID, "delivery_id", d.ID, "attempts", d.Attempts, "error", err)
		m.store.UpdateWebhookStatus(m.ctx, d.ID, "FAILED", d.Attempts, d.LastAttempt, nil)
		return
	}

	// Calculate exponential backoff (e.g., 2, 4, 8, 16, 32 minutes)
	backoff := time.Duration(math.Pow(2, float64(d.Attempts))) * time.Minute
	nextAttempt := now.Add(backoff)

	slog.Warn("Webhook failed, scheduling retry", "job_id", d.JobID, "delivery_id", d.ID, "attempts", d.Attempts, "next_attempt", nextAttempt, "error", err)
	m.store.UpdateWebhookStatus(m.ctx, d.ID, "PENDING", d.Attempts, d.LastAttempt, &nextAttempt)
}
