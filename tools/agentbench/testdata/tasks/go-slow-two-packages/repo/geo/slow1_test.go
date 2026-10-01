package geo

import (
	"os"
	"testing"
	"time"
)

// TestSlowExtend runs against a slow simulated service.
func TestSlowExtend(t *testing.T) {
	if _, err := os.ReadFile("testdata/fixture.txt"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Second)
	if b := (Box{}).Extend(Point{1, 2}); b.NE != (Point{1, 2}) {
		t.Fatalf("Extend = %v", b)
	}
}
