package job

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/AdrianTJ/gospeedtest/internal/collector/network"
	"github.com/AdrianTJ/gospeedtest/internal/store"
)

// Manager handles job orchestration and the worker pool.
type Manager struct {
	store      store.Store
	jobQueue   chan *store.Job
	workerCount int
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
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
func (m *Manager) Submit(ctx context.Context, url string, tiers []string, runs int) (*store.Job, error) {
	job := &store.Job{
		ID:        "jb_" + uuid.New().String()[:8],
		URL:       url,
		Status:    store.StatusPending,
		Tiers:     tiers,
		Runs:      runs,
		TimeoutS:  60, // Default
		CreatedAt: time.Now(),
	}

	if err := m.store.CreateJob(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to create job in store: %w", err)
	}

	select {
	case m.jobQueue <- job:
		return job, nil
	default:
		return nil, fmt.Errorf("job queue is full")
	}
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
}
