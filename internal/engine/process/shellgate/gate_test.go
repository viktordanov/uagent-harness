package shellgate_test

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/engine/process/shellgate"
	"github.com/viktordanov/uagent-harness/internal/rules"
)

func TestDecide(t *testing.T) {
	c := shellgate.Config{
		Rules: []rules.Rule{
			{Pattern: [][]string{{"rm"}}, Decision: rules.Forbidden, Justification: "never delete"},
			{Pattern: [][]string{{"make"}}, Decision: rules.Allow},
			{Pattern: [][]string{{"git"}, {"push"}}, Decision: rules.Prompt},
		},
		Sandboxed: "/sandboxed", Unsandboxed: "/plain",
	}
	ctx := context.Background()
	cases := map[string]struct {
		args    []string
		argv    []string
		refused string
	}{
		"no rule":            {args: []string{"-c", "ls"}, argv: []string{"/sandboxed", "-c", "ls"}},
		"allow":              {args: []string{"-c", "make test"}, argv: []string{"/plain", "-c", "make test"}},
		"forbidden":          {args: []string{"-c", "ls && rm -rf /"}, refused: "not run: a rule forbids this command: never delete."},
		"prompt, no one":     {args: []string{"-c", "git push"}, refused: "no user can approve it in this headless run"},
		"extra arguments":    {args: []string{"-c", "echo $0", "name"}, argv: []string{"/sandboxed", "-c", "echo $0", "name"}},
		"not a -c call":      {args: []string{"script.sh"}, argv: []string{"/sandboxed", "script.sh"}},
		"an interactive one": {args: nil, argv: []string{"/sandboxed"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			argv, refused := c.Decide(ctx, tc.args, "/work")
			assert.Equal(t, tc.argv, argv)
			if tc.refused == "" {
				assert.Empty(t, refused)
			} else {
				assert.Contains(t, refused, tc.refused)
			}
		})
	}

	never := c
	never.Policy = approval.Never
	_, refused := never.Decide(ctx, []string{"-c", "git push"}, "/work")
	assert.Contains(t, refused, "the approval policy is never")
}

func TestWriteAndMain(t *testing.T) {
	dir := t.TempDir()
	c := shellgate.Config{Rules: []rules.Rule{{Pattern: [][]string{{"rm"}}, Decision: rules.Forbidden}}, Sandboxed: "/bin/sh", Unsandboxed: "/bin/sh"}
	path, err := shellgate.Write(dir, "/usr/local/bin/uah", c)
	require.NoError(t, err)
	again, err := shellgate.Write(dir, "/usr/local/bin/uah", c)
	require.NoError(t, err)
	assert.Equal(t, path, again, "named by content")
	script, err := os.ReadFile(path) //nolint:gosec // the test's own file
	require.NoError(t, err)
	assert.Contains(t, string(script), "exec '/usr/local/bin/uah' "+shellgate.Command+" '{")

	var stderr bytes.Buffer
	assert.Equal(t, 1, shellgate.Main([]string{`{"rules":[{"Pattern":[["rm"]],"Decision":3}]}`, "-c", "rm x"}, &stderr))
	assert.Equal(t, "not run: a rule forbids this command.\n", stderr.String())
	stderr.Reset()
	assert.Equal(t, 127, shellgate.Main([]string{"not json"}, &stderr))
	assert.Contains(t, stderr.String(), "configuration is invalid")
}
