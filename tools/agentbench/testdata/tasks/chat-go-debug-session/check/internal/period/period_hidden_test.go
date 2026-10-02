package period_test

import (
	"testing"
	"time"
	_ "time/tzdata"

	"example.com/billing/internal/period"
)

// The agentbench check.

func hiddenLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestHiddenCalendarDaysDST(t *testing.T) {
	for _, name := range []string{"Europe/Berlin", "America/New_York", "Australia/Sydney", "UTC"} {
		loc := hiddenLoc(t, name)
		d := func(m time.Month, day int) time.Time { return time.Date(2026, m, day, 0, 0, 0, 0, loc) }
		cases := []struct {
			a, b time.Time
			want int
		}{
			{d(3, 15), d(4, 1), 17},
			{d(3, 1), d(4, 1), 31},
			{d(3, 28), d(3, 30), 2},
			{d(10, 1), d(11, 1), 31},
			{d(10, 25), d(11, 2), 8},
			{d(4, 1), d(4, 1), 0},
			{d(1, 1), d(12, 31), 364},
		}
		for _, c := range cases {
			if got := period.CalendarDays(c.a, c.b); got != c.want {
				t.Errorf("%s: CalendarDays(%s, %s) = %d, want %d", name, c.a.Format("01-02"), c.b.Format("01-02"), got, c.want)
			}
		}
	}
}

func TestHiddenMonthDaysDST(t *testing.T) {
	for _, name := range []string{"Europe/Berlin", "America/New_York", "Australia/Sydney"} {
		loc := hiddenLoc(t, name)
		for m := time.January; m <= time.December; m++ {
			want := period.Month(2026, m, time.UTC).Days()
			if got := period.Month(2026, m, loc).Days(); got != want {
				t.Errorf("%s 2026-%02d: %d days, want %d", name, m, got, want)
			}
		}
	}
}
