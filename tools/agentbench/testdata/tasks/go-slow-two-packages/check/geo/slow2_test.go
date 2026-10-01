package geo

import (
	"os"
	"testing"
	"time"
)

// TestSlowInside runs against a slow simulated service.
func TestSlowInside(t *testing.T) {
	if _, err := os.ReadFile("testdata/fixture.txt"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Second)
	if !(Box{Point{0, 0}, Point{10, 10}}).Contains(Point{5, 5}) {
		t.Fatal("inside point not contained")
	}
}
