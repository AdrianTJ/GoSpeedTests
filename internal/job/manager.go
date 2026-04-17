package job

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/AdrianTJ/gospeedtest/internal/collector/network"
	"github.com/AdrianTJ/gospeedtest/internal/store"
	"github.com/google/uuid"
)

// Manager handles job orchestration and the worker pool.
type Manager struct {
	store        store.Store
	jobQueue     chan *store.Job
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
		jobQueue:    make(chan *store.Job, queueDepth),
		workerCount: workerCount,
		ctx:         ctx,
		cancel:      cancel,
		pendingJobs: make(map[string]struct{}),
	}
}

// Start launches the worker pool.
func (m *Manager) Start() {
	for i := 0; i < m.workerCount; i++ {
		m.wg.Add(1)
		go m.worker(i)
	}
}

// Stop gracefully shuts down the worker pool.
func (m *Manager) Stop() {
	m.cancel()
	close(m.jobQueue)
	m.wg.Wait()
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
	log.Printf("Worker %d started", id)

	for {
		select {
		case <-m.ctx.Done():
			log.Printf("Worker %d shutting down", id)
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
				log.Printf("Worker skipping cancelled job %s", job.ID)
				continue
			}

			m.processJob(job)
		}
	}
}

func (m *Manager) processJob(job *store.Job) {
	log.Printf("Worker processing job %s for %s", job.ID, job.URL)

	// Update status to RUNNING
	if err := m.store.UpdateJobStatus(m.ctx, job.ID, store.StatusRunning, nil); err != nil {
		log.Printf("Failed to update job %s to RUNNING: %v", job.ID, err)
		return
	}

	// For now, we only have the network collector
	// In a real implementation, we'd iterate over job.Runs and tiers
	var lastErr error
	for i := 1; i <= job.Runs; i++ {
		res, err := network.Collect(m.ctx, job.URL)
		if err != nil {
			lastErr = err
			continue
		}

		resultRecord := &store.Result{
			ID:          "res_" + uuid.New().String()[:8],
			JobID:       job.ID,
			RunIndex:    i,
			Network:     res,
			CollectedAt: time.Now(),
		}

		if err := m.store.SaveResult(m.ctx, resultRecord); err != nil {
			log.Printf("Failed to save result for job %s run %d: %v", job.ID, i, err)
		}
	}

	status := store.StatusCompleted
	var errStr *string
	if lastErr != nil {
		status = store.StatusFailed
		s := lastErr.Error()
		errStr = &s
	}

	if err := m.store.UpdateJobStatus(m.ctx, job.ID, status, errStr); err != nil {
		log.Printf("Failed to update job %s to %s: %v", job.ID, status, err)
	}

	if job.WebhookURL != "" {
		go m.sendWebhook(job.ID)
	}
}

func (m *Manager) sendWebhook(jobID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	job, err := m.store.GetJob(ctx, jobID)
	if err != nil || job == nil {
		return
	}

	results, _ := m.store.GetResultsByJobID(ctx, jobID)

	payload := map[string]interface{}{
		"job_id":  job.ID,
		"status":  job.Status,
		"url":     job.URL,
		"results": results,
	}

	body, _ := json.Marshal(payload)
	resp, err := http.Post(job.WebhookURL, "application/json", bytes.NewBuffer(body))
	if err != nil {
		log.Printf("Webhook failed for job %s: %v", job.ID, err)
		return
	}
	defer resp.Body.Close()
	log.Printf("Webhook sent for job %s, status: %d", job.ID, resp.StatusCode)
}
