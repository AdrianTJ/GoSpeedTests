package browser

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// Result represents the metrics collected from a headless browser.
type Result struct {
	DOMContentLoadedMS float64 `json:"dom_content_loaded_ms"`
	PageLoadMS         float64 `json:"page_load_ms"`
	ResourceCount      int     `json:"resource_count"`
	// More metrics (ResourceBreakdown, Waterfall) can be added later.
}

// Collect performs a full page load analysis using headless Chrome.
func Collect(ctx context.Context, url string) (*Result, error) {
	// Allocate a new context for Chrome
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.NoSandbox, // Often required in headless/Docker environments
	)
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	defer cancelAlloc()

	taskCtx, cancelTask := chromedp.NewContext(allocCtx)
	defer cancelTask()

	var (
		res            Result
		resources      []*network.Request
		startTime      = time.Now()
		domContentLoaded float64
		loadEventEnd     float64
	)

	// Listen for network events to count resources
	chromedp.ListenTarget(taskCtx, func(ev interface{}) {
		switch ev := ev.(type) {
		case *network.EventRequestWillBeSent:
			resources = append(resources, ev.Request)
		}
	})

	// Perform navigation and extract timings via JS Performance API
	err := chromedp.Run(taskCtx,
		network.Enable(),
		chromedp.Navigate(url),
		// Wait for the full load event
		chromedp.WaitVisible("body", chromedp.ByQuery),
		chromedp.Evaluate(`(function() {
			const t = performance.getEntriesByType("navigation")[0];
			return {
				domContentLoaded: t.domContentLoadedEventEnd,
				loadEventEnd: t.loadEventEnd
			};
		})()`, &res),
	)

	if err != nil {
		return nil, fmt.Errorf("chromedp run failed: %w", err)
	}

	// Calculate metrics
	// res.DOMContentLoadedMS and res.PageLoadMS are already populated by Evaluate.
	// We just need to handle potential zero values if the performance API hasn't finished reporting.
	res.ResourceCount = len(resources)

	// In case performance API isn't fully ready (loadEventEnd is 0), 
	// we fall back to our own timer as a sanity check.
	if res.PageLoadMS <= 0 {
		res.PageLoadMS = float64(time.Since(startTime).Nanoseconds()) / 1e6
	}

	return &res, nil
}
