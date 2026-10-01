package report

import (
	"testing"

	"example.com/slowsuite/ledger"
)

func TestSummaryHidden(t *testing.T) {
	for _, c := range []struct {
		in   []ledger.Entry
		want string
	}{
		{nil, "no entries"},
		{[]ledger.Entry{{Kind: ledger.Credit, Amount: 1000}, {Kind: ledger.Debit, Amount: 250}}, "2 entries: 10.00 in, 2.50 out, balance 7.50"},
		{[]ledger.Entry{{Kind: ledger.Debit, Amount: 50}}, "1 entry: 0.00 in, 0.50 out, balance -0.50"},
		{[]ledger.Entry{{Kind: ledger.Credit, Amount: 5}, {Kind: ledger.Credit, Amount: 7}, {Kind: ledger.Debit, Amount: 1}}, "3 entries: 0.12 in, 0.01 out, balance 0.11"},
	} {
		if got := Summary(c.in); got != c.want {
			t.Errorf("Summary = %q, want %q", got, c.want)
		}
	}
}
