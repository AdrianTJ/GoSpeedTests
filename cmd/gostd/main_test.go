package main

import (
	"net/http"
	"os"
	"testing"
	"time"
)

func TestGostdStartup(t *testing.T) {
	// Simple smoke test to see if the server starts.
	// Bind and dial an explicit IPv4 address so the client and server can't end
	// up on different address families (localhost may resolve to ::1).
	const addr = "127.0.0.1:9090"
	os.Setenv("GOST_LISTEN_ADDR", addr)
	os.Setenv("DATABASE_URL", ":memory:") // Use in-memory SQLite for testing

	// Mock command line args
	os.Args = []string{"gostd", "-insecure"}
	go func() {
		main()
	}()

	// Poll the health endpoint until the server is listening, rather than
	// racing a fixed sleep against startup.
	client := &http.Client{Timeout: 1 * time.Second}
	healthURL := "http://" + addr + "/v1/health"

	var resp *http.Response
	var err error
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err = client.Get(healthURL)
		if err == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("Failed to connect to gostd server after startup window: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected health check 200 from server, got status: %d", resp.StatusCode)
	}
}
