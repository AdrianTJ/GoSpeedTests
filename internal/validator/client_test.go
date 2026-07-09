package validator

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestNewSafeClient_BlocksPrivateIP verifies the connect-time guard: a client
// dialing a loopback address is refused when the private-IP guard is active.
// This is the core DNS-rebinding defense — a hostname that resolves (or rebinds)
// to a private IP is blocked at the socket regardless of prior validation.
func TestNewSafeClient_BlocksPrivateIP(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	t.Setenv("GOST_ALLOW_PRIVATE_IPS", "false")

	client := NewSafeClient(5 * time.Second)
	resp, err := client.Get(ts.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected connection to loopback to be blocked, got success")
	}
}

// TestNewSafeClient_AllowsPrivateIPWhenConfigured verifies the escape hatch:
// with GOST_ALLOW_PRIVATE_IPS=true the same loopback connection succeeds.
func TestNewSafeClient_AllowsPrivateIPWhenConfigured(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	t.Setenv("GOST_ALLOW_PRIVATE_IPS", "true")

	client := NewSafeClient(5 * time.Second)
	resp, err := client.Get(ts.URL)
	if err != nil {
		t.Fatalf("expected loopback connection to succeed when allowed, got %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}
