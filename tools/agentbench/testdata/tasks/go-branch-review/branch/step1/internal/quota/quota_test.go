package quota

import "testing"

func TestAllowCounts(t *testing.T) {
	q := New(10)
	if !q.Allow("acme") {
		t.Fatal("first request refused")
	}
	if got := q.Remaining("acme"); got != 9 {
		t.Fatalf("Remaining = %d, want 9", got)
	}
	q.Reset()
	if got := q.Remaining("acme"); got != 10 {
		t.Fatalf("after Reset, Remaining = %d, want 10", got)
	}
}
