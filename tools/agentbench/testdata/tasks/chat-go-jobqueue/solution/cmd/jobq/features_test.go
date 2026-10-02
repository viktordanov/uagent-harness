package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMaxAttemptsFlag(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	a := add(t, path, "flaky", "3")
	if _, errOut, code := jobq(t, path, "run", "--max-attempts", "4", "--max-backoff", "1ms"); code != 0 {
		t.Fatalf("run: code %d\n%s", code, errOut)
	}
	done, _, _ := jobq(t, path, "list", "--state", "done")
	if got := ids(done); len(got) != 1 || got[0] != a {
		t.Fatalf("done:\n%s", done)
	}
	for _, bad := range []string{"0", "-2", "x"} {
		if _, _, code := jobq(t, path, "run", "--max-attempts", bad); code != 2 {
			t.Errorf("--max-attempts %s: code %d", bad, code)
		}
	}
}

func TestMaxBackoffCapsWaits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	add(t, path, "fail")
	start := time.Now()
	jobq(t, path, "run", "--quiet", "--max-attempts", "10", "--max-backoff", "2ms")
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("run took %v with --max-backoff 2ms", elapsed)
	}
	if _, _, code := jobq(t, path, "run", "--max-backoff", "0s"); code != 2 {
		t.Errorf("--max-backoff 0s: code %d", code)
	}
}

func TestDeadLetter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	dead := add(t, path, "fail")
	ok := add(t, path, "echo", "x")
	if _, _, code := jobq(t, path, "run", "--quiet", "--max-attempts", "2", "--max-backoff", "1ms"); code != 1 {
		t.Fatalf("run: code %d, want 1", code)
	}
	out, _, code := jobq(t, path, "list", "--state", "dead")
	if got := ids(out); code != 0 || len(got) != 1 || got[0] != dead {
		t.Fatalf("list --state dead: code %d\n%s", code, out)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `"state": "dead"`) {
		t.Fatalf("store file:\n%s", b)
	}
	done, _, _ := jobq(t, path, "list", "--state", "done")
	if got := ids(done); len(got) != 1 || got[0] != ok {
		t.Fatalf("done:\n%s", done)
	}
}

func TestStats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	out, _, code := jobq(t, path, "stats")
	if code != 0 || strings.TrimSpace(out) != `{"pending":0,"running":0,"done":0,"dead":0}` {
		t.Fatalf("empty stats: code %d, %q", code, out)
	}
	add(t, path, "echo", "a")
	add(t, path, "fail")
	jobq(t, path, "run", "--quiet", "--max-attempts", "1")
	add(t, path, "echo", "b")
	out, _, _ = jobq(t, path, "stats")
	var got map[string]int
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stats is not JSON: %v: %q", err, out)
	}
	want := map[string]int{"pending": 1, "running": 0, "done": 1, "dead": 1}
	if len(got) != len(want) {
		t.Fatalf("stats = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("stats = %v, want %v", got, want)
		}
	}
}
