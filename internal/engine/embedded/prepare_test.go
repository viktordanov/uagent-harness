package embedded_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/internal/approval"
	"github.com/viktordanov/uah/internal/contextprep"
	"github.com/viktordanov/uah/internal/engine/embedded"
	"github.com/viktordanov/uah/internal/sandbox"
	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/testing/fakellm"
)

// TestContextPreparation: with context preparation on, a new session's
// first request carries the prepared context before the user's message,
// with git's state, the tracked files, the instruction files, and the
// harness's guidance; the system prompt is the same as
// without it, and a later run of the session adds no second block. A
// subagent's session gets one too; off, nothing is added.
func TestContextPreparation(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	systems := map[string]string{}
	for _, tt := range []struct {
		name     string
		on       bool
		subagent bool
	}{{"off", false, false}, {"on", true, false}, {"subagent", true, true}} {
		e := newEnv(t, fakellm.Reply{Text: "one"}, fakellm.Reply{Text: "two"})
		agents := filepath.Join(e.Workspace, "AGENTS.md")
		require.NoError(t, os.WriteFile(agents, []byte("Rules.\n"), 0o644))
		require.NoError(t, os.MkdirAll(filepath.Join(e.Workspace, "pkg"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(e.Workspace, "pkg", "a.go"), []byte("package pkg\n"), 0o644))
		for _, args := range [][]string{{"init", "-q", "-b", "trunk"}, {"add", "pkg", "AGENTS.md"}} {
			out, err := exec.Command("git", append([]string{"-C", e.Workspace}, args...)...).CombinedOutput()
			require.NoError(t, err, string(out))
		}
		settings := e.settings()
		settings.SystemPrompt = "Base.\n\n# Project instructions\n\n## " + agents + "\n\nRules.\n"
		eng := embedded.New(embedded.Config{
			StateDir: e.StateDir, Provider: "openai", Getenv: e.getenv, ContextPreparation: tt.on, InstructionFiles: []string{agents},
		})
		opts := session.Options{Settings: settings}
		if tt.subagent {
			opts.ID = session.NewSubagentID()
		}
		s, err := session.Open(t.Context(), eng, opts)
		require.NoError(t, err)
		t.Cleanup(func() { _ = s.Close() })
		ev := &events{t: t, s: s}
		_, err = s.Submit("hello")
		require.NoError(t, err)
		ev.finished()
		ev.idle()
		_, err = s.Submit("again")
		require.NoError(t, err)
		ev.finished()

		reqs := e.llm.Requests()
		require.Len(t, reqs, 2, tt.name)
		systems[tt.name] = strings.ReplaceAll(reqs[0].System, e.Workspace, "<ws>")
		if !tt.on {
			assert.Equal(t, []string{"hello"}, reqs[0].UserTexts)

			continue
		}
		require.Len(t, reqs[0].UserTexts, 2, tt.name)
		prepared := reqs[0].UserTexts[0]
		assert.True(t, contextprep.IsPrepared(prepared), prepared)
		assert.Contains(t, prepared, "## workspace\nGit branch: trunk")
		assert.Contains(t, prepared, "A  AGENTS.md")
		assert.Contains(t, prepared, "pkg/ (1 files)")
		assert.Contains(t, prepared, "## agent files\nInstruction files in the system prompt, in order (their @ lines are expanded in place):\n- "+agents+"\n")
		assert.Contains(t, prepared, "## harness\n")
		assert.Contains(t, prepared, "(default 40000)")
		assert.Equal(t, "hello", reqs[0].UserTexts[1])
		assert.Equal(t, []string{prepared, "hello", "again"}, reqs[1].UserTexts, "the second run adds no context")
	}
	assert.Equal(t, systems["off"], systems["on"], "the system prompt is unchanged")
}

// TestContextPreparation_Sandbox: a session in a read-only sandbox is told
// so, with its private $TMPDIR, and the shell and OS it runs in.
func TestContextPreparation_Sandbox(t *testing.T) {
	e := newEnv(t, fakellm.Reply{Text: "one"})
	policy := sandbox.Policy{Mode: sandbox.WorkspaceWrite, Workspace: e.Workspace}
	if _, err := policy.Wrap([]string{"/bin/sh"}); err != nil {
		t.Skipf("no sandbox here: %v", err)
	}
	eng := embedded.New(embedded.Config{
		StateDir: e.StateDir, Provider: "openai", Getenv: e.getenv, ContextPreparation: true,
		Sandbox: &policy, SandboxDir: filepath.Join(e.StateDir, "sandbox"),
	})
	s, err := session.Open(t.Context(), eng, session.Options{Settings: e.settings().WithMode(approval.ModeReadOnly)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	ev := &events{t: t, s: s}
	_, err = s.Submit("hello")
	require.NoError(t, err)
	ev.finished()

	prepared := e.llm.Requests()[0].UserTexts[0]
	require.True(t, contextprep.IsPrepared(prepared), prepared)
	assert.Contains(t, prepared, "## environment\nCommands run in ")
	assert.Contains(t, prepared, "## sandbox\nSandbox: read-only. Commands can read any file and write only $TMPDIR.")
	assert.Contains(t, prepared, "$TMPDIR ("+session.TempDir(filepath.Join(e.StateDir, "sessions"), s.ID())+")")
}
