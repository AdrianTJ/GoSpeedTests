package metrics

import (
	"strings"
	"sync"
	"testing"
)

func render(t *testing.T, r *Registry) string {
	t.Helper()
	var b strings.Builder
	if err := r.Write(&b); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	return b.String()
}

func TestCounter_ExactOutput(t *testing.T) {
	r := NewRegistry()
	r.Help("jobs_total", "Jobs processed.")
	r.CounterInc("jobs_total", "status", "COMPLETED")
	r.CounterInc("jobs_total", "status", "COMPLETED")
	r.CounterInc("jobs_total", "status", "FAILED")

	want := `# HELP jobs_total Jobs processed.
# TYPE jobs_total counter
jobs_total{status="COMPLETED"} 2
jobs_total{status="FAILED"} 1
`
	if got := render(t, r); got != want {
		t.Errorf("output mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestGauge_StaticAndFunc(t *testing.T) {
	r := NewRegistry()
	r.GaugeSet("temperature", 21.5, "room", "lab")
	r.GaugeFunc("queue_depth", func() float64 { return 7 })

	out := render(t, r)
	if !strings.Contains(out, `temperature{room="lab"} 21.5`) {
		t.Errorf("missing static gauge:\n%s", out)
	}
	if !strings.Contains(out, "# TYPE queue_depth gauge") || !strings.Contains(out, "queue_depth 7") {
		t.Errorf("missing func gauge:\n%s", out)
	}
}

func TestHistogram_BucketsAndSum(t *testing.T) {
	r := NewRegistry()
	r.Observe("duration_seconds", 0.5) // le=1 and up
	r.Observe("duration_seconds", 7)   // le=10 and up
	r.Observe("duration_seconds", 999) // only +Inf

	out := render(t, r)
	checks := []string{
		"# TYPE duration_seconds histogram",
		`duration_seconds_bucket{le="1"} 1`,
		`duration_seconds_bucket{le="5"} 1`,
		`duration_seconds_bucket{le="10"} 2`,
		`duration_seconds_bucket{le="600"} 2`,
		`duration_seconds_bucket{le="+Inf"} 3`,
		"duration_seconds_sum 1006.5",
		"duration_seconds_count 3",
	}
	for _, c := range checks {
		if !strings.Contains(out, c) {
			t.Errorf("missing %q in:\n%s", c, out)
		}
	}
}

func TestLabels_SortedAndEscaped(t *testing.T) {
	r := NewRegistry()
	r.CounterInc("m", "zebra", "z", "alpha", "a")
	out := render(t, r)
	if !strings.Contains(out, `m{alpha="a",zebra="z"} 1`) {
		t.Errorf("labels not sorted by key:\n%s", out)
	}

	r2 := NewRegistry()
	r2.GaugeSet("g", 1, "url", `http://x/"quote"\back`+"\nnewline")
	out2 := render(t, r2)
	if !strings.Contains(out2, `g{url="http://x/\"quote\"\\back\nnewline"} 1`) {
		t.Errorf("label value not escaped per exposition format:\n%s", out2)
	}
}

func TestHistogramWithLabels_MergesLe(t *testing.T) {
	r := NewRegistry()
	r.Observe("d", 2, "kind", "x")
	out := render(t, r)
	if !strings.Contains(out, `d_bucket{kind="x",le="5"} 1`) {
		t.Errorf("le label not merged into existing labels:\n%s", out)
	}
	if !strings.Contains(out, `d_sum{kind="x"} 2`) || !strings.Contains(out, `d_count{kind="x"} 1`) {
		t.Errorf("sum/count keep original labels:\n%s", out)
	}
}

func TestConcurrentUse(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				r.CounterInc("c", "w", "x")
				r.Observe("h", float64(j%10))
				r.GaugeSet("g", float64(j))
			}
		}()
	}
	wg.Wait()

	out := render(t, r)
	if !strings.Contains(out, `c{w="x"} 8000`) {
		t.Errorf("concurrent counter lost increments:\n%s", out)
	}
	if !strings.Contains(out, "h_count 8000") {
		t.Errorf("concurrent histogram lost observations:\n%s", out)
	}
}

func TestNegativeCounterAddIgnored(t *testing.T) {
	r := NewRegistry()
	r.CounterAdd("c", -5)
	r.CounterInc("c")
	if out := render(t, r); !strings.Contains(out, "c 1") {
		t.Errorf("negative add should be ignored:\n%s", out)
	}
}
