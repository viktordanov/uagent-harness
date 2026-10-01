package evalrun_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/internal/compaction"
	"github.com/viktordanov/uah/internal/compaction/eval"
	"github.com/viktordanov/uah/internal/compaction/evalrun"
)

// TestRun_Fixtures runs the evaluation on the recorded fixture sessions
// and holds each strategy to its bounds, as the Markdown benchmarks gate
// the renderer. No model is called: the summaries are the fixture's own
// and stubs.
func TestRun_Fixtures(t *testing.T) {
	rep, err := evalrun.Run(context.Background(), fixtureDir, evalrun.Options{Scratch: t.TempDir(), Strategies: evalrun.Strategies(), Progress: testLog{t}})
	require.NoError(t, err)
	assert.Equal(t, 1, rep.Sessions)
	assert.Equal(t, 3, rep.Cases, "the recorded /compact and the ≥50k and ≥100k requests")
	assert.Zero(t, rep.Skipped)
	assert.Equal(t, 1, rep.Recorded)
	assert.Equal(t, rep.Recorded, rep.Reproduced, "the capture rebuilds exactly the history the session compacted")
	assert.Zero(t, rep.Sources[evalrun.SourceModel], "no model calls")

	row := map[string]eval.Aggregate{}
	for _, r := range rep.Rows {
		row[r.Strategy] = r
	}
	none, codex, ledger := row["none"], row["codex (summary)"], row["summary + ledger"]
	keep5, elide, auto := row["summary + ledger + last 5 calls"], row["elide (10 calls, big after 3)"], row["uah automatic"]

	assert.InDelta(t, 1.0, none.Ratio, 0.0001)
	assert.Zero(t, none.Refetch.Found)
	for _, k := range []eval.Kind{eval.KindChanged, eval.KindFailing, eval.KindTail, eval.KindRead, eval.KindSkills, eval.KindFocus} {
		assert.Equal(t, 1.0, ledger.Recall[k].Share(), "the ledger keeps every %s", k)
		assert.Less(t, codex.Recall[k].Share(), 1.0, "the summary alone loses %s", k)
	}
	ledgerTokens := (ledger.ByActor[eval.ActorSummary] - codex.ByActor[eval.ActorSummary]) / int64(ledger.Cases)
	assert.Positive(t, ledgerTokens)
	assert.LessOrEqual(t, ledgerTokens, int64(compaction.LedgerMaxTokens), "the ledger stays under its cap")
	assert.Less(t, ledger.Ratio, 0.7)
	assert.Equal(t, 1.0, ledger.UserKept.Share(), "user messages stay word for word")

	assert.Less(t, keep5.Refetch.Share(), ledger.Refetch.Share(), "keeping the last calls saves reading them again")
	assert.Less(t, elide.After, none.After, "the stubs free tokens")
	assert.Equal(t, 1.0, elide.SkillsKept.Share(), "skill bodies are never elided")
	assert.Zero(t, elide.CallInput, "elision calls no model")
	assert.LessOrEqual(t, auto.After, none.After)
	assert.True(t, row["codex remote (item ≈ summary)"].Opaque, "an encrypted item's facts cannot be read")
}

func TestWrite_PrintsNumbersOnly(t *testing.T) {
	rep, err := evalrun.Run(context.Background(), fixtureDir, evalrun.Options{Scratch: t.TempDir(), Strategies: evalrun.Only(evalrun.Strategies(), []string{"summary + ledger"})})
	require.NoError(t, err)
	var b strings.Builder
	eval.Write(&b, rep.Rows)
	out := b.String()
	assert.Contains(t, out, "| summary + ledger | 3 |")
	for _, secret := range []string{"helper", "TestHelper", "main.go", "fix the build"} {
		assert.NotContains(t, out, secret, "the tables carry no session content")
	}
}

// TestCut_CancelsOpenOperations cuts a session while its command, which
// appends to a file, has not ended: the capture sees the call canceled and
// never runs the command again (it used to, in the session's own
// workspace).
func TestCut_CancelsOpenOperations(t *testing.T) {
	const id = "a5ad5bba-0726-41cd-bf5d-1d5d4f7b12c6"
	src, err := os.ReadFile(filepath.Join(fixtureDir, id+".session.jsonl"))
	require.NoError(t, err)
	lines := bytes.SplitAfter(src, []byte("\n"))[:7] // through item 6: the first command, ready to run
	root, dir := t.TempDir(), t.TempDir()
	recorded := regexp.MustCompile(`"Directory":"([^"]*)/workspace"`).FindSubmatch(lines[6])
	require.NotNil(t, recorded)
	cut := bytes.ReplaceAll(bytes.Join(lines, nil), recorded[1], []byte(root))
	cut = bytes.ReplaceAll(cut, []byte("cat main.go docs/notes.md"), []byte("echo ran >> stamp.txt"))
	stamp := filepath.Join(root, "workspace", "stamp.txt")
	require.NoError(t, os.MkdirAll(filepath.Dir(stamp), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "state", "sessions", "operations", id), 0o700), "the recorded home's")
	require.NoError(t, os.WriteFile(stamp, []byte("ran\n"), 0o600), "the session's own run")
	require.NoError(t, os.WriteFile(filepath.Join(dir, id+".session.jsonl"), cut, 0o600))

	_, err = evalrun.Capture(context.Background(), t.TempDir(), dir, id, evalrun.Point{Seq: 6})
	require.NoError(t, err)
	b, err := os.ReadFile(stamp)
	require.NoError(t, err)
	assert.Equal(t, "ran\n", string(b), "the capture did not run the command again")

	dst := filepath.Join(t.TempDir(), id+".session.jsonl")
	require.NoError(t, evalrun.Cut(filepath.Join(dir, id+".session.jsonl"), dst, 6))
	f, err := os.Open(dst)
	require.NoError(t, err)
	defer f.Close()
	var last struct {
		Type string
		Data struct{ Operation struct{ Status string } }
	}
	for sc := bufio.NewScanner(f); sc.Scan(); {
		require.NoError(t, json.Unmarshal(sc.Bytes(), &last))
	}
	assert.Equal(t, "operation", last.Type)
	assert.Equal(t, "canceled", last.Data.Operation.Status)
}

// testLog writes the evaluation's progress, such as why a cut was skipped,
// to the test log.
type testLog struct{ t *testing.T }

func (l testLog) Write(p []byte) (int, error) {
	l.t.Log(strings.TrimRight(string(p), "\n"))

	return len(p), nil
}
