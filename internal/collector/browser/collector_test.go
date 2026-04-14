package browser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCollect(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!DOCTYPE html>
<html>
<head><title>Test Page</title></head>
<body>
    <h1>Hello, GoSpeedTest!</h1>
    <script>
        console.log("Browser test running...");
    </script>
</body>
</html>`))
	}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	result, err := Collect(ctx, ts.URL)
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}

	if result.PageLoadMS <= 0 {
		t.Errorf("expected positive PageLoadMS, got %v", result.PageLoadMS)
	}

	if result.DOMContentLoadedMS <= 0 {
		t.Errorf("expected positive DOMContentLoadedMS, got %v", result.DOMContentLoadedMS)
	}

	if result.ResourceCount < 1 {
		t.Errorf("expected at least 1 resource (the main HTML), got %d", result.ResourceCount)
	}

	t.Logf("Result: DOMContentLoaded: %.2fms, PageLoad: %.2fms, Resources: %d", 
		result.DOMContentLoadedMS, result.PageLoadMS, result.ResourceCount)
}
