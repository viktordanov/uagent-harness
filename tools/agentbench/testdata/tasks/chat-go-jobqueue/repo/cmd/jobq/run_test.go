package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// jobq runs the command with --store pointed at path after the subcommand.
func jobq(t *testing.T, path string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	if len(args) > 0 && path != "" {
		args = append([]string{args[0], "--store", path}, args[1:]...)
	}
	var o, e strings.Builder
	code = run(args, &o, &e)
	return o.String(), e.String(), code
}

func add(t *testing.T, path string, args ...string) string {
	t.Helper()
	out, errOut, code := jobq(t, path, append([]string{"add"}, args...)...)
	if code != 0 {
		t.Fatalf("add %v: code %d: %s", args, code, errOut)
	}
	return strings.TrimSpace(out)
}

func ids(list string) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(list), "\n") {
		if f := strings.Fields(line); len(f) > 0 {
			out = append(out, f[0])
		}
	}
	return out
}

func TestUsage(t *testing.T) {
	if _, _, code := jobq(t, ""); code != 2 {
		t.Errorf("no command: code %d", code)
	}
	if _, errOut, code := jobq(t, "", "frobnicate"); code != 2 || !strings.Contains(errOut, "unknown command") {
		t.Errorf("unknown command: code %d, %q", code, errOut)
	}
	if out, _, code := jobq(t, "", "help"); code != 0 || !strings.Contains(out, "requeue") {
		t.Errorf("help: code %d, %q", code, out)
	}
}

func TestAddAndList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	a := add(t, path, "echo", "hello world")
	b := add(t, path, "flaky", "2")
	if a != "job-0001" || b != "job-0002" {
		t.Fatalf("IDs %q, %q", a, b)
	}
	out, _, code := jobq(t, path, "list")
	if code != 0 {
		t.Fatalf("list: code %d", code)
	}
	if got := ids(out); len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("list:\n%s", out)
	}
	if !strings.Contains(out, `"hello world"`) {
		t.Errorf("list does not quote the payload:\n%s", out)
	}
	out, _, _ = jobq(t, path, "list", "--state", "done")
	if strings.TrimSpace(out) != "" {
		t.Errorf("list --state done:\n%s", out)
	}
}

func TestAddRejectsBadInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	cases := [][]string{
		{"add"},
		{"add", "teleport"},
		{"add", "flaky", "many"},
		{"add", "sleep", "forever"},
		{"add", "--delay", "-1s", "echo"},
		{"list", "--state", "lost"},
		{"list", "extra"},
	}
	for _, args := range cases {
		if _, _, code := jobq(t, path, args...); code != 2 {
			t.Errorf("%v: code %d, want 2", args, code)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("a rejected command created the store")
	}
}

func TestRunEchoAndFlaky(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	e := add(t, path, "echo", "hi")
	f := add(t, path, "flaky", "1")
	out, errOut, code := jobq(t, path, "run", "--workers", "2")
	if code != 0 {
		t.Fatalf("run: code %d\n%s", code, errOut)
	}
	if !strings.Contains(out, e+": hi\n") {
		t.Errorf("echo output missing:\n%s", out)
	}
	if !strings.Contains(out, "run: 3 claimed, 2 succeeded, 1 retried, 0 failed") {
		t.Errorf("summary:\n%s", out)
	}
	if !strings.Contains(errOut, f+" attempt 1 failed") {
		t.Errorf("retry log:\n%s", errOut)
	}
	done, _, _ := jobq(t, path, "list", "--state", "done")
	if got := ids(done); len(got) != 2 {
		t.Errorf("done:\n%s", done)
	}
}

func TestRunFailsForGood(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	id := add(t, path, "fail")
	_, errOut, code := jobq(t, path, "run", "--quiet")
	if code != 1 || !strings.Contains(errOut, "1 jobs failed") {
		t.Fatalf("run: code %d, %q", code, errOut)
	}
	out, _, code := jobq(t, path, "show", id)
	if code != 0 {
		t.Fatalf("show: code %d", code)
	}
	var job map[string]any
	if err := json.Unmarshal([]byte(out), &job); err != nil {
		t.Fatalf("show is not JSON: %v\n%s", err, out)
	}
	if job["state"] != "failed" || job["attempts"] != float64(3) {
		t.Fatalf("show: %v", job)
	}

	out, _, code = jobq(t, path, "requeue", id)
	if code != 0 || !strings.Contains(out, "requeued") {
		t.Fatalf("requeue: code %d, %q", code, out)
	}
	pending, _, _ := jobq(t, path, "list", "--state", "pending")
	if got := ids(pending); len(got) != 1 || got[0] != id {
		t.Fatalf("pending after requeue:\n%s", pending)
	}
}

func TestPurge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	add(t, path, "echo", "a")
	add(t, path, "echo", "b")
	keep := add(t, path, "echo", "c")
	if _, _, code := jobq(t, path, "run", "--workers", "1"); code != 0 {
		t.Fatal("run failed")
	}
	if _, _, code := jobq(t, path, "purge", "--state", "pending"); code != 2 {
		t.Errorf("purge of pending jobs: code %d", code)
	}
	add(t, path, "echo", "d")
	out, _, code := jobq(t, path, "purge", "--state", "done")
	if code != 0 || !strings.Contains(out, "purged 3 done jobs") {
		t.Fatalf("purge: code %d, %q", code, out)
	}
	all, _, _ := jobq(t, path, "list")
	if got := ids(all); len(got) != 1 || got[0] == keep {
		t.Fatalf("after purge:\n%s", all)
	}
}

func TestRunRecoversInterruptedJobs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	content := `{"version":1,"next_id":1,"jobs":[{"id":"job-0001","kind":"echo","payload":"again","state":"running","attempts":1}]}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errOut, code := jobq(t, path, "run")
	if code != 0 || !strings.Contains(errOut, "1 interrupted jobs") || !strings.Contains(out, "job-0001: again") {
		t.Fatalf("run: code %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
}

func TestStoreFromEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "env.json")
	t.Setenv("JOBQ_STORE", path)
	if _, _, code := jobq(t, "", "add", "echo", "x"); code != 0 {
		t.Fatal("add failed")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("JOBQ_STORE was not used: %v", err)
	}
}
