package app_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/app"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

// TestSetup_ReviewPolicyFile sets [review] policy_file: the auto-review
// request's instructions carry the file's policy instead of Codex's, and a
// missing or empty file stops the session from starting.
func TestSetup_ReviewPolicyFile(t *testing.T) {
	_, in := setupEnv(t)
	t.Setenv("OPENAI_API_KEY", "test-key")
	policy := filepath.Join(t.TempDir(), "review.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(in.ConfigPath), 0o700))
	require.NoError(t, os.WriteFile(in.ConfigPath, []byte("[review]\npolicy_file = \""+policy+"\"\n"), 0o600))

	_, err := app.Setup(context.Background(), in, io.Discard)
	require.ErrorContains(t, err, "failed to read review.policy_file")
	require.NoError(t, os.WriteFile(policy, []byte("\n"), 0o600))
	_, err = app.Setup(context.Background(), in, io.Discard)
	require.ErrorContains(t, err, "is empty")

	require.NoError(t, os.WriteFile(policy, []byte("CUSTOM-POLICY: deny anything that touches /etc.\n"), 0o600))
	outside := t.TempDir()
	llm := fakellm.New(t,
		fakellm.Reply{Escalated: []string{"touch " + filepath.Join(outside, "x.txt")}},
		fakellm.Reply{Text: `{"risk_level":"low","user_authorization":"high","outcome":"allow","rationale":"Asked for."}`},
		fakellm.Reply{Text: "done"},
	)
	in.Provider, in.Model, in.BaseURL = "openai", "gpt-test", llm.URL
	res, err := app.Setup(context.Background(), in, io.Discard)
	require.NoError(t, err)
	s, err := session.Open(context.Background(), res.Engine, res.Options)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	_, err = s.Submit("touch the file")
	require.NoError(t, err)
	waitFinished(t, s)

	reqs := llm.Requests()
	require.Len(t, reqs, 3)
	review := reqs[1]
	assert.Contains(t, review.System, "CUSTOM-POLICY: deny anything that touches /etc.")
	assert.NotContains(t, review.System, "{{ tenant_policy_config }}")
	assert.True(t, strings.Contains(review.System, `"outcome"`), "the output contract stays")
	assert.NotContains(t, reqs[0].System, "CUSTOM-POLICY")
}
