// Package stats provides the small set of descriptive statistics the rest of
// the codebase needs. Web performance reporting is percentile-based (Google
// scores Core Web Vitals at the 75th percentile), so Percentile is the
// workhorse; Mean exists only for backward-compatible "avg" fields.
package stats

import "sort"

// Percentile returns the p-th percentile (0-100) of values using linear
// interpolation between closest ranks. It returns 0 for an empty slice.
// The input slice is copied and never mutated.
func Percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}

	rank := p / 100 * float64(len(sorted)-1)
	lo := int(rank)
	frac := rank - float64(lo)
	if lo+1 >= len(sorted) {
		return sorted[lo]
	}
	return sorted[lo] + frac*(sorted[lo+1]-sorted[lo])
}

// Mean returns the arithmetic mean of values, or 0 for an empty slice.
func Mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// Median returns the 50th percentile of values.
func Median(values []float64) float64 {
	return Percentile(values, 50)
}
