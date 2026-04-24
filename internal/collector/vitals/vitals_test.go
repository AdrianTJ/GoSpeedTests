package vitals

import (
	"context"
	"testing"
	"time"

	"github.com/AdrianTJ/gospeedtest/internal/chrome"
)

func TestVitalsCollector_Benchmark(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping benchmark in short mode")
	}

	cm := chrome.NewManager()
	defer cm.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bCtx, bCancel := cm.NewContext(ctx)
	defer bCancel()

	// Use web.dev as it is optimized for Vitals
	res, err := Collect(bCtx, "https://web.dev")
	if err != nil {
		t.Fatalf("vitals collection failed: %v", err)
	}

	t.Logf("web.dev Vitals: FCP=%.2f, LCP=%.2f, CLS=%.4f", res.FCP, res.LCP, res.CLS)

	if res.FCP <= 0 {
		t.Errorf("expected positive FCP for web.dev, got %v", res.FCP)
	}
}
