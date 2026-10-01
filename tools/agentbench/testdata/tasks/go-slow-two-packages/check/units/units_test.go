package units

import (
	"math"
	"testing"
)

func TestFahrenheitToCelsius(t *testing.T) {
	for f, c := range map[float64]float64{32: 0, 212: 100, -40: -40, 98.6: 37} {
		if got := FahrenheitToCelsius(f); math.Abs(got-c) > 1e-9 {
			t.Errorf("FahrenheitToCelsius(%v) = %v, want %v", f, got, c)
		}
	}
}
