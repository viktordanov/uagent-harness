package invoice_test

import (
	"testing"
	"time"
	_ "time/tzdata"

	"example.com/billing/internal/customer"
	"example.com/billing/internal/invoice"
	"example.com/billing/internal/plan"
)

// The agentbench check.

func TestHiddenProrationAcrossDST(t *testing.T) {
	pro := plan.Plan{ID: "pro", Name: "Pro", Monthly: 3100}
	cases := []struct {
		zone      string
		start     time.Time
		end       time.Time
		month     time.Month
		days      int
		total     int64
		zoneStart bool
	}{
		{"Europe/Berlin", time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC), time.Time{}, time.March, 17, 1700, true},
		{"Europe/Berlin", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Time{}, time.March, 31, 3100, true},
		{"America/New_York", time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC), time.Time{}, time.March, 30, 3000, true},
		{"Europe/Berlin", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC), time.March, 31, 3100, true},
		{"America/New_York", time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC), time.Time{}, time.November, 30, 3100, true},
		{"Europe/Berlin", time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC), time.Time{}, time.October, 22, 2200, true},
	}
	for _, c := range cases {
		loc, err := time.LoadLocation(c.zone)
		if err != nil {
			t.Fatal(err)
		}
		in := func(u time.Time) time.Time {
			if u.IsZero() {
				return u
			}
			return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, loc)
		}
		cu := customer.Customer{ID: "x", PlanID: "pro", Location: loc, Start: in(c.start), End: in(c.end)}
		inv, ok := invoice.Build(cu, pro, 2026, c.month)
		if !ok || len(inv.Lines) != 1 {
			t.Errorf("%s from %s, %s: not billed (%+v)", c.zone, c.start.Format("2006-01-02"), c.month, inv)
			continue
		}
		if inv.Lines[0].Days != c.days || int64(inv.Total) != c.total {
			t.Errorf("%s from %s, %s: %d days, total %d; want %d days, total %d",
				c.zone, c.start.Format("2006-01-02"), c.month, inv.Lines[0].Days, inv.Total, c.days, c.total)
		}
	}
}
