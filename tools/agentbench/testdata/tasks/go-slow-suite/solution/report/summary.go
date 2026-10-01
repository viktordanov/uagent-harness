package report

import (
	"fmt"

	"example.com/slowsuite/ledger"
)

// Summary describes entries in one line; see docs/report.md.
func Summary(entries []ledger.Entry) string {
	if len(entries) == 0 {
		return "no entries"
	}
	var in, out int64
	for _, e := range entries {
		if e.Kind == ledger.Credit {
			in += e.Amount
		} else {
			out += e.Amount
		}
	}
	noun := "entries"
	if len(entries) == 1 {
		noun = "entry"
	}

	return fmt.Sprintf("%d %s: %s in, %s out, balance %s", len(entries), noun, Cents(in), Cents(out), Cents(in-out))
}
