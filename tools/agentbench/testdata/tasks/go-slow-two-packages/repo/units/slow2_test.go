package units

import (
	"os"
	"testing"
	"time"
)

// TestSlowBoiling runs against a slow simulated service.
func TestSlowBoiling(t *testing.T) {
	if _, err := os.ReadFile("testdata/fixture.txt"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Second)
	if f := CelsiusToFahrenheit(100); f != 212 {
		t.Fatalf("CelsiusToFahrenheit(100) = %v", f)
	}
}
