package report

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/AdrianTJ/loadstar/internal/collector/browser"
	"github.com/AdrianTJ/loadstar/internal/collector/lighthouse"
	"github.com/AdrianTJ/loadstar/internal/collector/network"
	"github.com/AdrianTJ/loadstar/internal/collector/vitals"
)

func fullSummary() Summary {
	return Summary{
		URL:        "http://example.com",
		Network:    &network.Result{DNSLookupMS: 1, TTFBMS: 2, TotalMS: 3, StatusCode: 200, ResponseBytes: 100},
		Browser:    &browser.Result{DOMContentLoadedMS: 4, PageLoadMS: 5, ResourceCount: 2},
		Vitals:     &vitals.Result{LCP: 6, FCP: 7, CLS: 0.123, TBT: 45},
		Lighthouse: &lighthouse.Result{Performance: 0.9, SEO: 0.8},
	}
}

func TestWriteJSON_AllTiers(t *testing.T) {
	var b bytes.Buffer
	if err := WriteJSON(&b, []Summary{fullSummary()}); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	out := b.String()
	for _, want := range []string{`"url"`, `"ttfb_ms"`, `"status_code": 200`, `"page_load_ms"`, `"lcp_ms"`, `"cls"`, `"tbt_ms"`, `"performance"`} {
		if !strings.Contains(out, want) {
			t.Errorf("JSON missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, `"inp_ms"`) {
		t.Errorf("JSON must not contain the removed inp_ms field\n%s", out)
	}
}

func TestWriteTextAndCSV_IncludeCLSAndTBT(t *testing.T) {
	var text bytes.Buffer
	WriteText(&text, []Summary{fullSummary()})
	for _, want := range []string{"CLS", "0.123", "TBT", "45.00ms"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("text output missing %q\n%s", want, text.String())
		}
	}

	var csvOut bytes.Buffer
	if err := WriteCSV(&csvOut, []Summary{fullSummary()}); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}
	for _, want := range []string{"vitals,cls,0.123", "vitals,tbt_ms,45.00"} {
		if !strings.Contains(csvOut.String(), want) {
			t.Errorf("CSV output missing %q\n%s", want, csvOut.String())
		}
	}
}

func TestWriteText_NilTiersOmitted(t *testing.T) {
	var b bytes.Buffer
	// Only the network tier is present; the others must not appear.
	WriteText(&b, []Summary{{URL: "http://x", Network: &network.Result{StatusCode: 200}}})
	out := b.String()
	if !strings.Contains(out, "network") {
		t.Errorf("expected network rows, got:\n%s", out)
	}
	for _, absent := range []string{"browser", "vitals", "lighthouse"} {
		if strings.Contains(out, absent) {
			t.Errorf("expected no %q rows for a network-only summary, got:\n%s", absent, out)
		}
	}
}

func TestWriteCSV_HeaderOnlyWhenEmpty(t *testing.T) {
	var b bytes.Buffer
	if err := WriteCSV(&b, nil); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}
	if got := strings.TrimSpace(b.String()); got != "url,tier,metric,value" {
		t.Errorf("expected header-only output, got %q", got)
	}
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestWriteCSV_PropagatesWriterError(t *testing.T) {
	if err := WriteCSV(errWriter{}, []Summary{fullSummary()}); err == nil {
		t.Error("expected WriteCSV to surface the writer error")
	}
}
