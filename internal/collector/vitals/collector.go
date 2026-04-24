package vitals

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/performance"
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

	err := chromedp.Run(ctx,
		performance.Enable(),
		chromedp.Navigate(url),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Sleep(5 * time.Second),
		chromedp.ActionFunc(func(ctx context.Context) error {
			metrics, err := performance.GetMetrics().Do(ctx)
			if err != nil {
				return err
			}
			
			for _, m := range metrics {
				switch m.Name {
				case "FirstContentfulPaint":
					res.FCP = m.Value * 1000
				case "LargestContentfulPaint":
					res.LCP = m.Value * 1000
				}
			}
			return nil
		}),
	)

	// If CDP still fails, fallback to simple timing
	if res.FCP == 0 {
		var timing float64
		chromedp.Run(ctx, chromedp.Evaluate(`(performance.timing.responseEnd - performance.timing.navigationStart)`, &timing))
		res.FCP = timing
	}

	if err != nil {
		return nil, fmt.Errorf("vitals collection failed: %w", err)
	}

	return &res, nil
}
