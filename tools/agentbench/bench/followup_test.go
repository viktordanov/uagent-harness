package bench_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/tools/agentbench/bench"
)

func TestPlanUAHOnly(t *testing.T) {
	cfg := bench.Config{
		Tasks:     []bench.Task{{Name: "a"}, {Name: "b", Tags: []string{bench.TagUAHOnly}}},
		Harnesses: []string{"uah", "codex"}, Repeat: 1, Model: "m", Effort: "low",
	}
	var order []string
	for _, k := range bench.Plan(cfg) {
		order = append(order, k.Task+k.Harness)
	}
	assert.Equal(t, []string{"auah", "acodex", "buah"}, order)
}

func TestLoadTaskFollowUps(t *testing.T) {
	write := func(task string) string {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "repo"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "solution"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "task.json"), []byte(task), 0o644))

		return dir
	}
	_, err := bench.LoadTask(write(`{"prompt":"p","follow_ups":["next"],"check":"true","exercises":"e","tags":["go"]}`))
	require.ErrorContains(t, err, "uah-only")
	_, err = bench.LoadTask(write(`{"prompt":"p","follow_ups":["two\nlines"],"check":"true","exercises":"e","tags":["uah-only"]}`))
	require.ErrorContains(t, err, "one non-empty line")
	task, err := bench.LoadTask(write(`{"prompt":"p","follow_ups":["next","last"],"check":"true","exercises":"e","tags":["uah-only"]}`))
	require.NoError(t, err)
	assert.Equal(t, []string{"next", "last"}, task.FollowUps)
}

// fakeUAH answers each message, its argument and then each line of stdin,
// with a run and an idle event, as uah exec --json --stdin does, and
// exits at the end of stdin.
const fakeUAH = `#!/bin/sh
for last; do :; done
n=0
answer() {
	n=$((n+1))
	printf '{"v":1,"type":"user_message","at":"2026-10-02T10:00:0%dZ","text":"%s"}\n' "$n" "$1"
	printf '{"v":1,"type":"model_responded","at":"2026-10-02T10:00:0%d.5Z","duration_ms":400,"stop":"complete","usage":{"input":10,"output":1}}\n' "$n"
	printf '{"v":1,"type":"idle","at":"2026-10-02T10:00:0%d.6Z"}\n' "$n"
}
answer "$last"
case " $* " in *" --stdin "*) ;; *) exit 0 ;; esac
while IFS= read -r line; do answer "$line"; done
`

// TestFollowUps runs a task with two follow-ups against a fake uah: each
// goes in after the run before it is idle, and uah exits after the last.
func TestFollowUps(t *testing.T) {
	work := t.TempDir()
	bin := filepath.Join(work, "uah")
	require.NoError(t, os.WriteFile(bin, []byte(fakeUAH), 0o755))
	dir := filepath.Join(work, "task")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "repo"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "repo", "README.md"), []byte("x\n"), 0o644))
	task := bench.Task{
		Name: "follow", Dir: dir, Prompt: "first", FollowUps: []string{"second", "third"},
		Check: "true", Tags: []string{bench.TagUAHOnly},
	}
	out := filepath.Join(work, "results.jsonl")
	var log bytes.Buffer
	err := bench.Execute(context.Background(), bench.Config{
		Tasks: []bench.Task{task}, Harnesses: []string{"uah", "codex"}, Repeat: 1, Model: "m", Effort: "low",
		Parallel: 1, Timeout: 30 * time.Second, MaxRuns: 5, Work: work, Out: out, UAH: bin, Codex: "false",
		Mode: bench.ModeWorkspace, Log: &log,
	})
	require.NoError(t, err, log.String())
	results, err := bench.LoadResults(out)
	require.NoError(t, err)
	require.Len(t, results, 1, "a uah-only task has no Codex run")
	r := results[0]
	assert.Equal(t, bench.StatusDone, r.Status, r.Error)
	assert.True(t, r.Passed)
	assert.Equal(t, 3, r.Metrics.Turns)
	assert.Equal(t, 3, r.Metrics.Requests)
	stream, err := os.ReadFile(filepath.Join(r.Artifacts, "stream.jsonl"))
	require.NoError(t, err)
	for _, msg := range []string{`"text":"first"`, `"text":"second"`, `"text":"third"`} {
		assert.Contains(t, string(stream), msg)
	}
	assert.Less(t, strings.Index(string(stream), `"second"`), strings.Index(string(stream), `"third"`))
}

func TestParseUAHCompaction(t *testing.T) {
	stream := `{"v":1,"type":"user_message","at":"2026-10-02T10:00:00Z","text":"go"}
{"v":1,"type":"model_responded","at":"2026-10-02T10:00:10Z","duration_ms":10000,"stop":"complete","usage":{"input":250000,"cached_input":200000,"output":100}}
{"v":1,"type":"compaction_started","at":"2026-10-02T10:00:11Z","trigger":"auto","tokens":250000}
{"v":1,"type":"compacted","at":"2026-10-02T10:00:41Z","trigger":"auto","summary":"..."}
{"v":1,"type":"model_responded","at":"2026-10-02T10:00:50Z","duration_ms":9000,"stop":"complete","usage":{"input":30000,"output":100}}
{"v":1,"type":"compaction_started","at":"2026-10-02T10:00:55Z","trigger":"auto","tokens":260000}
`
	start := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	tl, err := bench.ParseUAH(strings.NewReader(stream), start, "")
	require.NoError(t, err)
	require.Len(t, tl.Compactions, 2)
	assert.Equal(t, bench.Compaction{StartMS: 11000, EndMS: 41000, Trigger: "auto", Tokens: 250000}, tl.Compactions[0])
	assert.Equal(t, int64(55000), tl.Compactions[1].EndMS, "an unfinished compaction ends with the stream")

	m := tl.Compute(60*time.Second, price)
	assert.Equal(t, 2, m.Behavior.Compactions)
	assert.Equal(t, int64(30000), m.Behavior.CompactionMS)
	assert.Equal(t, int64(10000+30000+9000), m.ModelMS, "the summary call is model time")
}
