package browser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AdrianTJ/gospeedtest/internal/chrome"
)

func TestCollect(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!DOCTYPE html>
<html>
<head><title>Test Page</title></head>
<body>
    <h1>Hello, GoSpeedTest!</h1>
</body>
</html>`))
	}))
	defer ts.Close()

	// Need a chrome manager to provide a valid browser context
	cm := chrome.NewManager()
	defer cm.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	bCtx, bCancel := cm.NewContext(ctx)
	defer bCancel()

	result, err := Collect(bCtx, ts.URL)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if result.PageLoadMS <= 0 {
		t.Errorf("expected positive PageLoadMS, got %v", result.PageLoadMS)
	}

	if result.ResourceCount < 1 {
		t.Errorf("expected at least 1 resource (the main HTML), got %d", result.ResourceCount)
	}

	t.Logf("Result: DOMContentLoaded: %.2fms, PageLoad: %.2fms, Resources: %d", 
		result.DOMContentLoadedMS, result.PageLoadMS, result.ResourceCount)
}
