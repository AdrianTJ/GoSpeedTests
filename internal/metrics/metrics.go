// Package metrics is a minimal Prometheus text-exposition registry: counters,
// gauges (static and function-backed), and fixed-bucket histograms. It exists
// so the daemon can expose /metrics without pulling in client_golang — this
// repo stays deliberately dependency-light, and the exposition format for
// these three types is small and stable (verified by exact-output tests).
package metrics

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

type metricType int

const (
	typeCounter metricType = iota
	typeGauge
	typeHistogram
)

// series is one (name, labels) time series.
type series struct {
	labels string // pre-rendered {k="v",...} or ""
	value  float64
	// histogram state
	buckets []float64 // upper bounds, sorted
	counts  []uint64  // cumulative per bucket
	sum     float64
	count   uint64
}

type family struct {
	name    string
	typ     metricType
	help    string
	order   int // registration order, for stable output
	series  map[string]*series
	gaugeFn func() float64 // non-nil for function-backed gauges
}

// Registry collects metric families and renders them in Prometheus text
// exposition format (version 0.0.4). Safe for concurrent use.
type Registry struct {
	mu       sync.Mutex
	families map[string]*family
	nextOrd  int
}

// DefaultBuckets are the histogram bounds used for job durations (seconds).
var DefaultBuckets = []float64{1, 5, 10, 30, 60, 120, 300, 600}

func NewRegistry() *Registry {
	return &Registry{families: make(map[string]*family)}
}

// Help registers descriptive text for a metric name. Optional; families
// without help render "# HELP <name> <name>".
func (r *Registry) Help(name, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.family(name, typeCounter).help = text // type fixed on first data point
}

// family returns (creating if needed) the family for name. Caller holds mu.
// The type is set on creation; later calls keep the original type.
func (r *Registry) family(name string, typ metricType) *family {
	f, ok := r.families[name]
	if !ok {
		f = &family{name: name, typ: typ, order: r.nextOrd, series: make(map[string]*series)}
		r.nextOrd++
		r.families[name] = f
	}
	return f
}

func (f *family) getSeries(labels string) *series {
	s, ok := f.series[labels]
	if !ok {
		s = &series{labels: labels}
		f.series[labels] = s
	}
	return s
}

// renderLabels turns k,v pairs into a deterministic `{k="v",...}` string.
// Label values are escaped per the exposition format (backslash, quote, newline).
func renderLabels(labels []string) string {
	if len(labels) == 0 {
		return ""
	}
	if len(labels)%2 != 0 {
		labels = append(labels, "INVALID")
	}
	type kv struct{ k, v string }
	pairs := make([]kv, 0, len(labels)/2)
	for i := 0; i < len(labels); i += 2 {
		pairs = append(pairs, kv{labels[i], labels[i+1]})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].k < pairs[j].k })
	var b strings.Builder
	b.WriteByte('{')
	for i, p := range pairs {
		if i > 0 {
			b.WriteByte(',')
		}
		// Go's %q escaping of backslash, quote, and newline matches the
		// Prometheus exposition-format label escaping exactly.
		fmt.Fprintf(&b, `%s=%q`, p.k, p.v)
	}
	b.WriteByte('}')
	return b.String()
}

// CounterInc increments a counter by 1. Labels are k,v pairs.
func (r *Registry) CounterInc(name string, labels ...string) {
	r.CounterAdd(name, 1, labels...)
}

// CounterAdd increments a counter by v (must be >= 0).
func (r *Registry) CounterAdd(name string, v float64, labels ...string) {
	if v < 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.family(name, typeCounter).getSeries(renderLabels(labels)).value += v
}

// GaugeSet sets a gauge to v.
func (r *Registry) GaugeSet(name string, v float64, labels ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f := r.family(name, typeGauge)
	f.typ = typeGauge
	f.getSeries(renderLabels(labels)).value = v
}

// GaugeFunc registers a gauge evaluated at scrape time (e.g. queue depth).
func (r *Registry) GaugeFunc(name string, fn func() float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f := r.family(name, typeGauge)
	f.typ = typeGauge
	f.gaugeFn = fn
}

// Observe records v into a fixed-bucket histogram (DefaultBuckets).
func (r *Registry) Observe(name string, v float64, labels ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f := r.family(name, typeHistogram)
	f.typ = typeHistogram
	s := f.getSeries(renderLabels(labels))
	if s.buckets == nil {
		s.buckets = DefaultBuckets
		s.counts = make([]uint64, len(DefaultBuckets))
	}
	for i, ub := range s.buckets {
		if v <= ub {
			s.counts[i]++
		}
	}
	s.sum += v
	s.count++
}

// Write renders the registry in Prometheus text exposition format.
func (r *Registry) Write(w io.Writer) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	fams := make([]*family, 0, len(r.families))
	for _, f := range r.families {
		fams = append(fams, f)
	}
	sort.Slice(fams, func(i, j int) bool { return fams[i].order < fams[j].order })

	for _, f := range fams {
		help := f.help
		if help == "" {
			help = f.name
		}
		typeName := map[metricType]string{typeCounter: "counter", typeGauge: "gauge", typeHistogram: "histogram"}[f.typ]
		if _, err := fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", f.name, help, f.name, typeName); err != nil {
			return err
		}

		if f.gaugeFn != nil {
			if _, err := fmt.Fprintf(w, "%s %g\n", f.name, f.gaugeFn()); err != nil {
				return err
			}
		}

		keys := make([]string, 0, len(f.series))
		for k := range f.series {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			s := f.series[k]
			switch f.typ {
			case typeHistogram:
				if err := writeHistogram(w, f.name, s); err != nil {
					return err
				}
			default:
				if _, err := fmt.Fprintf(w, "%s%s %g\n", f.name, s.labels, s.value); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func writeHistogram(w io.Writer, name string, s *series) error {
	// Merge the le label into any existing labels.
	openLabels := func(le string) string {
		if s.labels == "" {
			return fmt.Sprintf(`{le=%q}`, le)
		}
		return s.labels[:len(s.labels)-1] + fmt.Sprintf(`,le=%q}`, le)
	}
	for i, ub := range s.buckets {
		if _, err := fmt.Fprintf(w, "%s_bucket%s %d\n", name, openLabels(fmt.Sprintf("%g", ub)), s.counts[i]); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "%s_bucket%s %d\n", name, openLabels("+Inf"), s.count); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "%s_sum%s %g\n", name, s.labels, s.sum); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "%s_count%s %d\n", name, s.labels, s.count)
	return err
}
