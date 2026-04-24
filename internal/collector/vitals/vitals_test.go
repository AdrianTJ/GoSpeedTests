package vitals

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AdrianTJ/gospeedtest/internal/chrome"
)

func TestVitalsCollector(t *testing.T) {
	// Setup test server that triggers a layout shift and has some content
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `
			<!DOCTYPE html>
			<html>
			<head><title>Vitals Test</title></head>
			<style>
				body { font-family: sans-serif; }
				#header { font-size: 48px; color: blue; }
				.content { margin-top: 20px; }
			</style>
			<body>
				<h1 id="header">GoSpeedTest Vitals Benchmark</h1>
				<div class="content">
					<p>This is a test page designed to trigger Core Web Vitals.</p>
					<img src="https://via.placeholder.com/800x400" alt="LCP Image" width="800" height="400">
				</div>
				<script>
					// Trigger a layout shift after 1 second by pushing content down
					setTimeout(() => {
						const spacer = document.createElement('div');
						spacer.style.height = '300px';
						spacer.textContent = 'LATE SPACER';
						document.body.insertBefore(spacer, document.getElementById('header'));
					}, 1000);
				</script>
			</body>
			</html>
		`)
	}))
	defer ts.Close()

	// Setup chrome manager
	cm := chrome.NewManager()
	defer cm.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	bCtx, bCancel := cm.NewContext(ctx)
	defer bCancel()

	res, err := Collect(bCtx, ts.URL)
	if err != nil {
		t.Fatalf("vitals collection failed: %v", err)
	}

	if res.FCP <= 0 {
		t.Errorf("expected positive FCP, got %v", res.FCP)
	}
	if res.LCP <= 0 {
		t.Errorf("expected positive LCP, got %v", res.LCP)
	}
	if res.CLS <= 0 {
		t.Errorf("expected positive CLS (layout shift), got %v", res.CLS)
	}

	t.Logf("Collected Vitals: FCP=%.2f, LCP=%.2f, CLS=%.4f", res.FCP, res.LCP, res.CLS)
}
