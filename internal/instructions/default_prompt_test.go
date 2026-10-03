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

// TestWithoutQuestionTool: with the question tool off, hunk 4 is item 63's
// text again, word for word, and nothing names the tool.
func TestWithoutQuestionTool(t *testing.T) {
	without := instructions.WithoutQuestionTool(instructions.DefaultPrompt)
	assert.NotContains(t, without, "request_user_input")
	assert.Contains(t, without, "You ask the user for missing information, a preference, constraint, or clarification in the `final` channel, which ends your turn. "+
		"You can ask multiple questions in a single final message. Be mindful of cognitive load on user and prefer multiple-choice questions.")
	assert.Equal(t, len(instructions.DefaultPrompt)-479, len(without), "only the tool's sentences go")
	assert.Equal(t, "custom", instructions.WithoutQuestionTool("custom"))
}
