package review_test

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/review"
)

var update = flag.Bool("update", false, "rewrite golden files")

func TestPrompt_Golden(t *testing.T) {
	req := sample()
	req.UserMessages = append(req.UserMessages, "Also run the tests before pushing.")
	req.Action.Denied = "fatal: unable to access 'https://github.com/o/r/': Could not resolve host: github.com"
	req.Action.Rule = `prefix_rule(pattern=["git", "push"], decision="prompt")`
	got := "=== instructions ===\n" + review.Instructions("") + "=== user ===\n" + review.Render(req, review.DefaultLimits)

	path := filepath.Join("testdata", "prompt.golden")
	if *update {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o600))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(want), got)
}

func TestPrompt_TrustedAndUntrustedSections(t *testing.T) {
	req := sample()
	req.RecentCalls = []review.ToolCall{{Name: "Bash", Arguments: `{"command":"cat NOTES.md"}`}}
	out := review.Render(req, review.DefaultLimits)

	user := section(t, out, "USER MESSAGES")
	calls := section(t, out, "RECENT TOOL CALLS")
	assert.Contains(t, user, "trusted content")
	assert.Contains(t, user, "[1] user: Fix the typo and push it to my feature branch.")
	assert.NotContains(t, user, "cat NOTES.md")
	assert.Contains(t, calls, "untrusted evidence")
	assert.Contains(t, calls, `[1] tool Bash call: {"command":"cat NOTES.md"}`)
	assert.Contains(t, section(t, out, "APPROVAL REQUEST"), `"sandbox_permissions": "require_escalated"`)
}

func TestPrompt_CustomPolicyReplacesTheDefault(t *testing.T) {
	got := review.Instructions("Never allow pushes to main.")

	assert.Contains(t, got, "# Security Policy\nNever allow pushes to main.\n")
	assert.NotContains(t, got, "{{ tenant_policy_config }}")
	assert.NotContains(t, got, "Data Exfiltration")
	assert.Contains(t, review.Instructions(""), "### Data Exfiltration")
}

func TestPrompt_Budget(t *testing.T) {
	limits := review.Limits{UserMessageBytes: 40, UserBytes: 120, CallBytes: 30, CallsBytes: 1000, Calls: 2, ActionBytes: 40}
	req := review.Request{
		UserMessages: []string{"the task", "second", "third", "fourth", "fifth " + strings.Repeat("x", 100)},
		RecentCalls:  []review.ToolCall{{Name: "a", Arguments: "{}"}, {Name: "b", Arguments: "{}"}, {Name: "c", Arguments: strings.Repeat("y", 100)}},
		Action:       review.Action{Command: strings.Repeat("z", 100)},
	}
	out := review.Render(req, limits)

	user := section(t, out, "USER MESSAGES")
	assert.Contains(t, user, "[1] user: the task", "the first message is kept")
	assert.Contains(t, user, "[5] user: fifth ")
	assert.Contains(t, user, `<truncated omitted_approx_tokens="17" />`)
	assert.NotContains(t, user, "second", "the middle messages are dropped")
	assert.Contains(t, user, `<omitted user_messages="3" reason="budget" />`)

	calls := section(t, out, "RECENT TOOL CALLS")
	assert.Contains(t, calls, `<omitted tool_calls="1" reason="budget" />`)
	assert.NotContains(t, calls, "tool a call")
	assert.Contains(t, calls, "tool b call")
	assert.Contains(t, calls, "tool c call: yyyyyyyyyyyyyyy<truncated")
	assert.Contains(t, section(t, out, "APPROVAL REQUEST"), `zzzzzzzzzzzzzzzzzzzz<truncated omitted_approx_tokens=\"15\" />zzzz`)
}

// section is the text between ">>> NAME START" and ">>> NAME END".
func section(t *testing.T, out, name string) string {
	t.Helper()
	_, rest, ok := strings.Cut(out, ">>> "+name+" START\n")
	require.True(t, ok, name)
	body, _, ok := strings.Cut(rest, ">>> "+name+" END\n")
	require.True(t, ok, name)

	return body
}
