package report

import "testing"

func TestTotals(t *testing.T) {
	got := Totals([]Line{{"a", "x", 100}, {"b", "y", 50}, {"a", "z", 25}})
	if got["a"] != 125 || got["b"] != 50 {
		t.Fatalf("Totals = %v", got)
	}
}
