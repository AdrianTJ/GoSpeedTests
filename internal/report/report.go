package report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/AdrianTJ/gospeedtest/internal/collector/browser"
	"github.com/AdrianTJ/gospeedtest/internal/collector/lighthouse"
	"github.com/AdrianTJ/gospeedtest/internal/collector/network"
	"github.com/AdrianTJ/gospeedtest/internal/collector/vitals"
)

type Summary struct {
	URL        string             `json:"url"`
	Network    *network.Result    `json:"network,omitempty"`
	Browser    *browser.Result    `json:"browser,omitempty"`
	Vitals     *vitals.Result     `json:"vitals,omitempty"`
	Lighthouse *lighthouse.Result `json:"lighthouse,omitempty"`
}

func WriteJSON(w io.Writer, data interface{}) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

func WriteText(w io.Writer, summaries []Summary) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "URL\tTIER\tMETRIC\tVALUE")
	fmt.Fprintln(tw, "---\t----\t------\t-----")

	for _, s := range summaries {
		if s.Network != nil {
			fmt.Fprintf(tw, "%s\tnetwork\tDNS Lookup\t%.2fms\n", s.URL, s.Network.DNSLookupMS)
			fmt.Fprintf(tw, "%s\tnetwork\tTCP Connect\t%.2fms\n", s.URL, s.Network.TCPConnectMS)
			fmt.Fprintf(tw, "%s\tnetwork\tTTFB\t%.2fms\n", s.URL, s.Network.TTFBMS)
			fmt.Fprintf(tw, "%s\tnetwork\tTotal\t%.2fms\n", s.URL, s.Network.TotalMS)
		}
		if s.Browser != nil {
			fmt.Fprintf(tw, "%s\tbrowser\tDOM Content\t%.2fms\n", s.URL, s.Browser.DOMContentLoadedMS)
			fmt.Fprintf(tw, "%s\tbrowser\tPage Load\t%.2fms\n", s.URL, s.Browser.PageLoadMS)
			fmt.Fprintf(tw, "%s\tbrowser\tResources\t%d\n", s.URL, s.Browser.ResourceCount)
		}
		if s.Vitals != nil {
			fmt.Fprintf(tw, "%s\tvitals\tLCP\t%.2fms\n", s.URL, s.Vitals.LCP)
			fmt.Fprintf(tw, "%s\tvitals\tFCP\t%.2fms\n", s.URL, s.Vitals.FCP)
		}
		if s.Lighthouse != nil {
			fmt.Fprintf(tw, "%s\tlighthouse\tPerformance\t%.2f\n", s.URL, s.Lighthouse.Performance)
			fmt.Fprintf(tw, "%s\tlighthouse\tAccessibility\t%.2f\n", s.URL, s.Lighthouse.Accessibility)
			fmt.Fprintf(tw, "%s\tlighthouse\tBest Practices\t%.2f\n", s.URL, s.Lighthouse.BestPractices)
			fmt.Fprintf(tw, "%s\tlighthouse\tSEO\t%.2f\n", s.URL, s.Lighthouse.SEO)
		}
	}
	tw.Flush()
}

func WriteCSV(w io.Writer, summaries []Summary) error {
	cw := csv.NewWriter(w)
	cw.Write([]string{"url", "tier", "metric", "value"})

	for _, s := range summaries {
		if s.Network != nil {
			cw.Write([]string{s.URL, "network", "dns_lookup_ms", fmt.Sprintf("%.2f", s.Network.DNSLookupMS)})
			cw.Write([]string{s.URL, "network", "total_ms", fmt.Sprintf("%.2f", s.Network.TotalMS)})
		}
		if s.Browser != nil {
			cw.Write([]string{s.URL, "browser", "page_load_ms", fmt.Sprintf("%.2f", s.Browser.PageLoadMS)})
		}
		if s.Vitals != nil {
			cw.Write([]string{s.URL, "vitals", "lcp_ms", fmt.Sprintf("%.2f", s.Vitals.LCP)})
			cw.Write([]string{s.URL, "vitals", "fcp_ms", fmt.Sprintf("%.2f", s.Vitals.FCP)})
		}
		if s.Lighthouse != nil {
			cw.Write([]string{s.URL, "lighthouse", "performance", fmt.Sprintf("%.2f", s.Lighthouse.Performance)})
			cw.Write([]string{s.URL, "lighthouse", "accessibility", fmt.Sprintf("%.2f", s.Lighthouse.Accessibility)})
			cw.Write([]string{s.URL, "lighthouse", "best_practices", fmt.Sprintf("%.2f", s.Lighthouse.BestPractices)})
			cw.Write([]string{s.URL, "lighthouse", "seo", fmt.Sprintf("%.2f", s.Lighthouse.SEO)})
		}
	}
	cw.Flush()
	return cw.Error()
}
