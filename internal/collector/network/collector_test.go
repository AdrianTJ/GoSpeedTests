package network

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCollect_Success_200(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello world"))
	}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := Collect(ctx, ts.URL)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if result.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", result.StatusCode)
	}

	if result.ResponseBytes != int64(len("hello world")) {
		t.Errorf("expected %d bytes, got %d", len("hello world"), result.ResponseBytes)
	}

	if result.TotalMS <= 0 {
		t.Errorf("expected positive TotalMS, got %v", result.TotalMS)
	}
}

func TestCollect_Success_202(t *testing.T) {
	// Specifically test 202 Accepted for async contexts
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"job_id":"test"}`))
	}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := Collect(ctx, ts.URL)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if result.StatusCode != http.StatusAccepted {
		t.Errorf("expected status 202, got %d", result.StatusCode)
	}
}
