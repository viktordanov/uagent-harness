package embedded_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/engine/embedded"
	"github.com/viktordanov/uah/internal/sandbox"
	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/testing/fakellm"
)

// withExperiments serves UAH_EXPERIMENTS as names, and getenv the rest.
func withExperiments(getenv func(string) string, names string) func(string) string {
	return func(key string) string {
		if key == "UAH_EXPERIMENTS" {
			return names
		}

		return getenv(key)
	}
}

// experimentEngine is the env's engine with the experiments on.
func (e *env) experimentEngine(names string) *embedded.Engine {
	return embedded.New(embedded.Config{StateDir: e.StateDir, Provider: "openai", Getenv: withExperiments(e.getenv, names)})
}

// TestAutoVerify_ChecksTheGoModule: with auto-verify, a patch to a Go
// module that no longer builds comes back with go build's verdict and
// output, labelled, in the same tool output; without it, with the summary
// alone.
func TestAutoVerify_ChecksTheGoModule(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain")
	}
	for _, on := range []bool{true, false} {
		names := ""
		if on {
			names = "auto-verify"
		}
		e := newPatchEnv(t, patchOpts{mode: sandbox.FullAccess, experiments: names}, func(ws, _ string) []fakellm.Reply {
			require.NoError(t, os.WriteFile(filepath.Join(ws, "go.mod"), []byte("module example.com/m\n\ngo 1.22\n"), 0o644))

			return applyPatch("*** Add File: main.go\n+package main\n+\n+func main() { undefinedName() }")
		})
		assert.Equal(t, core.StatusOK, e.ev.finished().Status)

		out := e.lastOutput()
		assert.True(t, strings.HasPrefix(out, "Success. Updated the following files:\nA main.go\n"), out)
		if !on {
			assert.Equal(t, "Success. Updated the following files:\nA main.go\n", out)

			continue
		}
		assert.Contains(t, out, "Automatic check after the edit: go build ./... → exit 1")
		assert.Contains(t, out, "undefined: undefinedName")
		assert.Contains(t, out, "do not run it again")
	}
}

// TestAutoVerify_PassingCheckAndNothingToCheck: a Python file that compiles
// reports exit 0 and leaves no __pycache__ in the workspace; a text file
// has nothing to check.
func TestAutoVerify_PassingCheckAndNothingToCheck(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3")
	}
	e := newPatchEnv(t, patchOpts{mode: sandbox.FullAccess, experiments: "auto-verify"}, func(string, string) []fakellm.Reply {
		return []fakellm.Reply{
			{Calls: []fakellm.Call{{Name: "apply_patch", Args: "*** Begin Patch\n*** Add File: a.py\n+print('hi')\n*** End Patch\n", Custom: true}}},
			{Calls: []fakellm.Call{{Name: "apply_patch", Args: "*** Begin Patch\n*** Add File: notes.txt\n+hi\n*** End Patch\n", Custom: true}}},
			{Text: "done"},
		}
	})
	assert.Equal(t, core.StatusOK, e.ev.finished().Status)

	reqs := e.llm.Requests()
	require.Len(t, reqs, 3)
	first := reqs[1].ToolOutputs[0]
	assert.Contains(t, first, `python3 -m py_compile 'a.py' → exit 0`)
	assert.NoDirExists(t, filepath.Join(e.Workspace, "__pycache__"))
	assert.Equal(t, "Success. Updated the following files:\nA notes.txt\n", reqs[2].ToolOutputs[1])
}

// TestPrimedFirstTurn: a new session's first request carries the workspace
// context before the user's message, with git's state, the tracked files,
// and the file AGENTS.md includes; the system prompt is the same as
// without it, and a later run of the session adds no second context.
func TestPrimedFirstTurn(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	systems := map[bool]string{}
	for _, on := range []bool{true, false} {
		e := newEnv(t, fakellm.Reply{Text: "one"}, fakellm.Reply{Text: "two"})
		agents := filepath.Join(e.Workspace, "AGENTS.md")
		require.NoError(t, os.WriteFile(agents, []byte("@INC.md\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(e.Workspace, "INC.md"), []byte("Use rtk for every command."), 0o644))
		require.NoError(t, os.MkdirAll(filepath.Join(e.Workspace, "pkg"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(e.Workspace, "pkg", "a.go"), []byte("package pkg\n"), 0o644))
		for _, args := range [][]string{{"init", "-q", "-b", "trunk"}, {"add", "pkg", "AGENTS.md"}} {
			out, err := exec.Command("git", append([]string{"-C", e.Workspace}, args...)...).CombinedOutput()
			require.NoError(t, err, string(out))
		}
		names := ""
		if on {
			names = "primed-first-turn"
		}
		settings := e.settings()
		settings.SystemPrompt = "Base.\n\n# Project instructions\n\n## " + agents + "\n\n@INC.md\n"
		s, err := session.Open(t.Context(), e.experimentEngine(names), session.Options{Settings: settings})
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
		require.Len(t, reqs, 2)
		systems[on] = strings.ReplaceAll(reqs[0].System, e.Workspace, "<ws>")
		if !on {
			assert.Equal(t, []string{"hello"}, reqs[0].UserTexts)

			continue
		}
		require.Len(t, reqs[0].UserTexts, 2)
		primed := reqs[0].UserTexts[0]
		assert.True(t, strings.HasPrefix(primed, "<workspace_context>"), primed)
		assert.Contains(t, primed, "Use rtk for every command.")
		assert.Contains(t, primed, "Git branch: trunk")
		assert.Contains(t, primed, "A  AGENTS.md")
		assert.Contains(t, primed, "?? INC.md")
		assert.Contains(t, primed, "pkg/ (1 files)")
		assert.LessOrEqual(t, len(primed), 4<<10+100)
		assert.Equal(t, "hello", reqs[0].UserTexts[1])
		assert.Equal(t, []string{primed, "hello", "again"}, reqs[1].UserTexts, "the second run adds no context")
	}
	assert.Equal(t, systems[false], systems[true], "the system prompt is unchanged")
}

// TestEffortByTurn: the first request and one with a user message run at
// the configured effort; a request that only continues after tool results
// runs one level lower. Without the experiment, every request keeps it.
func TestEffortByTurn(t *testing.T) {
	for _, on := range []bool{true, false} {
		e := newEnv(t, fakellm.Reply{Commands: []string{"echo one"}}, fakellm.Reply{Text: "done"}, fakellm.Reply{Text: "again"})
		names := ""
		if on {
			names = "effort-by-turn"
		}
		s, ev := e.open(t, e.experimentEngine(names), "")
		_, err := s.Submit("run it")
		require.NoError(t, err)
		ev.finished()
		ev.idle()
		_, err = s.Submit("more")
		require.NoError(t, err)
		ev.finished()

		var efforts []string
		for _, r := range e.llm.Requests() {
			efforts = append(efforts, r.Effort)
		}
		if on {
			assert.Equal(t, []string{"high", "medium", "high"}, efforts)
		} else {
			assert.Equal(t, []string{"high", "high", "high"}, efforts)
		}
	}
}
