package ids

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDedupeLarge(t *testing.T) {
	const n = 200_000
	in := make([]string, 0, 2*n)
	for i := 0; i < n; i++ {
		in = append(in, fmt.Sprintf("Order-%07d", i))
	}
	for i := 0; i < n; i += 3 {
		in = append(in, strings.ToUpper(fmt.Sprintf(" order-%07d ", i)))
	}
	start := time.Now()
	got := Dedupe(in)
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("Dedupe of %d IDs took %v", len(in), d)
	}
	if len(got) != n || got[0] != "Order-0000000" || got[n-1] != fmt.Sprintf("Order-%07d", n-1) {
		t.Fatalf("wrong result: %d IDs, first %q", len(got), got[0])
	}
}

func TestDedupeSemantics(t *testing.T) {
	for _, c := range []struct{ in, want []string }{
		{nil, nil},
		{[]string{"", " "}, nil},
		{[]string{"x", "X", " x "}, []string{"x"}},
		{[]string{"Straße", "STRASSE", "straße"}, []string{"Straße", "STRASSE"}},
		{[]string{"ǅ", "ǆ", "Ǆ"}, []string{"ǅ"}},
		{[]string{"b", "a", "B", "c", "A"}, []string{"b", "a", "c"}},
	} {
		if got := Dedupe(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Dedupe(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
