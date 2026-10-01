package geo

import "testing"

func TestContainsEdges(t *testing.T) {
	b := Box{Point{0, 0}, Point{10, 10}}
	for _, p := range []Point{{0, 0}, {10, 10}, {0, 5}, {5, 10}} {
		if !b.Contains(p) {
			t.Errorf("Contains(%v) = false on the edge", p)
		}
	}
	if b.Contains(Point{10.5, 5}) {
		t.Error("Contains outside point")
	}
}
