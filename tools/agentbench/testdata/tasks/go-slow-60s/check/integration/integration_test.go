package integration

import (
	"fmt"
	"testing"
	"time"

	"example.com/billing/money"
	"example.com/billing/report"
)

// Each scenario stands in for a run against a staging billing database,
// which takes a few seconds; they run one after another.
func scenario(t *testing.T, lines []report.Line, want map[string]money.Cents) {
	t.Helper()
	time.Sleep(5 * time.Second)
	got := report.Totals(lines)
	for c, w := range want {
		if got[c] != w {
			t.Errorf("%s: total %s, want %s", c, money.Format(got[c]), money.Format(w))
		}
	}
}

func TestScenarios(t *testing.T) {
	for i := range 10 {
		t.Run(fmt.Sprintf("month-%02d", i+1), func(t *testing.T) {
			n := money.Cents(100 * (i + 1))
			scenario(t, []report.Line{
				{Customer: "acme", Item: "seat", Amount: n},
				{Customer: "acme", Item: "fee", Amount: 5},
				{Customer: "bolt", Item: "seat", Amount: n * 2},
			}, map[string]money.Cents{"acme": n + 5, "bolt": n * 2})
		})
	}
}
