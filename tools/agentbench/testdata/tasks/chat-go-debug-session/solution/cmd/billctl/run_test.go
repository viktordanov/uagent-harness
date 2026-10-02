package main

import (
	"strings"
	"testing"
)

func runCLI(args ...string) (stdout, stderr string, code int) {
	var o, e strings.Builder
	code = run(args, &o, &e)
	return o.String(), e.String(), code
}

func TestInvoiceCommand(t *testing.T) {
	out, stderr, code := runCLI("invoice", "-month", "2026-03", "../../testdata/customers.json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{
		"INV-2026-03-acme",
		"INV-2026-03-globex",
		"Pro plan, 17 of 31 days",
		"4 invoices",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	// globex (UTC) and umbrella (Europe/Berlin) start the same day.
	if n := strings.Count(out, "17 of 31 days"); n != 2 {
		t.Errorf("%d invoices of 17 of 31 days, want 2:\n%s", n, out)
	}
}

func TestOverdueCommand(t *testing.T) {
	out, stderr, code := runCLI("overdue", "-month", "2026-03", "-today", "2026-05-20", "../../testdata/customers.json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(out, "INV-2026-03-acme\t35 days\tsuspend\t6.00") {
		t.Errorf("output:\n%s", out)
	}
}

func TestUsageErrors(t *testing.T) {
	cases := [][]string{
		{},
		{"refund"},
		{"invoice", "../../testdata/customers.json"},
		{"invoice", "-month", "March", "../../testdata/customers.json"},
		{"overdue", "-month", "2026-03", "../../testdata/customers.json"},
	}
	for _, args := range cases {
		if _, _, code := runCLI(args...); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
	if _, _, code := runCLI("invoice", "-month", "2026-03", "missing.json"); code != 1 {
		t.Errorf("missing file: exit %d, want 1", code)
	}
}
