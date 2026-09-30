package instructions_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/internal/instructions"
)

// TestDefaultPrompt: uah's default prompt is Codex's with exactly the
// nine hunks of default_prompt.diff, the change docs/design/system-prompt.md
// lists, and nothing else.
func TestDefaultPrompt(t *testing.T) {
	diff, err := os.ReadFile("default_prompt.diff")
	require.NoError(t, err)
	assert.Equal(t, 9, strings.Count(string(diff), "\n@@ "), "the documented hunks")

	assert.Equal(t, instructions.DefaultPrompt, applyDiff(t, instructions.CodexPrompt, string(diff)))
	assert.True(t, strings.HasPrefix(instructions.DefaultPrompt, "You are uah, a coding agent in the user's terminal."))
	for _, codexOnly := range []string{"functions.exec", "exec_command", "request_user_input_async", "skills.list", "# Apps", "# Plugins", "Mermaid"} {
		assert.NotContains(t, instructions.DefaultPrompt, codexOnly)
	}
	assert.NotContains(t, instructions.DefaultPrompt, "\n"+instructions.ProjectHeader, "/context can tell it from the instructions")
	assert.NotContains(t, instructions.DefaultPrompt, instructions.EnvironmentOpen)
}

// applyDiff applies a unified diff to text, checking every context and
// removed line against it.
func applyDiff(t *testing.T, text, diff string) string {
	t.Helper()
	lines := strings.SplitAfter(text, "\n")
	var out []string
	next := 0 // the first line not yet copied
	for _, hunk := range strings.Split(diff, "\n@@ ")[1:] {
		body := strings.Split(strings.TrimSuffix(hunk, "\n"), "\n")
		var from, n int
		_, err := fmt.Sscanf(body[0], "-%d,%d", &from, &n)
		require.NoError(t, err, body[0])
		out = append(out, lines[next:from-1]...)
		next = from - 1
		for _, l := range body[1:] {
			op, rest := l[:1], l[1:]+"\n"
			if op != "+" {
				require.Equal(t, lines[next], rest, "hunk %s", body[0])
				next++
			}
			if op != "-" {
				out = append(out, rest)
			}
		}
	}

	return strings.Join(append(out, lines[next:]...), "")
}
