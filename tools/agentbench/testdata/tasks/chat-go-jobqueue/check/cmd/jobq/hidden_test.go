package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The agentbench check: jobq driven through run(), as a user would.

func hiddenJobq(t *testing.T, store string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	full := append([]string{args[0], "--store", store}, args[1:]...)
	var o, e strings.Builder
	code = run(full, &o, &e)
	return o.String(), e.String(), code
}

func hiddenAdd(t *testing.T, store string, args ...string) string {
	t.Helper()
	out, errOut, code := hiddenJobq(t, store, append([]string{"add"}, args...)...)
	id := strings.TrimSpace(out)
	if code != 0 || id == "" {
		t.Fatalf("add %v: code %d, stdout %q, stderr %q", args, code, out, errOut)
	}
	return id
}

// hiddenListed returns the set of job IDs that jobq list --state S prints.
func hiddenListed(t *testing.T, store, state string) map[string]bool {
	t.Helper()
	out, errOut, code := hiddenJobq(t, store, "list", "--state", state)
	if code != 0 {
		t.Fatalf("list --state %s: code %d: %s", state, code, errOut)
	}
	set := make(map[string]bool)
	for _, f := range strings.Fields(out) {
		set[f] = true
	}
	return set
}

func hiddenExpect(t *testing.T, store, state string, in, notIn []string) {
	t.Helper()
	got := hiddenListed(t, store, state)
	for _, id := range in {
		if !got[id] {
			t.Errorf("list --state %s does not show %s", state, id)
		}
	}
	for _, id := range notIn {
		if got[id] {
			t.Errorf("list --state %s shows %s", state, id)
		}
	}
}

func hiddenAttempts(t *testing.T, store, id string) int {
	t.Helper()
	out, errOut, code := hiddenJobq(t, store, "show", id)
	if code != 0 {
		t.Fatalf("show %s: code %d: %s", id, code, errOut)
	}
	var job map[string]any
	if err := json.Unmarshal([]byte(out), &job); err != nil {
		t.Fatalf("show %s is not JSON: %v\n%s", id, err, out)
	}
	n, ok := job["attempts"].(float64)
	if !ok {
		t.Fatalf("show %s has no attempts: %s", id, out)
	}
	return int(n)
}

func hiddenStats(t *testing.T, store string) map[string]int {
	t.Helper()
	out, errOut, code := hiddenJobq(t, store, "stats")
	if code != 0 {
		t.Fatalf("stats: code %d: %s", code, errOut)
	}
	var got map[string]int
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &got); err != nil {
		t.Fatalf("stats is not one JSON object of counts: %v\n%s", err, out)
	}
	return got
}

func TestHiddenMaxAttemptsRaised(t *testing.T) {
	store := filepath.Join(t.TempDir(), "jobs.json")
	a := hiddenAdd(t, store, "flaky", "3")
	b := hiddenAdd(t, store, "flaky", "1")
	c := hiddenAdd(t, store, "echo", "x")
	_, errOut, code := hiddenJobq(t, store, "run", "--max-attempts", "4", "--max-backoff", "1ms", "--workers", "2")
	if code != 0 {
		t.Fatalf("run --max-attempts 4: code %d: %s", code, errOut)
	}
	hiddenExpect(t, store, "done", []string{a, b, c}, nil)
	if n := hiddenAttempts(t, store, a); n != 4 {
		t.Errorf("%s (flaky 3) took %d attempts, want 4", a, n)
	}
}

func TestHiddenMaxAttemptsDefault(t *testing.T) {
	store := filepath.Join(t.TempDir(), "jobs.json")
	a := hiddenAdd(t, store, "flaky", "3")
	b := hiddenAdd(t, store, "flaky", "2")
	hiddenJobq(t, store, "run", "--max-backoff", "1ms")
	hiddenExpect(t, store, "dead", []string{a}, []string{b})
	hiddenExpect(t, store, "done", []string{b}, []string{a})
	if n := hiddenAttempts(t, store, a); n != 3 {
		t.Errorf("%s used %d attempts by default, want 3", a, n)
	}
}

func TestHiddenMaxAttemptsOne(t *testing.T) {
	store := filepath.Join(t.TempDir(), "jobs.json")
	a := hiddenAdd(t, store, "fail")
	b := hiddenAdd(t, store, "flaky", "1")
	hiddenJobq(t, store, "run", "--max-attempts", "1", "--max-backoff", "1ms")
	hiddenExpect(t, store, "dead", []string{a, b}, nil)
	if n := hiddenAttempts(t, store, a); n != 1 {
		t.Errorf("%s: %d attempts with --max-attempts 1", a, n)
	}
}

func TestHiddenMaxBackoffBoundsWaits(t *testing.T) {
	store := filepath.Join(t.TempDir(), "jobs.json")
	a := hiddenAdd(t, store, "fail")
	start := time.Now()
	hiddenJobq(t, store, "run", "--max-attempts", "10", "--max-backoff", "5ms")
	// Nine waits of at most 5ms each, plus polling. Without the cap the
	// default backoff alone takes well over a second.
	if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
		t.Errorf("run with --max-backoff 5ms took %v", elapsed)
	}
	hiddenExpect(t, store, "dead", []string{a}, nil)
	if n := hiddenAttempts(t, store, a); n != 10 {
		t.Errorf("%s: %d attempts with --max-attempts 10", a, n)
	}
}

func TestHiddenDeadLetter(t *testing.T) {
	store := filepath.Join(t.TempDir(), "jobs.json")
	dead := hiddenAdd(t, store, "fail")
	ok := hiddenAdd(t, store, "echo", "fine")
	hiddenJobq(t, store, "run", "--max-attempts", "2", "--max-backoff", "1ms")
	hiddenExpect(t, store, "dead", []string{dead}, []string{ok})
	hiddenExpect(t, store, "done", []string{ok}, []string{dead})
	hiddenExpect(t, store, "pending", nil, []string{dead, ok})

	b, err := os.ReadFile(store)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"dead"`) {
		t.Fatalf("the store file does not record the dead job:\n%s", b)
	}
	// A later run leaves the dead job alone.
	hiddenJobq(t, store, "run", "--max-backoff", "1ms")
	hiddenExpect(t, store, "dead", []string{dead}, nil)
	if n := hiddenAttempts(t, store, dead); n != 2 {
		t.Errorf("dead job ran again: %d attempts", n)
	}
}

func TestHiddenStats(t *testing.T) {
	store := filepath.Join(t.TempDir(), "jobs.json")
	want := map[string]int{"pending": 0, "running": 0, "done": 0, "dead": 0}
	hiddenSameCounts(t, "empty store", hiddenStats(t, store), want)

	hiddenAdd(t, store, "echo", "a")
	hiddenAdd(t, store, "echo", "b")
	hiddenAdd(t, store, "fail")
	hiddenJobq(t, store, "run", "--max-attempts", "1", "--max-backoff", "1ms")
	hiddenAdd(t, store, "echo", "c")
	hiddenAdd(t, store, "fail")
	hiddenAdd(t, store, "flaky", "1")
	want = map[string]int{"pending": 3, "running": 0, "done": 2, "dead": 1}
	hiddenSameCounts(t, "after a run", hiddenStats(t, store), want)
}

func hiddenSameCounts(t *testing.T, what string, got, want map[string]int) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: stats = %v, want exactly %v", what, got, want)
		return
	}
	for k, v := range want {
		if n, ok := got[k]; !ok || n != v {
			t.Errorf("%s: stats = %v, want %v", what, got, want)
			return
		}
	}
}

var hiddenEchoLine = regexp.MustCompile(`(?m)^(job-[0-9]+): `)

func TestHiddenNoDoubleRuns(t *testing.T) {
	store := filepath.Join(t.TempDir(), "jobs.json")
	const n = 150
	for i := 0; i < n; i++ {
		hiddenAdd(t, store, "echo", strconv.Itoa(i))
	}
	out, errOut, code := hiddenJobq(t, store, "run", "--workers", "16", "--max-backoff", "1ms")
	if code != 0 {
		t.Fatalf("run --workers 16: code %d: %s", code, errOut)
	}
	runs := make(map[string]int)
	for _, m := range hiddenEchoLine.FindAllStringSubmatch(out, -1) {
		runs[m[1]]++
	}
	if len(runs) != n {
		t.Errorf("%d jobs printed, want %d", len(runs), n)
	}
	for id, k := range runs {
		if k != 1 {
			t.Errorf("%s ran %d times", id, k)
		}
	}
	hiddenSameCounts(t, "after the run", hiddenStats(t, store), map[string]int{"pending": 0, "running": 0, "done": n, "dead": 0})
}
