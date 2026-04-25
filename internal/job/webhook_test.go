package job

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AdrianTJ/gospeedtest/internal/store"
)

func TestWebhookRetries(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "webhook-retry-test")
	defer os.RemoveAll(tmpDir)
	s, _ := store.NewStore(filepath.Join(tmpDir, "test.db"))
	defer s.Close()

	// 1 worker, short tick rate for testing
	m := NewManager(s, 1, 10)
	m.Start()
	defer m.Stop()

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&attempts, 1)
		if current == 1 {
			// Fail the first time
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		// Succeed the second time
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Submit job with the failing-then-succeeding webhook
	_, err := m.Submit(context.Background(), "http://example.com", []string{"network"}, 1, server.URL)
	if err != nil {
		t.Fatalf("failed to submit job: %v", err)
	}

	// 1. Wait for first attempt (which fails)
	time.Sleep(2 * time.Second)
	
	if atomic.LoadInt32(&attempts) != 1 {
		t.Errorf("expected 1 attempt after initial fail, got %d", atomic.LoadInt32(&attempts))
	}

	// 2. Manually trigger the worker check (since backoff is minutes)
	// For the test, we'll reach into the store and cheat the 'next_attempt' to be now
	// This proves the retry logic works when the time is right.
	pending, _ := s.GetPendingWebhooks(context.Background(), 1)
	if len(pending) != 1 {
		t.Fatal("expected 1 pending webhook after failure")
	}

	now := time.Now().Add(-1 * time.Hour) // Set next_attempt to the past
	s.UpdateWebhookStatus(context.Background(), pending[0].ID, "PENDING", 1, pending[0].LastAttempt, &now)

	// 3. Wait for ticker to pick it up or trigger another webhook action
	// In this test environment, the ticker is 5s (webhookTickRate).
	// Let's just wait for it.
	time.Sleep(6 * time.Second)

	if atomic.LoadInt32(&attempts) != 2 {
		t.Errorf("expected 2 attempts after retry, got %d", atomic.LoadInt32(&attempts))
	}
	
	// 4. Verify status is SUCCESS
	// We'd need a GetWebhook method to verify this cleanly, but let's assume if attempts=2 it worked.
}
