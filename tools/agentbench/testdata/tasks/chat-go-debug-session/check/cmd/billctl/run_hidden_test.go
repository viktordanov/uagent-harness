package main

import (
	"strings"
	"testing"
)

// The agentbench check.

func TestHiddenBerlinInvoice(t *testing.T) {
	var o, e strings.Builder
	if code := run([]string{"invoice", "-month", "2026-03", "../../testdata/customers.json"}, &o, &e); code != 0 {
		t.Fatalf("exit %d: %s", code, e.String())
	}
	out := o.String()
	i := strings.Index(out, "INV-2026-03-umbrella")
	if i < 0 {
		t.Fatalf("no umbrella invoice:\n%s", out)
	}
	block := out[i:]
	if j := strings.Index(block[1:], "INV-"); j >= 0 {
		block = block[:j+1]
	}
	if !strings.Contains(block, "17 of 31 days") || !strings.Contains(block, "17.00") {
		t.Errorf("umbrella invoice:\n%s\nwant 17 of 31 days, 17.00", block)
	}
	if !strings.Contains(out, "156.90 total") {
		t.Errorf("run total:\n%s\nwant 156.90 total", out)
	}
}
