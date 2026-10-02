package dunning_test

import (
	"testing"
	"time"
	_ "time/tzdata"

	"example.com/billing/internal/dunning"
	"example.com/billing/internal/invoice"
)

// The agentbench check.

func TestHiddenDaysOverdueAcrossDST(t *testing.T) {
	cases := []struct {
		zone     string
		due, now [2]int // month, day in 2026
		want     int
	}{
		{"Europe/Berlin", [2]int{3, 20}, [2]int{4, 3}, 14},
		{"Europe/Berlin", [2]int{3, 15}, [2]int{4, 14}, 30},
		{"Europe/Berlin", [2]int{3, 28}, [2]int{3, 30}, 2},
		{"America/New_York", [2]int{3, 1}, [2]int{3, 15}, 14},
		{"Europe/Berlin", [2]int{10, 20}, [2]int{11, 3}, 14},
		{"UTC", [2]int{4, 15}, [2]int{4, 29}, 14},
	}
	for _, c := range cases {
		loc, err := time.LoadLocation(c.zone)
		if err != nil {
			t.Fatal(err)
		}
		due := time.Date(2026, time.Month(c.due[0]), c.due[1], 0, 0, 0, 0, loc)
		now := time.Date(2026, time.Month(c.now[0]), c.now[1], 0, 0, 0, 0, loc)
		if got := dunning.DaysOverdue(due, now); got != c.want {
			t.Errorf("%s: DaysOverdue(%s, %s) = %d, want %d", c.zone, due.Format("01-02"), now.Format("01-02"), got, c.want)
		}
	}
}

func TestHiddenAssessAcrossDST(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	inv := invoice.Invoice{Number: "INV-x", Due: time.Date(2026, 3, 20, 0, 0, 0, 0, berlin), Total: 12000}
	n := dunning.Assess(inv, time.Date(2026, 4, 3, 0, 0, 0, 0, berlin))
	if n.Stage != dunning.LateFee || n.Fee != 240 || n.DaysOverdue != 14 {
		t.Errorf("Assess 14 days after due in Berlin: %+v, want late-fee, fee 240", n)
	}
	n = dunning.Assess(inv, time.Date(2026, 4, 19, 0, 0, 0, 0, berlin))
	if n.Stage != dunning.Suspend || n.DaysOverdue != 30 {
		t.Errorf("Assess 30 days after due in Berlin: %+v, want suspend", n)
	}
}
