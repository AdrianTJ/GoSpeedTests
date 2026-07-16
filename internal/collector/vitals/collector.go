package vitals

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// Result represents the lab-measurable web vitals.
//
// INP is deliberately absent: it requires real user interactions and cannot
// be measured in a lab page load. TBT (Total Blocking Time) is the standard
// lab proxy for responsiveness.
type Result struct {
	LCP float64 `json:"lcp_ms"`
	FCP float64 `json:"fcp_ms"`
	// CLS is the Cumulative Layout Shift score (unitless), computed with the
	// standard session windowing: shifts without recent input, grouped into
	// sessions capped at 5s (1s max gap), reporting the worst session.
	CLS float64 `json:"cls"`
	// TBT is the sum of main-thread long-task time beyond 50ms per task,
	// counted after FCP, over the lab observation window (navigation until
	// load + settle). It is the recommended lab proxy for INP.
	TBT float64 `json:"tbt_ms"`
}

// settleDelay is how long we keep observing after the page is ready so that
// late layout shifts, long tasks, and LCP candidates are captured.
const settleDelay = 3 * time.Second

// observerJS is installed before navigation (so buffered observers catch
// every entry) and accumulates vitals into window.__gost.
const observerJS = `
(() => {
	const g = window.__gost = { lcp: 0, fcp: 0, cls: 0, tbt: 0 };

	try {
		new PerformanceObserver((list) => {
			const entries = list.getEntries();
			if (entries.length) g.lcp = entries[entries.length - 1].startTime;
		}).observe({ type: 'largest-contentful-paint', buffered: true });
	} catch (e) {}

	try {
		new PerformanceObserver((list) => {
			for (const e of list.getEntries()) {
				if (e.name === 'first-contentful-paint') g.fcp = e.startTime;
			}
		}).observe({ type: 'paint', buffered: true });
	} catch (e) {}

	// CLS with session windowing (web.dev/articles/cls): ignore shifts with
	// recent input; a session ends after a 1s gap or 5s total; CLS is the
	// worst session's sum.
	try {
		let sessionValue = 0, sessionStart = 0, sessionEnd = 0;
		new PerformanceObserver((list) => {
			for (const e of list.getEntries()) {
				if (e.hadRecentInput) continue;
				if (sessionValue > 0 && (e.startTime - sessionEnd > 1000 || e.startTime - sessionStart > 5000)) {
					sessionValue = 0;
				}
				if (sessionValue === 0) sessionStart = e.startTime;
				sessionValue += e.value;
				sessionEnd = e.startTime;
				if (sessionValue > g.cls) g.cls = sessionValue;
			}
		}).observe({ type: 'layout-shift', buffered: true });
	} catch (e) {}

	// TBT: blocking portion (duration - 50ms) of long tasks after FCP.
	try {
		new PerformanceObserver((list) => {
			for (const e of list.getEntries()) {
				if (g.fcp > 0 && e.startTime < g.fcp) continue;
				g.tbt += Math.max(0, e.duration - 50);
			}
		}).observe({ type: 'longtask', buffered: true });
	} catch (e) {}
})();
`

// readVitalsJS extracts the accumulated metrics, with paint/LCP fallbacks for
// pages where an observer type is unsupported.
const readVitalsJS = `
(() => {
	const g = window.__gost || { lcp: 0, fcp: 0, cls: 0, tbt: 0 };
	if (!g.fcp) {
		for (const e of performance.getEntriesByType('paint')) {
			if (e.name === 'first-contentful-paint') g.fcp = e.startTime;
		}
	}
	if (!g.fcp) {
		const nav = performance.getEntriesByType('navigation')[0];
		if (nav) g.fcp = nav.responseEnd;
	}
	if (!g.lcp) {
		const lcp = performance.getEntriesByType('largest-contentful-paint');
		if (lcp.length) g.lcp = lcp[lcp.length - 1].startTime;
	}
	return { lcp_ms: Math.max(0, g.lcp), fcp_ms: Math.max(0, g.fcp), cls: Math.max(0, g.cls), tbt_ms: Math.max(0, g.tbt) };
})()
`

// Collect measures lab web vitals for the given URL using headless Chrome.
// The observer script must be registered before navigation, which is why this
// uses page.AddScriptToEvaluateOnNewDocument rather than a post-load Evaluate.
func Collect(ctx context.Context, url string) (*Result, error) {
	var res Result

	err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(observerJS).Do(ctx)
			return err
		}),
		chromedp.Navigate(url),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Sleep(settleDelay),
		chromedp.Evaluate(readVitalsJS, &res),
	)

	if err != nil {
		return nil, fmt.Errorf("vitals collection failed: %w", err)
	}

	// LCP is the largest contentful paint, which by definition occurs at or
	// after FCP. On tiny/fast pages the independent measurements can invert due
	// to fallbacks; clamp so the reported values stay internally consistent.
	if res.LCP < res.FCP {
		res.LCP = res.FCP
	}

	return &res, nil
}
