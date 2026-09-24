package approval_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/rules"
)

// TestDecideTyped pins how a command the user typed follows the rules: a
// forbid refuses it and an allow runs it outside the sandbox; a prompt
// rule, even with the policy never, runs it in the sandbox, since typing
// it was the approval.
func TestDecideTyped(t *testing.T) {
	forbid, err := rules.FromPrefixes([]string{"rm"}, rules.Forbidden, "test")
	require.NoError(t, err)
	allow, err := rules.FromPrefixes([]string{"git commit"}, rules.Allow, "test")
	require.NoError(t, err)
	prompt, err := rules.FromPrefixes([]string{"git push"}, rules.Prompt, "test")
	require.NoError(t, err)
	a := approval.New(approval.Config{Policy: approval.Never, Rules: slices.Concat(forbid, allow, prompt)})

	for command, want := range map[string]approval.Run{
		"ls && rm -rf build":   approval.Deny,
		"git commit -m wip":    approval.Unsandboxed,
		"git push origin main": approval.Sandboxed,
		"go test ./...":        approval.Sandboxed,
	} {
		d := a.DecideTyped(command)
		assert.Equal(t, want, d.Run, command)
		if want == approval.Deny {
			assert.Equal(t, "not run: a rule forbids this command.", d.Reason)
		}
	}
}
