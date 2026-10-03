package main_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/testing/fakellm"
	"github.com/viktordanov/uah/testing/harnesstest"
)

func TestExecReadsThePromptFromStdin(t *testing.T) {
	llm := fakellm.New(t, fakellm.Reply{Text: "read it"})
	e, env := modelEnv(t, llm)

	res := uahWith(t, env, "first line\nsecond line\n", "exec", "-C", e.Workspace, "-")

	require.Equal(t, 0, res.code, res.stderr)
	assert.Equal(t, "read it\n", res.stdout)
	assert.Equal(t, []string{"first line\nsecond line"}, lastRequest(t, llm).UserTexts, "one message, not one per line")
}

func TestExecPromptErrors(t *testing.T) {
	_, env := fakeEnv(t)
	for name, args := range map[string][]string{
		"empty stdin":             {"exec", "-"},
		"- with --stdin":          {"exec", "--stdin", "-"},
		"--ephemeral with --last": {"exec", "--ephemeral", "--last", "hi"},
		"--ephemeral with -s":     {"exec", "--ephemeral", "-s", "abc", "hi"},
	} {
		res := uahWith(t, env, "", args...)
		assert.Equal(t, 2, res.code, name)
		assert.Equal(t, 1, strings.Count(res.stderr, "\n"), name+": "+res.stderr)
	}
}

func TestExecEphemeralKeepsNothing(t *testing.T) {
	e, env := fakeEnv(t, fakellm.Reply{Text: "gone", Commands: []string{"echo hi"}}, fakellm.Reply{Text: "gone"})
	tmp := t.TempDir()
	env = append(env, "TMPDIR="+tmp)

	res := uahWith(t, env, "", "exec", "--ephemeral", "-C", e.Workspace, "hi")

	require.Equal(t, 0, res.code, res.stderr)
	assert.Equal(t, "gone\n", res.stdout)
	for _, name := range []string{"sessions", "runs", "uah.db"} {
		assert.NoFileExists(t, filepath.Join(e.StateDir, name))
		assert.NoDirExists(t, filepath.Join(e.StateDir, name))
	}
	left, err := os.ReadDir(tmp)
	require.NoError(t, err)
	assert.Empty(t, left, "the temporary directory is removed")
	list := uahWith(t, env, "", "sessions", "--all")
	require.Equal(t, 0, list.code, list.stderr)
	assert.NotContains(t, list.stdout, "hi")
}

func TestExecOutputLastMessage(t *testing.T) {
	e, env := fakeEnv(t, fakellm.Reply{Text: "first"}, fakellm.Reply{Text: "the last one"})
	out := filepath.Join(t.TempDir(), "last.txt")

	res := uahWith(t, env, "and more\n", "exec", "-q", "--stdin", "-o", out, "-C", e.Workspace, "hi")

	require.Equal(t, 0, res.code, res.stderr)
	data, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, "the last one", string(data))

	t.Run("no answer writes an empty file", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(e.Workspace, ".env"), []byte("UAH_LLM_BASE_URL=http://evil\n"), 0o600))
		res := uahWith(t, env, "", "exec", "--output-last-message", out, "-C", e.Workspace, "hi")
		assert.Equal(t, 1, res.code, "the blocked run still fails")
		data, err := os.ReadFile(out)
		require.NoError(t, err)
		assert.Empty(t, data)
		assert.Contains(t, res.stderr, "no final answer")
	})
}

// TestExecJSONIsStream checks --json against --stream, under both names.
func TestExecJSONIsStream(t *testing.T) {
	for _, args := range [][]string{{"exec", "--json"}, {"run", "--json"}, {"exec", "--stream"}} {
		e, env := fakeEnv(t)
		res := uahWith(t, env, "", append(args, "-C", e.Workspace, "hi")...)
		require.Equal(t, 0, res.code, res.stderr)
		lines := strings.Split(strings.TrimSpace(res.stdout), "\n")
		var first struct {
			Type string `json:"type"`
		}
		require.NoError(t, json.Unmarshal([]byte(lines[0]), &first), lines[0])
		assert.Equal(t, "session_opened", first.Type, args)
	}
}

// TestExecYolo: a command outside the workspace that workspace mode would
// escalate, and a headless run then refuses, runs under --yolo without
// anyone approving it; --yolo takes no --sandbox or --ask.
func TestExecYolo(t *testing.T) {
	outside := harnesstest.OutsideDir(t, "uah-yolo-test-")
	target := filepath.Join(outside, "x.txt")
	touch := []fakellm.Reply{{Escalated: []string{"touch " + target}}, {Text: "done"}}

	llm := fakellm.New(t, touch...)
	e, env := modelEnv(t, llm)
	res := uahWith(t, env, "", "exec", "-C", e.Workspace, "touch it")
	require.Equal(t, 0, res.code, res.stderr)
	assert.NoFileExists(t, target, "workspace mode: no one approves the escalation")
	assert.Contains(t, lastRequest(t, llm).ToolOutputs[0], "not run")

	llm = fakellm.New(t, touch...)
	e, env = modelEnv(t, llm)
	res = uahWith(t, env, "", "exec", "--yolo", "-C", e.Workspace, "touch it")
	require.Equal(t, 0, res.code, res.stderr)
	assert.FileExists(t, target, "yolo: it runs, unasked")

	for _, extra := range [][]string{{"--sandbox", "read-only"}, {"--ask", "never"}} {
		res := uahWith(t, env, "", append([]string{"exec", "--dangerously-bypass-approvals-and-sandbox", "-C", e.Workspace}, append(extra, "hi")...)...)
		assert.Equal(t, 2, res.code, extra)
		assert.Contains(t, res.stderr, "takes no --sandbox or --ask")
	}
}
