package sandbox_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/sandbox"
)

func TestShellFullAccessIsTheRealShell(t *testing.T) {
	got, err := sandbox.Shell(t.TempDir(), sandbox.Policy{Mode: sandbox.FullAccess}, sandbox.EnvPolicy{}, "/bin/zsh")
	require.NoError(t, err)
	assert.Equal(t, "/bin/zsh", got)
}

func TestShellScript(t *testing.T) {
	p := sandbox.Policy{Mode: sandbox.WorkspaceWrite, Workspace: t.TempDir()}
	if _, err := p.Wrap([]string{"/bin/sh"}); err != nil {
		t.Skipf("no sandbox here: %v", err)
	}
	dir := t.TempDir()
	first, err := sandbox.Shell(dir, p, sandbox.EnvPolicy{}, "/bin/sh")
	require.NoError(t, err)
	again, err := sandbox.Shell(dir, p, sandbox.EnvPolicy{}, "/bin/sh")
	require.NoError(t, err)
	assert.Equal(t, first, again, "the same policy reuses its script")
	info, err := os.Stat(first)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temporary files are left")
	assert.Equal(t, filepath.Dir(first), dir)
}

func TestShellEnvPolicy(t *testing.T) {
	t.Setenv("UAH_TEST_TOKEN", "secret-value")
	t.Setenv("UAH_TEST_PLAIN", "plain value")
	no := false
	env := sandbox.EnvPolicy{IgnoreDefaultExcludes: &no, Set: map[string]string{"UAH_TEST_SET": "it's set"}}
	path, err := sandbox.Shell(t.TempDir(), sandbox.Policy{Mode: sandbox.FullAccess}, env, "/bin/sh")
	require.NoError(t, err)
	script, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(script), "secret-value", "inherited values are not written to disk")
	assert.NotContains(t, string(script), "UAH_TEST_TOKEN")

	out, err := exec.Command(path, "-c", `printf '%s|%s|%s' "$UAH_TEST_TOKEN" "$UAH_TEST_PLAIN" "$UAH_TEST_SET"`).Output()
	require.NoError(t, err)
	assert.Equal(t, "|plain value|it's set", string(out))
}
