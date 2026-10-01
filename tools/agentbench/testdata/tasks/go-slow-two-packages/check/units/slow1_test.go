package units

import (
	"os"
	"testing"
	"time"
)

// TestSlowMiles runs against a slow simulated service.
func TestSlowMiles(t *testing.T) {
	if _, err := os.ReadFile("testdata/fixture.txt"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Second)
	if m := KmToMiles(1.609344); m < 0.999 || m > 1.001 {
		t.Fatalf("KmToMiles = %v", m)
	}
}
