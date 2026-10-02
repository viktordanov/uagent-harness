package dunning

import (
	"testing"
	"time"

	"example.com/billing/internal/invoice"
)

func day(m time.Month, d int) time.Time {
	return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC)
}

func TestDaysOverdue(t *testing.T) {
	due := day(time.April, 15)
	cases := []struct {
		now  time.Time
		want int
	}{
		{day(time.April, 10), 0},
		{due, 0},
		{due.Add(time.Hour), 0},
		{day(time.April, 16), 1},
		{day(time.May, 15), 30},
	}
	for _, c := range cases {
		if got := DaysOverdue(due, c.now); got != c.want {
			t.Errorf("DaysOverdue(%v) = %d, want %d", c.now, got, c.want)
		}
	}
}

func TestAssess(t *testing.T) {
	inv := invoice.Invoice{Number: "INV-1", Due: day(time.April, 15), Total: 12000}
	cases := []struct {
		now   time.Time
		stage Stage
		fee   int64
	}{
		{day(time.April, 1), NotDue, 0},
		{day(time.April, 20), Reminder, 0},
		{day(time.April, 29), LateFee, 240},
		{day(time.May, 20), Suspend, 600},
	}
	for _, c := range cases {
		n := Assess(inv, c.now)
		if n.Stage != c.stage || int64(n.Fee) != c.fee {
			t.Errorf("%v: %+v, want %v fee %d", c.now, n, c.stage, c.fee)
		}
	}
}

func TestMinFee(t *testing.T) {
	inv := invoice.Invoice{Due: day(time.April, 15), Total: 900}
	if n := Assess(inv, day(time.May, 1)); n.Fee != MinFee {
		t.Fatalf("fee %d, want %d", n.Fee, MinFee)
	}
}
