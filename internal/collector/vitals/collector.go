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
	// Create a new tab context from the parent context
	taskCtx, cancelTask := chromedp.NewContext(ctx)
	defer cancelTask()

	var res Result

	// This script uses the PerformanceObserver API to capture CWV metrics.
	const script = `
		(function() {
			window.__vitals = { fcp: 0, lcp: 0, cls: 0, inp: 0 };
			
			// FCP
			new PerformanceObserver((entryList) => {
				for (const entry of entryList.getEntriesByName('first-contentful-paint')) {
					window.__vitals.fcp = entry.startTime;
				}
			}).observe({type: 'paint', buffered: true});

			// LCP
			new PerformanceObserver((entryList) => {
				for (const entry of entryList.getEntries()) {
					window.__vitals.lcp = entry.startTime;
				}
			}).observe({type: 'largest-contentful-paint', buffered: true});

			// CLS
			new PerformanceObserver((entryList) => {
				for (const entry of entryList.getEntries()) {
					if (!entry.hadRecentInput) {
						window.__vitals.cls += entry.value;
					}
				}
			}).observe({type: 'layout-shift', buffered: true});

			// INP (Interaction to Next Paint)
			new PerformanceObserver((entryList) => {
				for (const entry of entryList.getEntries()) {
					if (entry.duration > window.__vitals.inp) {
						window.__vitals.inp = entry.duration;
					}
				}
			}).observe({type: 'event-timing', buffered: true, durationThreshold: 0});
		})();
	`

	err := chromedp.Run(taskCtx,
		chromedp.Navigate(url),
		// Inject script as soon as possible after navigation starts
		chromedp.ActionFunc(func(ctx context.Context) error {
			return chromedp.Evaluate(script, nil).Do(ctx)
		}),
		// Wait for the page to be somewhat stable
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Sleep(1 * time.Second),
		// Inject synthetic interactions to trigger INP
		chromedp.Click("body", chromedp.ByQuery),
		chromedp.Sleep(1 * time.Second),
		chromedp.Evaluate(`(function() {
			if (!window.__vitals) return { lcp_ms: 0, cls_score: 0, fcp_ms: 0, inp_ms: 0 };
			return {
				lcp_ms: window.__vitals.lcp || 0,
				cls_score: window.__vitals.cls || 0,
				fcp_ms: window.__vitals.fcp || 0,
				inp_ms: window.__vitals.inp || 0
			};
		})()`, &res),
	)

	if err != nil {
		return nil, fmt.Errorf("vitals collection failed: %w", err)
	}

	return &res, nil
}
