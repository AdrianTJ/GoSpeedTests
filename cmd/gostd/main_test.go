package main

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestGostdStartup(t *testing.T) {
	// Simple smoke test to see if the server starts
	// Use environment variables for config
	os.Setenv("GOST_LISTEN_ADDR", "localhost:9090")
	os.Setenv("DATABASE_URL", ":memory:") // Use in-memory SQLite for testing

	go func() {
		main()
	}()

	// Give it some time to start
	time.Sleep(200 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", "http://localhost:9090/v1/jobs", nil)
	client := &http.Client{}
	
	// We expect 404 because /v1/jobs GET is not implemented yet in NewServeMux
	// but the server should respond.
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to connect to gostd server: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected response from server, got status: %d", resp.StatusCode)
	}
}
