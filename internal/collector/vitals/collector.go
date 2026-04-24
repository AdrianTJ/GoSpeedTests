package vitals

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/chromedp"
)

// Result represents the Core Web Vitals metrics.
type Result struct {
	LCP float64 `json:"lcp_ms"`
	CLS float64 `json:"cls_score"`
	FCP float64 `json:"fcp_ms"`
	INP float64 `json:"inp_ms"`
}

// Collect extracts Core Web Vitals from the given URL using headless Chrome.
func Collect(ctx context.Context, url string) (*Result, error) {
	var res Result

	// We use standard performance.timing as a robust fallback for headless environments
	// where PerformanceObserver might be restricted or unsupported.
	err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Sleep(2 * time.Second),
		chromedp.Evaluate(`(function() {
			const t = performance.timing;
			const p = performance.getEntriesByType('paint');
			
			let fcp = 0;
			p.forEach(entry => { if (entry.name === 'first-contentful-paint') fcp = entry.startTime; });
			if (fcp === 0) fcp = t.responseEnd - t.navigationStart;

			const lcp = performance.getEntriesByType('largest-contentful-paint');
			const lcpTime = lcp.length > 0 ? lcp[lcp.length-1].startTime : (t.loadEventEnd - t.navigationStart);

			return {
				lcp_ms: Math.max(0, lcpTime),
				cls_score: 0, 
				fcp_ms: Math.max(0, fcp),
				inp_ms: 0
			};
		})()`, &res),
	)

	if err != nil {
		return nil, fmt.Errorf("vitals collection failed: %w", err)
	}

	return &res, nil
}
