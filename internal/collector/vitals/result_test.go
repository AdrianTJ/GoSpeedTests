package vitals

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestResultJSONShape pins the wire format: cls and tbt_ms present, and the
// old hardcoded-zero inp_ms gone (INP needs real user input; a lab load has
// none, and reporting 0 was misleading).
func TestResultJSONShape(t *testing.T) {
	data, err := json.Marshal(Result{LCP: 1200, FCP: 800, CLS: 0.05, TBT: 150})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(data)
	for _, want := range []string{`"lcp_ms":1200`, `"fcp_ms":800`, `"cls":0.05`, `"tbt_ms":150`} {
		if !strings.Contains(out, want) {
			t.Errorf("JSON missing %q: %s", want, out)
		}
	}
	if strings.Contains(out, "inp_ms") {
		t.Errorf("JSON must not contain inp_ms: %s", out)
	}
}

// TestOldRowsStillDecode ensures results stored by pre-v7 versions (which
// include inp_ms) unmarshal cleanly, simply dropping the removed field.
func TestOldRowsStillDecode(t *testing.T) {
	old := `{"lcp_ms": 1500, "fcp_ms": 900, "inp_ms": 0}`
	var r Result
	if err := json.Unmarshal([]byte(old), &r); err != nil {
		t.Fatalf("unmarshal old row: %v", err)
	}
	if r.LCP != 1500 || r.FCP != 900 || r.CLS != 0 || r.TBT != 0 {
		t.Errorf("old row decoded wrong: %+v", r)
	}
}

// TestObserverJSWellFormed sanity-checks the injected script for the entry
// types it must observe — a typo here silently zeroes a metric.
func TestObserverJSWellFormed(t *testing.T) {
	for _, want := range []string{
		"largest-contentful-paint", "first-contentful-paint",
		"layout-shift", "longtask", "hadRecentInput", "buffered: true",
	} {
		if !strings.Contains(observerJS, want) {
			t.Errorf("observerJS missing %q", want)
		}
	}
	if !strings.Contains(readVitalsJS, "tbt_ms") || !strings.Contains(readVitalsJS, "cls") {
		t.Error("readVitalsJS must return cls and tbt_ms")
	}
}
