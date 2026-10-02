// Package period has the date arithmetic the billing code shares: billing
// months and day counts.
package period

import (
	"fmt"
	"time"
)

// Period is the half-open range [Start, End).
type Period struct {
	Start time.Time
	End   time.Time
}

// Month returns the calendar month as a Period in loc: from midnight on the
// first to midnight on the first of the next month.
func Month(year int, month time.Month, loc *time.Location) Period {
	start := time.Date(year, month, 1, 0, 0, 0, 0, loc)
	return Period{Start: start, End: start.AddDate(0, 1, 0)}
}

// ParseMonth reads "2026-03" as that month in loc.
func ParseMonth(s string, loc *time.Location) (Period, error) {
	t, err := time.ParseInLocation("2006-01", s, loc)
	if err != nil {
		return Period{}, fmt.Errorf("period: bad month %q, want YYYY-MM", s)
	}
	return Month(t.Year(), t.Month(), loc), nil
}

// Days returns the number of days in p.
func (p Period) Days() int { return DaysBetween(p.Start, p.End) }

// Contains reports whether t is in p.
func (p Period) Contains(t time.Time) bool {
	return !t.Before(p.Start) && t.Before(p.End)
}

// Overlap returns the part of p that is also in q, and false when they do
// not overlap.
func (p Period) Overlap(q Period) (Period, bool) {
	start, end := p.Start, p.End
	if q.Start.After(start) {
		start = q.Start
	}
	if q.End.Before(end) {
		end = q.End
	}
	if !start.Before(end) {
		return Period{}, false
	}
	return Period{Start: start, End: end}, true
}

// String formats p as "2026-03-01..2026-04-01".
func (p Period) String() string {
	return p.Start.Format("2006-01-02") + ".." + p.End.Format("2006-01-02")
}

// DaysBetween returns the number of whole days from a to b. Both are
// expected to be midnights in the same location.
func DaysBetween(a, b time.Time) int {
	return int(b.Sub(a).Hours() / 24)
}
