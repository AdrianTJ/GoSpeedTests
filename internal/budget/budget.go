// Package budget implements performance budgets: named thresholds on
// collected metrics that are evaluated against a job's results. Budgets make
// the tool usable as a CI gate — a job carries a verdict (pass/warn/fail),
// not just numbers.
//
// This package must not import internal/store: store embeds Budget and
// Evaluation on its Job type, so the dependency runs the other way. That is
// why Flatten takes the collector result types directly.
package budget

import (
	"fmt"
	"os"
	"sort"

	"github.com/AdrianTJ/loadstar/internal/collector/browser"
	"github.com/AdrianTJ/loadstar/internal/collector/lighthouse"
	"github.com/AdrianTJ/loadstar/internal/collector/network"
	"github.com/AdrianTJ/loadstar/internal/collector/vitals"
	"github.com/AdrianTJ/loadstar/internal/stats"
	"gopkg.in/yaml.v3"
)

// Level is the severity of an assertion. Error-level violations fail the
// budget; warn-level violations are reported but do not fail it.
type Level string

const (
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Status is the outcome of an assertion or of the budget as a whole.
type Status string

const (
	StatusPass Status = "pass"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
)

const maxAssertions = 50

// Assertion is a threshold on a single metric. At least one of Max or Min
// must be set. Level defaults to "error".
type Assertion struct {
	Max   *float64 `json:"max,omitempty" yaml:"max"`
	Min   *float64 `json:"min,omitempty" yaml:"min"`
	Level Level    `json:"level,omitempty" yaml:"level"`
}

// Budget is a set of assertions keyed by metric name (see metricKeys).
type Budget struct {
	Assertions map[string]Assertion `json:"assertions" yaml:"assertions"`
}

// AssertionResult is the evaluated outcome of one assertion. Actual is nil
// when the metric was not collected (tier failed or not requested) — the
// assertion still trips at its own level so missing data cannot silently
// pass a CI gate.
type AssertionResult struct {
	Metric    string   `json:"metric"`
	Level     Level    `json:"level"`
	Op        string   `json:"op"` // "max" or "min"
	Threshold float64  `json:"threshold"`
	Actual    *float64 `json:"actual"`
	Status    Status   `json:"status"`
}

// Evaluation is the outcome of evaluating a whole budget.
type Evaluation struct {
	Status     Status            `json:"status"`
	Assertions []AssertionResult `json:"assertions"`
}

// metricKeys is the registry of assertable metrics. Keys are
// "<tier>.<json_field>" so budget files read like the API responses.
var metricKeys = map[string]bool{
	"network.ttfb_ms":               true,
	"network.total_ms":              true,
	"network.dns_lookup_ms":         true,
	"network.tls_handshake_ms":      true,
	"network.response_bytes":        true,
	"browser.page_load_ms":          true,
	"browser.dom_content_loaded_ms": true,
	"browser.resource_count":        true,
	"vitals.lcp_ms":                 true,
	"vitals.fcp_ms":                 true,
	"vitals.cls":                    true,
	"vitals.tbt_ms":                 true,
	"lighthouse.performance":        true,
	"lighthouse.accessibility":      true,
	"lighthouse.best_practices":     true,
	"lighthouse.seo":                true,
}

// Parse decodes a budget from YAML or JSON (YAML is a superset of JSON, so
// one decoder handles both).
func Parse(data []byte) (*Budget, error) {
	var b Budget
	if err := yaml.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("parsing budget: %w", err)
	}
	return &b, nil
}

// LoadFile reads and parses a budget file (YAML or JSON).
func LoadFile(path string) (*Budget, error) {
	// #nosec G304 -- path comes from the CLI's -budget flag, not from a
	// request. Anyone who can set it can already read the file as this user.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading budget file: %w", err)
	}
	b, err := Parse(data)
	if err != nil {
		return nil, err
	}
	if err := b.Validate(); err != nil {
		return nil, err
	}
	return b, nil
}

// Validate checks that every assertion targets a known metric, sets at least
// one bound, and uses a valid level.
func (b *Budget) Validate() error {
	if len(b.Assertions) == 0 {
		return fmt.Errorf("budget has no assertions")
	}
	if len(b.Assertions) > maxAssertions {
		return fmt.Errorf("budget has %d assertions (max %d)", len(b.Assertions), maxAssertions)
	}
	for key, a := range b.Assertions {
		if !metricKeys[key] {
			return fmt.Errorf("unknown metric %q (supported: see docs)", key)
		}
		if a.Max == nil && a.Min == nil {
			return fmt.Errorf("assertion %q sets neither max nor min", key)
		}
		switch a.Level {
		case "", LevelWarn, LevelError:
		default:
			return fmt.Errorf("assertion %q has invalid level %q (allowed: warn, error)", key, a.Level)
		}
	}
	return nil
}

// Flatten converts one run's collector results into the flat metric map the
// budget assertions are keyed by. Nil tiers contribute nothing.
func Flatten(n *network.Result, br *browser.Result, v *vitals.Result, lh *lighthouse.Result) map[string]float64 {
	m := make(map[string]float64)
	if n != nil {
		m["network.ttfb_ms"] = n.TTFBMS
		m["network.total_ms"] = n.TotalMS
		m["network.dns_lookup_ms"] = n.DNSLookupMS
		m["network.tls_handshake_ms"] = n.TLSHandshakeMS
		m["network.response_bytes"] = float64(n.ResponseBytes)
	}
	if br != nil {
		m["browser.page_load_ms"] = br.PageLoadMS
		m["browser.dom_content_loaded_ms"] = br.DOMContentLoadedMS
		m["browser.resource_count"] = float64(br.ResourceCount)
	}
	if v != nil {
		m["vitals.lcp_ms"] = v.LCP
		m["vitals.fcp_ms"] = v.FCP
		m["vitals.cls"] = v.CLS
		m["vitals.tbt_ms"] = v.TBT
	}
	if lh != nil {
		m["lighthouse.performance"] = lh.Performance
		m["lighthouse.accessibility"] = lh.Accessibility
		m["lighthouse.best_practices"] = lh.BestPractices
		m["lighthouse.seo"] = lh.SEO
	}
	return m
}

// Aggregate combines per-run metric maps into one map using the median per
// key, so a single noisy run does not decide the verdict. A key only counts
// runs in which it was collected.
func Aggregate(runs []map[string]float64) map[string]float64 {
	byKey := make(map[string][]float64)
	for _, run := range runs {
		for k, v := range run {
			byKey[k] = append(byKey[k], v)
		}
	}
	agg := make(map[string]float64, len(byKey))
	for k, vs := range byKey {
		agg[k] = stats.Median(vs)
	}
	return agg
}

// Evaluate checks every assertion against the aggregated metrics. A metric
// absent from the map trips its assertion at the assertion's level with a
// nil Actual. Overall status: fail if any error-level assertion tripped,
// else warn if any warn-level tripped, else pass.
func Evaluate(b *Budget, metrics map[string]float64) *Evaluation {
	eval := &Evaluation{Status: StatusPass}
	for key, a := range b.Assertions {
		level := a.Level
		if level == "" {
			level = LevelError
		}
		bounds := []struct {
			op        string
			threshold *float64
		}{
			{"max", a.Max},
			{"min", a.Min},
		}
		for _, bound := range bounds {
			if bound.threshold == nil {
				continue
			}
			ar := AssertionResult{
				Metric:    key,
				Level:     level,
				Op:        bound.op,
				Threshold: *bound.threshold,
				Status:    StatusPass,
			}
			actual, ok := metrics[key]
			if !ok {
				ar.Status = tripStatus(level)
			} else {
				v := actual
				ar.Actual = &v
				if (bound.op == "max" && actual > *bound.threshold) ||
					(bound.op == "min" && actual < *bound.threshold) {
					ar.Status = tripStatus(level)
				}
			}
			eval.Assertions = append(eval.Assertions, ar)
			if ar.Status == StatusFail {
				eval.Status = StatusFail
			} else if ar.Status == StatusWarn && eval.Status == StatusPass {
				eval.Status = StatusWarn
			}
		}
	}
	sortResults(eval.Assertions)
	return eval
}

func tripStatus(level Level) Status {
	if level == LevelWarn {
		return StatusWarn
	}
	return StatusFail
}

// sortResults orders assertion results by metric then op so Evaluate output
// is deterministic despite map iteration order.
func sortResults(results []AssertionResult) {
	sort.Slice(results, func(i, j int) bool {
		if results[i].Metric != results[j].Metric {
			return results[i].Metric < results[j].Metric
		}
		return results[i].Op < results[j].Op
	})
}
