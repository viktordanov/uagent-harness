package period

import (
	"testing"
	"time"
	_ "time/tzdata"
)

func TestMonthDays(t *testing.T) {
	cases := []struct {
		year  int
		month time.Month
		want  int
	}{
		{2026, time.January, 31},
		{2026, time.February, 28},
		{2028, time.February, 29},
		{2026, time.April, 30},
		{2026, time.December, 31},
	}
	for _, c := range cases {
		if got := Month(c.year, c.month, time.UTC).Days(); got != c.want {
			t.Errorf("%d-%02d: %d days, want %d", c.year, c.month, got, c.want)
		}
	}
}

func TestCalendarDays(t *testing.T) {
	a := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	b := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	if got := CalendarDays(a, b); got != 17 {
		t.Fatalf("CalendarDays = %d, want 17", got)
	}
	if got := CalendarDays(a, a); got != 0 {
		t.Fatalf("CalendarDays(a, a) = %d", got)
	}
}

func TestParseMonth(t *testing.T) {
	p, err := ParseMonth("2026-03", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if p.String() != "2026-03-01..2026-04-01" {
		t.Fatalf("got %s", p)
	}
	if _, err := ParseMonth("March", time.UTC); err == nil {
		t.Fatal("ParseMonth(March) did not fail")
	}
}

func TestOverlap(t *testing.T) {
	m := Month(2026, time.March, time.UTC)
	sub := Period{Start: time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}
	o, ok := m.Overlap(sub)
	if !ok || o.String() != "2026-03-10..2026-04-01" {
		t.Fatalf("Overlap = %s, %v", o, ok)
	}
	before := Period{Start: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), End: m.Start}
	if _, ok := m.Overlap(before); ok {
		t.Fatal("adjacent periods overlap")
	}
	if !m.Contains(m.Start) || m.Contains(m.End) {
		t.Fatal("Contains is not half-open")
	}
}

func TestCalendarDaysAcrossDST(t *testing.T) {
	for _, name := range []string{"Europe/Berlin", "America/New_York"} {
		loc, err := time.LoadLocation(name)
		if err != nil {
			t.Fatal(err)
		}
		a := time.Date(2026, 3, 15, 0, 0, 0, 0, loc)
		b := time.Date(2026, 4, 1, 0, 0, 0, 0, loc)
		if got := CalendarDays(a, b); got != 17 {
			t.Errorf("%s: CalendarDays = %d, want 17", name, got)
		}
		for m := time.January; m <= time.December; m++ {
			want := Month(2026, m, time.UTC).Days()
			if got := Month(2026, m, loc).Days(); got != want {
				t.Errorf("%s %s: %d days, want %d", name, m, got, want)
			}
		}
	}
}
