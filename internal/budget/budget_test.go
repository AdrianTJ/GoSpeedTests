package budget

import (
	"strings"
	"testing"

	"github.com/AdrianTJ/gospeedtest/internal/collector/lighthouse"
	"github.com/AdrianTJ/gospeedtest/internal/collector/network"
	"github.com/AdrianTJ/gospeedtest/internal/collector/vitals"
)

func f(v float64) *float64 { return &v }

func TestParse_YAMLAndJSONEquivalent(t *testing.T) {
	yamlSrc := `
assertions:
  network.ttfb_ms:
    max: 500
  lighthouse.performance:
    min: 0.9
    level: warn
`
	jsonSrc := `{"assertions":{"network.ttfb_ms":{"max":500},"lighthouse.performance":{"min":0.9,"level":"warn"}}}`

	for name, src := range map[string]string{"yaml": yamlSrc, "json": jsonSrc} {
		b, err := Parse([]byte(src))
		if err != nil {
			t.Fatalf("%s: Parse failed: %v", name, err)
		}
		if err := b.Validate(); err != nil {
			t.Fatalf("%s: Validate failed: %v", name, err)
		}
		a := b.Assertions["network.ttfb_ms"]
		if a.Max == nil || *a.Max != 500 {
			t.Errorf("%s: ttfb max = %v, want 500", name, a.Max)
		}
		lh := b.Assertions["lighthouse.performance"]
		if lh.Min == nil || *lh.Min != 0.9 || lh.Level != LevelWarn {
			t.Errorf("%s: lighthouse assertion mismatch: %+v", name, lh)
		}
	}
}

func TestValidate_Rejections(t *testing.T) {
	cases := []struct {
		name    string
		budget  Budget
		wantSub string
	}{
		{"empty", Budget{}, "no assertions"},
		{"unknown metric", Budget{Assertions: map[string]Assertion{"bogus.metric": {Max: f(1)}}}, "unknown metric"},
		{"no bounds", Budget{Assertions: map[string]Assertion{"network.ttfb_ms": {}}}, "neither max nor min"},
		{"bad level", Budget{Assertions: map[string]Assertion{"network.ttfb_ms": {Max: f(1), Level: "critical"}}}, "invalid level"},
	}
	for _, c := range cases {
		err := c.budget.Validate()
		if err == nil || !strings.Contains(err.Error(), c.wantSub) {
			t.Errorf("%s: Validate() = %v, want error containing %q", c.name, err, c.wantSub)
		}
	}
}

func TestValidate_TooManyAssertions(t *testing.T) {
	// The size cap is checked before per-key validation, so synthetic key
	// names are fine here.
	b := Budget{Assertions: map[string]Assertion{}}
	for i := 0; i < maxAssertions+1; i++ {
		b.Assertions[string(rune('a'+i%26))+string(rune('0'+i/26))] = Assertion{Max: f(1)}
	}
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "assertions") {
		t.Errorf("expected cap error, got %v", err)
	}
}

func TestEvaluate_PassWarnFail(t *testing.T) {
	b := &Budget{Assertions: map[string]Assertion{
		"network.ttfb_ms":        {Max: f(500)},                   // pass (error level default)
		"network.total_ms":       {Max: f(100), Level: LevelWarn}, // warn (tripped)
		"lighthouse.performance": {Min: f(0.9)},                   // fail (below min)
	}}
	metrics := map[string]float64{
		"network.ttfb_ms":        200,
		"network.total_ms":       150,
		"lighthouse.performance": 0.5,
	}
	eval := Evaluate(b, metrics)
	if eval.Status != StatusFail {
		t.Errorf("overall = %v, want fail", eval.Status)
	}
	byMetric := map[string]AssertionResult{}
	for _, a := range eval.Assertions {
		byMetric[a.Metric] = a
	}
	if byMetric["network.ttfb_ms"].Status != StatusPass {
		t.Errorf("ttfb = %v, want pass", byMetric["network.ttfb_ms"].Status)
	}
	if byMetric["network.total_ms"].Status != StatusWarn {
		t.Errorf("total = %v, want warn", byMetric["network.total_ms"].Status)
	}
	if byMetric["lighthouse.performance"].Status != StatusFail {
		t.Errorf("performance = %v, want fail", byMetric["lighthouse.performance"].Status)
	}
	if byMetric["network.ttfb_ms"].Actual == nil || *byMetric["network.ttfb_ms"].Actual != 200 {
		t.Errorf("ttfb actual not recorded")
	}
}

func TestEvaluate_WarnOnlyOverall(t *testing.T) {
	b := &Budget{Assertions: map[string]Assertion{
		"network.ttfb_ms": {Max: f(100), Level: LevelWarn},
	}}
	eval := Evaluate(b, map[string]float64{"network.ttfb_ms": 200})
	if eval.Status != StatusWarn {
		t.Errorf("overall = %v, want warn", eval.Status)
	}
}

func TestEvaluate_MissingMetricTripsAtLevel(t *testing.T) {
	b := &Budget{Assertions: map[string]Assertion{
		"vitals.lcp_ms":        {Max: f(2500)},                   // error level: missing -> fail
		"browser.page_load_ms": {Max: f(3000), Level: LevelWarn}, // warn level: missing -> warn
	}}
	eval := Evaluate(b, map[string]float64{}) // nothing collected
	if eval.Status != StatusFail {
		t.Errorf("overall = %v, want fail (missing error-level metric)", eval.Status)
	}
	for _, a := range eval.Assertions {
		if a.Actual != nil {
			t.Errorf("%s: Actual = %v, want nil for missing metric", a.Metric, *a.Actual)
		}
		switch a.Metric {
		case "vitals.lcp_ms":
			if a.Status != StatusFail {
				t.Errorf("lcp = %v, want fail", a.Status)
			}
		case "browser.page_load_ms":
			if a.Status != StatusWarn {
				t.Errorf("page_load = %v, want warn", a.Status)
			}
		}
	}
}

func TestEvaluate_MinAndMaxBothChecked(t *testing.T) {
	// A single assertion with both bounds produces two results.
	b := &Budget{Assertions: map[string]Assertion{
		"network.ttfb_ms": {Min: f(10), Max: f(100)},
	}}
	eval := Evaluate(b, map[string]float64{"network.ttfb_ms": 5})
	if len(eval.Assertions) != 2 {
		t.Fatalf("got %d assertion results, want 2", len(eval.Assertions))
	}
	if eval.Status != StatusFail {
		t.Errorf("overall = %v, want fail (below min)", eval.Status)
	}
}

func TestEvaluate_Deterministic(t *testing.T) {
	b := &Budget{Assertions: map[string]Assertion{
		"network.ttfb_ms":  {Max: f(1)},
		"network.total_ms": {Max: f(1)},
		"vitals.lcp_ms":    {Max: f(1)},
	}}
	first := Evaluate(b, nil)
	for i := 0; i < 10; i++ {
		next := Evaluate(b, nil)
		for j := range first.Assertions {
			if first.Assertions[j].Metric != next.Assertions[j].Metric {
				t.Fatalf("iteration %d: order changed: %v vs %v", i, first.Assertions[j].Metric, next.Assertions[j].Metric)
			}
		}
	}
}

func TestAggregate_Median(t *testing.T) {
	runs := []map[string]float64{
		{"network.ttfb_ms": 100},
		{"network.ttfb_ms": 300},
		{"network.ttfb_ms": 200},
	}
	agg := Aggregate(runs)
	if agg["network.ttfb_ms"] != 200 {
		t.Errorf("odd median = %v, want 200", agg["network.ttfb_ms"])
	}

	runs = append(runs, map[string]float64{"network.ttfb_ms": 400})
	agg = Aggregate(runs)
	if agg["network.ttfb_ms"] != 250 {
		t.Errorf("even median = %v, want 250", agg["network.ttfb_ms"])
	}
}

func TestAggregate_KeyOnlyCountsRunsThatCollectedIt(t *testing.T) {
	runs := []map[string]float64{
		{"network.ttfb_ms": 100, "vitals.lcp_ms": 1000},
		{"network.ttfb_ms": 200}, // vitals tier failed this run
	}
	agg := Aggregate(runs)
	if agg["vitals.lcp_ms"] != 1000 {
		t.Errorf("lcp = %v, want 1000 (single-run median)", agg["vitals.lcp_ms"])
	}
}

func TestFlatten_NilTiersSafe(t *testing.T) {
	m := Flatten(nil, nil, nil, nil)
	if len(m) != 0 {
		t.Errorf("Flatten(all nil) = %v, want empty", m)
	}

	m = Flatten(&network.Result{TTFBMS: 123, TotalMS: 456, ResponseBytes: 789}, nil, nil,
		&lighthouse.Result{Performance: 0.95})
	if m["network.ttfb_ms"] != 123 || m["network.total_ms"] != 456 || m["network.response_bytes"] != 789 {
		t.Errorf("network flatten mismatch: %v", m)
	}
	if m["lighthouse.performance"] != 0.95 {
		t.Errorf("lighthouse flatten mismatch: %v", m)
	}
	if _, ok := m["browser.page_load_ms"]; ok {
		t.Error("nil browser tier must not contribute keys")
	}
}

func TestLoadFile_Missing(t *testing.T) {
	if _, err := LoadFile("/nonexistent/budget.yaml"); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestVitalsCLSAndTBTKeys(t *testing.T) {
	b := Budget{Assertions: map[string]Assertion{
		"vitals.cls":    {Max: f(0.1)},
		"vitals.tbt_ms": {Max: f(200)},
	}}
	if err := b.Validate(); err != nil {
		t.Fatalf("cls/tbt keys must validate: %v", err)
	}

	m := Flatten(nil, nil, &vitals.Result{LCP: 1, FCP: 1, CLS: 0.25, TBT: 300}, nil)
	if m["vitals.cls"] != 0.25 || m["vitals.tbt_ms"] != 300 {
		t.Errorf("flatten mismatch: %v", m)
	}

	eval := Evaluate(&b, m)
	if eval.Status != StatusFail {
		t.Errorf("status = %v, want fail (both thresholds exceeded)", eval.Status)
	}
}
