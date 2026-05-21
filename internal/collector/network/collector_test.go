package network

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
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
	// ... (existing code)
}

func TestCollect_InvalidURL(t *testing.T) {
	ctx := context.Background()
	_, err := Collect(ctx, "not-a-url")
	if err == nil {
		t.Error("expected error for invalid URL, got nil")
	}
}

func TestCollect_UnreachableHost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Use a non-routable IP address
	_, err := Collect(ctx, "http://192.0.2.1")
	if err == nil {
		t.Error("expected error for unreachable host, got nil")
	}
}

func TestCollect_Timeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Set a timeout shorter than the server's sleep
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := Collect(ctx, ts.URL)
	if err == nil {
		t.Error("expected timeout error, got nil")
	}
}

func TestCollect_SSRFRedirect(t *testing.T) {
	// Start a local test server that redirects requests.
	// Since 127.0.0.1 is a loopback IP, standard redirects to it should be blocked by our SSRF client when GOST_ALLOW_PRIVATE_IPS is not "true".
	
	tsTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("target content"))
	}))
	defer tsTarget.Close()

	tsRedirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, tsTarget.URL, http.StatusFound)
	}))
	defer tsRedirector.Close()

	// Under normal operation (GOST_ALLOW_PRIVATE_IPS unset or false), the redirect to target (loopback IP) should be blocked.
	os.Unsetenv("GOST_ALLOW_PRIVATE_IPS")
	ctx := context.Background()
	_, err := Collect(ctx, tsRedirector.URL)
	if err == nil {
		t.Error("expected SSRF redirect block error, got nil")
	}

	// When GOST_ALLOW_PRIVATE_IPS is true, the redirect to loopback target should be allowed.
	os.Setenv("GOST_ALLOW_PRIVATE_IPS", "true")
	defer os.Unsetenv("GOST_ALLOW_PRIVATE_IPS")
	
	res, err := Collect(ctx, tsRedirector.URL)
	if err != nil {
		t.Fatalf("expected redirect to loopback to succeed when GOST_ALLOW_PRIVATE_IPS=true, got error: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got status %d", res.StatusCode)
	}
}

