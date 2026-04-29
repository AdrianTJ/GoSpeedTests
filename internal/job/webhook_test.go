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
	m := NewManager(s, 1, 10, "")
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

	// 2. Wait for retry. 
	// Backoff is 2^1 = 2 seconds.
	// Ticker is 5 seconds.
	// We wait 10 seconds total to be safe.
	time.Sleep(10 * time.Second)

	if atomic.LoadInt32(&attempts) != 2 {
		t.Errorf("expected 2 attempts after retry, got %d", atomic.LoadInt32(&attempts))
	}
}
