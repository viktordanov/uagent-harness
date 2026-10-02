package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The agentbench check.
func hidden(t *testing.T, stdin string, args ...string) (lines int, stderr string, code int) {
	t.Helper()
	var o, e strings.Builder
	code = run(args, strings.NewReader(stdin), &o, &e)

	return strings.Count(o.String(), "\n"), e.String(), code
}

func TestHiddenAfter(t *testing.T) {
	const f = "testdata/app.jsonl"
	cases := []struct {
		args []string
		want int
	}{
		{[]string{"--after", "2026-03-01T11:00:00Z", f}, 3},
		{[]string{"--after", "2026-03-01T11:05:00Z", f}, 3},
		{[]string{"--after", "2026-03-01T12:00:00+01:00", f}, 3},
		{[]string{"--after", "2h", f}, 5},
		{[]string{"--after", "90m", f}, 3},
		{[]string{"--after", "2h", "--level", "warn", f}, 2},
		{[]string{"--level", "warn", "--after", "2026-03-01T10:20:00Z", "--format", "json", f}, 2},
	}
	for _, c := range cases {
		n, stderr, code := hidden(t, "", c.args...)
		if code != 0 || n != c.want {
			t.Errorf("%v: code %d, %d lines, want 0 and %d (stderr %q)", c.args, code, n, c.want, stderr)
		}
		if strings.Contains(stderr, "deprecated") {
			t.Errorf("%v: --after warns: %q", c.args, stderr)
		}
	}
}

func TestHiddenRelativeToNewest(t *testing.T) {
	in := `{"time":"2020-01-01T00:00:00Z","level":"info","msg":"a"}
{"time":"2020-01-01T00:30:00Z","level":"info","msg":"b"}
{"time":"2020-01-01T01:00:00Z","level":"info","msg":"c"}
`
	if n, _, code := hidden(t, in, "--after", "30m"); code != 0 || n != 2 {
		t.Errorf("stdin --after 30m: code %d, %d lines, want 2", code, n)
	}
	other := filepath.Join(t.TempDir(), "later.jsonl")
	if err := os.WriteFile(other, []byte(`{"time":"2026-03-01T13:00:00Z","level":"info","msg":"late"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if n, _, code := hidden(t, "", "--after", "2h", "testdata/app.jsonl", other); code != 0 || n != 4 {
		t.Errorf("two files --after 2h: code %d, %d lines, want 4 (newest across both)", code, n)
	}
}

func TestHiddenSinceAlias(t *testing.T) {
	n, stderr, code := hidden(t, "", "--since", "2h", "testdata/app.jsonl")
	if code != 0 || n != 5 {
		t.Errorf("--since 2h: code %d, %d lines, want 0 and 5", code, n)
	}
	if !strings.Contains(stderr, "logq: --since is deprecated, use --after") {
		t.Errorf("--since warning: %q", stderr)
	}
	if n, _, code := hidden(t, "", "--since", "2026-03-01T11:00:00Z", "testdata/app.jsonl"); code != 0 || n != 3 {
		t.Errorf("--since time: code %d, %d lines, want 3", code, n)
	}
}

func TestHiddenBadValue(t *testing.T) {
	for _, v := range []string{"yesterday", "10 o'clock"} {
		if _, _, code := hidden(t, "", "--after", v, "testdata/app.jsonl"); code != 2 {
			t.Errorf("--after %s: code %d, want 2", v, code)
		}
	}
}
