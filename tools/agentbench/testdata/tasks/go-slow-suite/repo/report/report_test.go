package report

import "testing"

func TestCents(t *testing.T) {
	for in, want := range map[int64]string{0: "0.00", 5: "0.05", 1234: "12.34", -1234: "-12.34"} {
		if got := Cents(in); got != want {
			t.Errorf("Cents(%d) = %q, want %q", in, got, want)
		}
	}
}
