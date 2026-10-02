package money

import "testing"

func TestProrate(t *testing.T) {
	cases := []struct {
		c        Cents
		num, den int64
		want     Cents
	}{
		{3000, 15, 30, 1500},
		{1000, 1, 3, 333},
		{1000, 2, 3, 667},
		{-1000, 2, 3, -667},
		{999, 0, 31, 0},
	}
	for _, c := range cases {
		if got := c.c.Prorate(c.num, c.den); got != c.want {
			t.Errorf("%d.Prorate(%d, %d) = %d, want %d", c.c, c.num, c.den, got, c.want)
		}
	}
}

func TestStringAndParse(t *testing.T) {
	for _, s := range []string{"0.00", "12.34", "-0.05", "1000.10"} {
		c, err := Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		if c.String() != s {
			t.Errorf("Parse(%q).String() = %q", s, c.String())
		}
	}
	if c, _ := Parse("7.5"); c != 750 {
		t.Errorf("Parse(7.5) = %d", c)
	}
	for _, bad := range []string{"", "x", "1.234", "1.", ".5"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) did not fail", bad)
		}
	}
}
