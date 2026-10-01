package stats

import (
	"reflect"
	"testing"
)

func TestMedian(t *testing.T) {
	if got := Median([]float64{3, 1, 2}); got != 2 {
		t.Errorf("Median odd = %v", got)
	}
	if got := Median([]float64{4, 1, 3, 2}); got != 2.5 {
		t.Errorf("Median even = %v", got)
	}
	if got := Median(nil); got != 0 {
		t.Errorf("Median(nil) = %v", got)
	}
}

func TestMedianKeepsInput(t *testing.T) {
	xs := []float64{5, 3, 9, 1}
	Median(xs)
	if !reflect.DeepEqual(xs, []float64{5, 3, 9, 1}) {
		t.Fatalf("Median changed its input: %v", xs)
	}
}
