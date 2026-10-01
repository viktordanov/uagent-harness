package embedded_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/internal/engine/embedded"
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

// TestPreambleWake: with preamble-wake, the system message starts with the
// preamble that describes the wake policy, and the rest of it is the same;
// without it, with the runner's.
func TestPreambleWake(t *testing.T) {
	systems := map[bool]string{}
	for _, on := range []bool{true, false} {
		e := newEnv(t, fakellm.Reply{Text: "done"})
		names := ""
		if on {
			names = "preamble-wake"
		}
		s, ev := e.open(t, e.experimentEngine(names), "")
		_, err := s.Submit("hello")
		require.NoError(t, err)
		ev.finished()
		systems[on] = e.llm.Requests()[0].System
	}
	assert.Contains(t, systems[true], "Their results arrive together")
	assert.Contains(t, systems[true], "A call still running after 5 minutes wakes you with its output so far")
	assert.NotContains(t, systems[true], "placeholder")
	assert.Contains(t, systems[false], "a call still running shows a placeholder")
	_, rest, _ := strings.Cut(systems[true], "I believe in you!")
	_, restOff, _ := strings.Cut(systems[false], "I believe in you!")
	assert.Equal(t, restOff, rest, "only the preamble changes")
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
