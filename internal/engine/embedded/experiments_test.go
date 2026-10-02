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

// leanEngine is the env's engine, in Lean mode when lean, with the
// experiments on.
func (e *env) leanEngine(lean bool, names string) *embedded.Engine {
	return embedded.New(embedded.Config{StateDir: e.StateDir, Provider: "openai", Lean: lean, Getenv: withExperiments(e.getenv, names)})
}

// TestPrimedFirstTurn: in Lean mode, a new session's first request carries the workspace
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
		settings := e.settings()
		settings.SystemPrompt = "Base.\n\n# Project instructions\n\n## " + agents + "\n\n@INC.md\n"
		s, err := session.Open(t.Context(), e.leanEngine(on, ""), session.Options{Settings: settings})
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

// TestLean_Effort: in Lean mode, the first request and one with a user
// message go at the session's effort, and one after a command that only
// confirms goes one level lower; a read keeps the effort under the default
// rule, r1, and not under r0. Without Lean mode, every request keeps it.
func TestLean_Effort(t *testing.T) {
	tests := []struct {
		name  string
		lean  bool
		rule  string
		reply fakellm.Reply
		want  []string
	}{
		{"off", false, "", fakellm.Reply{Commands: []string{"echo one"}}, []string{"high", "high", "high"}},
		{"a confirmation", true, "", fakellm.Reply{Commands: []string{"echo one"}}, []string{"high", "medium", "high"}},
		{"a read under r1", true, "", fakellm.Reply{Commands: []string{"cat go.mod"}}, []string{"high", "high", "high"}},
		{"a read under r0", true, "lean-rule=r0", fakellm.Reply{Commands: []string{"cat go.mod"}}, []string{"high", "medium", "high"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, tt.reply, fakellm.Reply{Text: "done"}, fakellm.Reply{Text: "again"})
			s, ev := e.open(t, e.leanEngine(tt.lean, tt.rule), "")
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
			assert.Equal(t, tt.want, efforts)
			if tt.name == "a confirmation" {
				logs, _ := filepath.Glob(filepath.Join(e.StateDir, "runs", "*", "stderr.log"))
				var all string
				for _, l := range logs {
					all += readFile(t, l)
				}
				assert.Contains(t, all, `"effort":"medium","effort_reason":"r1: confirmations: short output"`, "the attempt's diagnostics say why")
				assert.Contains(t, all, `"effort":"high","effort_reason":"r1: first request"`)
			}
		})
	}
}
