package main

import (
	"strings"
	"testing"
)

func logq(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var o, e strings.Builder
	code = run(args, strings.NewReader(""), &o, &e)

	return o.String(), e.String(), code
}

func TestLevelAndGrep(t *testing.T) {
	out, _, code := logq(t, "--level", "warn", "--grep", "query", "testdata/app.jsonl")
	if code != 0 || strings.Count(out, "\n") != 2 {
		t.Fatalf("code %d, output:\n%s", code, out)
	}
}

func TestBadLevel(t *testing.T) {
	if _, _, code := logq(t, "--level", "loud", "testdata/app.jsonl"); code != 2 {
		t.Errorf("code %d, want 2", code)
	}
}

func TestMissingFile(t *testing.T) {
	if _, _, code := logq(t, "nope.jsonl"); code != 1 {
		t.Errorf("code %d, want 1", code)
	}
}
