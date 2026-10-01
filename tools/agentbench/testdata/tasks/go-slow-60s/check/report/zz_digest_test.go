package report

import "testing"

func TestHiddenDigest(t *testing.T) {
	lines := []Line{
		{"bolt", "seat", 300}, {"acme", "seat", 500}, {"acme", "fee", 300},
		{"cora", "seat", 150}, {"cora", "fee", 0}, {"dyna", "seat", 300},
	}
	if got, want := Digest(lines, 2), "4 customers, total $15.50\n- acme: $8.00\n- bolt: $3.00\n"; got != want {
		t.Errorf("top 2:\n%q\nwant\n%q", got, want)
	}
	if got, want := Digest(lines, 0), "4 customers, total $15.50\n- acme: $8.00\n- bolt: $3.00\n- dyna: $3.00\n- cora: $1.50\n"; got != want {
		t.Errorf("top 0:\n%q\nwant\n%q", got, want)
	}
	if got, want := Digest(lines, 10), Digest(lines, 0); got != want {
		t.Errorf("top 10 = %q", got)
	}
	if got := Digest(nil, 3); got != "0 customers, total $0.00\n" {
		t.Errorf("empty = %q", got)
	}
}
