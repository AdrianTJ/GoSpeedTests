package stats

import (
	"math"
	"testing"
)

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestPercentile_Empty(t *testing.T) {
	if got := Percentile(nil, 75); got != 0 {
		t.Errorf("Percentile(nil, 75) = %v, want 0", got)
	}
}

func TestPercentile_Single(t *testing.T) {
	for _, p := range []float64{0, 50, 75, 100} {
		if got := Percentile([]float64{42}, p); got != 42 {
			t.Errorf("Percentile([42], %v) = %v, want 42", p, got)
		}
	}
}

func TestPercentile_KnownValues(t *testing.T) {
	// 1..10: linear interpolation between closest ranks.
	vs := []float64{10, 9, 8, 7, 6, 5, 4, 3, 2, 1} // unsorted on purpose
	cases := []struct {
		p    float64
		want float64
	}{
		{0, 1},
		{50, 5.5},
		{75, 7.75},
		{95, 9.55},
		{100, 10},
	}
	for _, c := range cases {
		if got := Percentile(vs, c.p); !almostEqual(got, c.want) {
			t.Errorf("Percentile(1..10, %v) = %v, want %v", c.p, got, c.want)
		}
	}
}

func TestPercentile_DoesNotMutateInput(t *testing.T) {
	vs := []float64{3, 1, 2}
	Percentile(vs, 50)
	if vs[0] != 3 || vs[1] != 1 || vs[2] != 2 {
		t.Errorf("input slice mutated: %v", vs)
	}
}

func TestMean(t *testing.T) {
	if got := Mean(nil); got != 0 {
		t.Errorf("Mean(nil) = %v, want 0", got)
	}
	if got := Mean([]float64{1, 2, 3, 4}); !almostEqual(got, 2.5) {
		t.Errorf("Mean(1..4) = %v, want 2.5", got)
	}
}

func TestMedian(t *testing.T) {
	if got := Median([]float64{1, 2, 3}); !almostEqual(got, 2) {
		t.Errorf("Median odd = %v, want 2", got)
	}
	if got := Median([]float64{1, 2, 3, 4}); !almostEqual(got, 2.5) {
		t.Errorf("Median even = %v, want 2.5", got)
	}
}
