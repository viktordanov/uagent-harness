package bench_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/tools/agentbench/bench"
)

// sessionWriter writes a session file as uah records it: a header, then
// items, a tool call status carrying its operation's snapshot.
type sessionWriter struct {
	t     *testing.T
	lines []string
	seq   int
}

func newSession(t *testing.T, id string) *sessionWriter {
	t.Helper()
	w := &sessionWriter{t: t}
	w.line(map[string]any{"type": "session", "data": map[string]any{"Version": 2, "Session": map[string]any{"ID": id, "CreatedAt": time.Now()}}})

	return w
}

func (w *sessionWriter) line(v any) {
	b, err := json.Marshal(v)
	require.NoError(w.t, err)
	w.lines = append(w.lines, string(b))
}

func (w *sessionWriter) item(kind string, data any, ops ...any) {
	w.seq++
	w.line(map[string]any{"type": "item", "data": map[string]any{
		"Item":       map[string]any{"Sequence": w.seq, "RecordedAt": time.Now(), "Kind": kind, "Data": data},
		"Operations": ops,
	}})
}

// request records a model response issuing the calls (name, arguments),
// returning their ids.
func (w *sessionWriter) request(calls ...[2]string) []string {
	var out []any
	var ids []string
	for i, c := range calls {
		id := fmt.Sprintf("call-%d-%d", w.seq, i)
		ids = append(ids, id)
		out = append(out, map[string]any{"Type": "tool_call", "Data": map[string]any{"CallID": id, "Name": c[0], "Arguments": c[1]}})
	}
	w.item("turn", map[string]any{"ID": "t"})
	w.item("model_response", map[string]any{"TurnID": "t", "Response": map[string]any{"Stop": "complete", "Output": out}})

	return ids
}

// shell finishes the call with a shell operation's result.
func (w *sessionWriter) shell(call, cmd string, code int, stderr string, truncated bool) {
	op := map[string]any{"ID": "op-" + call, "Type": "shell", "Status": "completed", "State": map[string]any{
		"Input":        map[string]any{"Command": cmd},
		"Result":       map[string]any{"Out": "", "Err": stderr, "ExitCode": code},
		"OutTruncated": truncated,
	}}
	w.item("tool_call_status", map[string]any{"TurnID": "t", "CallID": call, "Status": map[string]any{"WaitingFor": []string{"op-" + call}}}, op)
}

func (w *sessionWriter) failed(call, err string) {
	w.item("tool_call_status", map[string]any{"TurnID": "t", "CallID": call, "Status": map[string]any{"Error": err}})
}

func (w *sessionWriter) write(dir, id string) {
	require.NoError(w.t, os.MkdirAll(filepath.Join(dir, "sessions"), 0o755))
	require.NoError(w.t, os.WriteFile(filepath.Join(dir, "sessions", id+".session.jsonl"), []byte(strings.Join(w.lines, "\n")+"\n"), 0o644))
}

func bash(cmd string) [2]string {
	b, _ := json.Marshal(map[string]string{"command": cmd})

	return [2]string{"Bash", string(b)}
}

// TestCountFailures reads a main session and a subagent's and counts each
// kind of failure the owner's sessions showed.
func TestCountFailures(t *testing.T) {
	state := t.TempDir()
	main := newSession(t, "main")
	main.request([2]string{"SkillUse", `{"name":"i-have-adhd"}`})
	r := main.request(bash("x=1; for f in *.go; do echo $f; done"), bash("go test ./..."), bash("sh -c 'cat <<EOF\nhi\nEOF'"))
	main.shell(r[0], "x=1", 127, "fish: Unsupported use of '='. In fish, please use 'set x 1'.", false)
	main.shell(r[1], "go test ./...", 1, "go: failed to initialize build cache at /Users/o/Library/Caches/go-build: mkdir /Users/o/Library/Caches/go-build/aa: operation not permitted", false)
	main.shell(r[2], "sh -c", 0, "", false)
	r = main.request(bash("cat big.log"), bash("rg -n AGENTS.md ~"), bash("GOCACHE=$PWD/.cache go test ./..."))
	main.shell(r[0], "cat big.log", 0, "", true)
	main.shell(r[1], "rg", 0, "", false)
	main.shell(r[2], "go test", 0, "", false)
	r = main.request(bash("rtk sed -n '1,200p' big.log"), bash("curl -s https://example.com"), [2]string{"apply_patch", "*** Begin Patch"})
	main.shell(r[0], "sed", 0, "", false)
	main.shell(r[1], "curl", 6, "curl: (6) Could not resolve host: example.com", false)
	main.failed(r[2], "auto-reviewer denied: writes outside the workspace")
	main.write(state, "main")

	sub := newSession(t, "sub")
	sub.shell(sub.request(bash("cat > /tmp/x <<EOF\nEOF"))[0], "cat", 1, "sh: can't create temp file for here document: Operation not permitted", false)
	sub.write(state, "sub")

	f, err := bench.CountFailures(state, "main")
	require.NoError(t, err)
	assert.Equal(t, 9, f.Commands)
	assert.Equal(t, 11, f.Calls)
	assert.Equal(t, 5, f.Failed)
	assert.Equal(t, map[string]int{"fish-syntax": 1, "go-cache": 1, "network": 1, "reviewer-denied": 1, "sandbox-tmpdir": 1}, f.ByCause)
	assert.Equal(t, 1, f.SubagentFailed)
	assert.Equal(t, 1, f.Cause(bench.CauseFish))
	assert.Equal(t, 1, f.Sandbox())
	assert.Equal(t, 0, f.Other())
	assert.Equal(t, 1, f.Wrapped)
	assert.Equal(t, 2, f.Heredocs)
	assert.Equal(t, 1, f.GoCacheOverrides)
	assert.Equal(t, 1, f.AgentsLookups)
	assert.Equal(t, 1, f.Truncated)
	assert.Equal(t, 1, f.TruncatedRetried, "the next request reads big.log again")
	assert.Equal(t, 1, f.Rereads)
	assert.Equal(t, 1, f.SkillUses)
	assert.Equal(t, 0, f.IncludeReads)

	report := bench.FailuresReport([]bench.Result{{Key: bench.Key{Task: "a", Harness: "uah", Repeat: 1}, Status: bench.StatusDone, Failures: f, Shell: "/opt/homebrew/bin/fish"}})
	assert.Contains(t, report, "Harness shell: /opt/homebrew/bin/fish.")
	assert.Contains(t, report, "| a | uah | 1 | fail |")
	assert.Contains(t, report, "fish-syntax 1")
}

func TestClassifyCommand(t *testing.T) {
	for _, c := range []struct{ cmd, out, want string }{
		{"for f in *; do echo; done", "fish: Missing end to balance this for loop", "fish-syntax"},
		{"go test ./...", "open /Users/o/Library/Caches/go-build/01/x: operation not permitted", "go-cache"},
		{"rtk go test ./...", "Go test: 0 passed, 1 failed\n... [build failed]\n  pattern ./...: open /Users/o/Library/Caches/go-build/54/54d4...\n", "go-cache"},
		{"go test ./...", "go: creating work dir: mkdir /var/folders/xy/T/go-build1: operation not permitted", "sandbox-tmpdir"},
		{"git commit -m x", "fatal: Unable to create '/w/.git/index.lock': Operation not permitted", "sandbox-git-write"},
		{"touch ~/x", "touch: /Users/o/x: Operation not permitted", "sandbox-write"},
		{"curl localhost:1", "curl: (7) Failed to connect to localhost port 1", "network"},
		{"go test ./...", "--- FAIL: TestX", "test-fail"},
		{"rg nothing", "", "search-no-match"},
		{"sed -i 's/a/b/' f", "sed: 1: \"f\": undefined label", "bsd-vs-gnu"},
	} {
		code := 1
		assert.Equal(t, c.want, bench.ClassifyCommand(c.cmd, c.out, code), c.cmd)
	}
}

func TestReadFiles(t *testing.T) {
	assert.Equal(t, []string{"a.go", "b.go"}, bench.ReadFiles("cat a.go b.go"))
	assert.Equal(t, []string{"x/y.go"}, bench.ReadFiles("rtk sed -n '10,80p' x/y.go"))
	assert.Equal(t, []string{"f.txt"}, bench.ReadFiles("nl -ba f.txt | sed -n '1,20p'"))
	assert.Equal(t, []string{"log"}, bench.ReadFiles("head -n 50 log && echo done"))
	assert.Empty(t, bench.ReadFiles("sed -i 's/a/b/' f.go"))
	assert.Empty(t, bench.ReadFiles("cat > out <<EOF"))
}
