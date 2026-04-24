package vitals

import (
	"context"
	"testing"
	"time"

	"github.com/AdrianTJ/gospeedtest/internal/chrome"
)

func TestVitalsCollector_Functional(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-world vitals test in short mode")
	}

	cm := chrome.NewManager()
	defer cm.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bCtx, bCancel := cm.NewContext(ctx)
	defer bCancel()

	// Navigate to a site known to have CWV metrics
	res, err := Collect(bCtx, "https://www.google.com")
	if err != nil {
		t.Fatalf("vitals collection failed: %v", err)
	}

	t.Logf("Collected: FCP=%.2f, LCP=%.2f", res.FCP, res.LCP)

	if res.FCP <= 0 {
		t.Errorf("expected positive FCP, got %v", res.FCP)
	}
	if res.LCP <= 0 {
		t.Errorf("expected positive LCP, got %v", res.LCP)
	}
}
