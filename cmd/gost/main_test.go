package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestGostCLI(t *testing.T) {
	// Setup test server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer ts.Close()

	// Run the CLI against the test server. Use the network tier only so the
	// test does not require a headless Chrome install, and resolve "go" from
	// PATH rather than a hardcoded path.
	cmd := exec.Command("go", "run", ".", "-u", ts.URL, "-t", "network", "-f", "json")
	cmd.Dir = "." // Current directory cmd/gost
	cmd.Env = append(os.Environ(), "GOST_ALLOW_PRIVATE_IPS=true")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI execution failed: %v\nOutput: %s", err, string(output))
	}

	outStr := string(output)
	if !strings.Contains(outStr, `"url":`) {
		t.Errorf("Expected JSON output, got: %s", outStr)
	}
	if !strings.Contains(outStr, `"status_code": 200`) {
		t.Errorf("Expected JSON result with status_code 200, got: %s", outStr)
	}
}
