package ledger

import "testing"

func TestBalance(t *testing.T) {
	got := Balance([]Entry{{Credit, 1000, "pay"}, {Debit, 250, "lunch"}, {Debit, 50, "coffee"}})
	if got != 700 {
		t.Fatalf("Balance = %d, want 700", got)
	}
}

func TestBalanceEmpty(t *testing.T) {
	if got := Balance(nil); got != 0 {
		t.Fatalf("Balance(nil) = %d, want 0", got)
	}
}
