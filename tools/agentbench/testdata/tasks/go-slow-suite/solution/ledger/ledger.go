// Package ledger keeps account entries.
package ledger

// Kind is a credit or a debit.
type Kind int

const (
	Credit Kind = iota
	Debit
)

// Entry is one ledger line; Amount is in cents and always positive.
type Entry struct {
	Kind   Kind
	Amount int64
	Memo   string
}

// Balance is the credits minus the debits, in cents.
func Balance(entries []Entry) int64 {
	var b int64
	for _, e := range entries {
		switch e.Kind {
		case Credit:
			b += e.Amount
		case Debit:
			b -= e.Amount
		}
	}

	return b
}
