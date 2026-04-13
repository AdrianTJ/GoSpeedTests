package main

import (
	"net/http"
	"net/http/httptest"
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

	// Build the CLI binary (temporary)
	cmd := exec.Command("/opt/homebrew/bin/go", "run", "main.go", "-u", ts.URL)
	cmd.Dir = "." // Current directory cmd/gost

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI execution failed: %v\nOutput: %s", err, string(output))
	}

	outStr := string(output)
	if !strings.Contains(outStr, "Analyzing network metrics") {
		t.Errorf("Expected output to contain 'Analyzing network metrics', got: %s", outStr)
	}
	if !strings.Contains(outStr, `"status_code": 200`) {
		t.Errorf("Expected JSON result with status_code 200, got: %s", outStr)
	}
}
