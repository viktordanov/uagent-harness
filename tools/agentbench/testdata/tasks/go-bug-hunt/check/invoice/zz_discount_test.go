package invoice

import "testing"

func TestDiscountRoundsHalfUp(t *testing.T) {
	for _, c := range []struct {
		lines  []Line
		code   string
		region string
		disc   Cents
		total  Cents
	}{
		{[]Line{{"MUG", 1, 1250}}, "SPRING15", "OR", 1063, 1063},
		{[]Line{{"X", 3, 333}}, "WELCOME10", "OR", 899, 899},
		{[]Line{{"X", 1, 1001}}, "VIP12", "OR", 876, 876},
		{[]Line{{"X", 1, 2900}}, "SPRING15", "OR", 2465, 2465},
		{[]Line{{"X", 1, 1999}}, "VIP12", "TX", 1749, 1858},
		{[]Line{{"X", 1, 2000}}, "", "OR", 2000, 2000},
	} {
		got := Price(Order{Lines: c.lines, Code: c.code, Region: c.region})
		if got.Discounted != c.disc || got.Total != c.total {
			t.Errorf("%v %s: discounted %v total %v, want %v and %v", c.lines, c.code, got.Discounted, got.Total, c.disc, c.total)
		}
	}
}
