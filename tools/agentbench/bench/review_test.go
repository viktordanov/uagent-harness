package bench_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/tools/agentbench/bench"
)

// fakeReviewer writes its arguments to the file after -o, as `uah review
// -o` and `codex review -o` write the review, and prints the JSON line
// `uah review --json` prints, which the timeline's parser must accept.
const fakeReviewer = `#!/bin/sh
args="$*"
while [ $# -gt 0 ]; do
	if [ "$1" = -o ]; then printf '%s\n' "$args" > "$2"; fi
	shift
done
printf '{"findings":[],"overall_correctness":"patch is correct","target":"changes against main","status":"ok","error":"","model":"m","effort":"low","duration_ms":1200,"usage":{"input":10,"cached_input":0,"output":2,"reasoning":0}}\n'
`

// TestReviewRuns runs a task in review mode (-review): each harness gets
// its review command against the task's base instead of the prompt, and
// the check reads the review it wrote to REVIEW.md.
func TestReviewRuns(t *testing.T) {
	work := t.TempDir()
	bin := filepath.Join(work, "reviewer")
	require.NoError(t, os.WriteFile(bin, []byte(fakeReviewer), 0o755))
	dir := filepath.Join(work, "task")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "repo"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "repo", "a.go"), []byte("package a\n"), 0o644))
	task := bench.Task{
		Name: "rev", Dir: dir, Prompt: "review it", ReviewBase: "main",
		Check: `grep -q -- "review.* --base main -o .*/REVIEW.md" REVIEW.md && ! grep -q "review it" REVIEW.md`,
	}
	out := filepath.Join(work, "results.jsonl")
	var log bytes.Buffer
	err := bench.Execute(context.Background(), bench.Config{
		Tasks: []bench.Task{task}, Harnesses: []string{"uah", "codex"}, Repeat: 1, Model: "m", Effort: "low",
		Parallel: 1, Timeout: 30 * time.Second, MaxRuns: 5, Work: work, Out: out, UAH: bin, Codex: bin,
		Mode: bench.ModeWorkspace, Log: &log, Review: true,
	})
	require.NoError(t, err, log.String())
	results, err := bench.LoadResults(out)
	require.NoError(t, err)
	require.Len(t, results, 2)
	for _, r := range results {
		assert.Equal(t, bench.CommandReview, r.Command)
		assert.Equal(t, r.Harness+"@review", r.Label())
		assert.Equal(t, bench.StatusDone, r.Status, r.Error)
		assert.True(t, r.Passed, r.Check.Output)
		if r.Harness == bench.HarnessUAH {
			assert.Empty(t, r.Error, "uah review's JSON line parses as a stream")
		}
	}
}
