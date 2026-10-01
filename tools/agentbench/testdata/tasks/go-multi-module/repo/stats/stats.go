// Package stats has small descriptive statistics.
package stats

import "sort"

// Median returns the middle value of xs (the mean of the two middle ones
// for an even count) without changing xs. It returns 0 for no values.
func Median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sort.Float64s(xs)
	n := len(xs)
	if n%2 == 0 {
		return (xs[n/2-1] + xs[n/2]) / 2
	}

	return xs[n/2]
}
