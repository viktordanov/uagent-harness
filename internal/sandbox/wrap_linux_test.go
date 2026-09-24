//go:build linux

package sandbox_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/sandbox"
)

// requireBwrap skips the test unless bwrap can create the namespaces it
// needs; on Ubuntu 24.04 AppArmor blocks unprivileged user namespaces unless
// kernel.apparmor_restrict_unprivileged_userns is 0. CI sets
// UAH_REQUIRE_BWRAP=1 to fail instead of skipping.
func requireBwrap(t *testing.T) {
	t.Helper()
	skip := t.Skipf
	if os.Getenv("UAH_REQUIRE_BWRAP") != "" {
		skip = t.Fatalf
	}
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		skip("bwrap is not installed")
	}
	out, err := exec.Command(bwrap, "--unshare-user", "--unshare-net", "--ro-bind", "/", "/", "true").CombinedOutput()
	if err != nil {
		skip("bwrap cannot create user namespaces: %v: %s", err, out)
	}
}

type linuxRun struct {
	base, ws, outside string
}

// newLinuxRun makes a workspace with a .git directory and a directory outside
// every writable root. Both live under the package directory, not /tmp, which
// is writable in workspace-write.
func newLinuxRun(t *testing.T) linuxRun {
	t.Helper()
	requireBwrap(t)
	base := goldenBase(t)
	t.Setenv("TMPDIR", mkdir(t, base, "tmpdir"))
	r := linuxRun{base: base, ws: mkdir(t, base, "ws", ".git"), outside: mkdir(t, base, "outside")}
	require.NoError(t, os.WriteFile(filepath.Join(r.ws, ".git", "config"), []byte("[core]\n"), 0o600))
	for _, mode := range []sandbox.Mode{sandbox.ReadOnly, sandbox.WorkspaceWrite} {
		code, out := sh(t, r.policy(mode, false), "true")
		require.Equal(t, 0, code, "the sandbox itself fails in %s: %s", mode, out)
	}

	return r
}

func (r linuxRun) policy(mode sandbox.Mode, network bool) sandbox.Policy {
	return sandbox.Policy{Mode: mode, Workspace: r.ws, Network: network}
}

// run runs script with sh inside the sandbox and returns its exit code and
// output, removing the mount points bwrap left behind.
func run(t *testing.T, p sandbox.Policy, stdout *os.File, name string, args ...string) (code int, output string) {
	t.Helper()
	// The mount points bwrap will create, listed before it creates them.
	targets := sandbox.BwrapMountTargets(p)
	t.Cleanup(func() {
		for _, target := range targets {
			_ = os.Remove(target)
		}
	})
	argv, err := p.Wrap(append([]string{name}, args...))
	require.NoError(t, err)
	cmd := exec.Command(argv[0], argv[1:]...)
	var buf strings.Builder
	cmd.Stderr = &buf
	cmd.Stdout = &buf
	if stdout != nil {
		cmd.Stdout = stdout
	}
	cmd.Dir = p.Workspace
	err = cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), buf.String()
	}
	require.NoError(t, err)

	return 0, buf.String()
}

func sh(t *testing.T, p sandbox.Policy, script string) (code int, output string) {
	t.Helper()

	return run(t, p, nil, "/bin/sh", "-c", script)
}

func TestLinuxWorkspaceWrite(t *testing.T) {
	r := newLinuxRun(t)
	p := r.policy(sandbox.WorkspaceWrite, false)

	code, out := sh(t, p, "echo hi > a && mkdir -p sub && echo hi > sub/b && echo hi > \"$TMPDIR/c\" && echo hi > /dev/null")
	require.Equal(t, 0, code, out)
	assert.FileExists(t, filepath.Join(r.ws, "a"))
	assert.FileExists(t, filepath.Join(r.ws, "sub", "b"))

	code, out = sh(t, p, "echo hi > "+filepath.Join(r.outside, "x"))
	assert.NotEqual(t, 0, code)
	assert.Contains(t, out, "Read-only file system")
	assert.True(t, sandbox.Denied(code, out), out)
	assert.NoFileExists(t, filepath.Join(r.outside, "x"))

	code, out = sh(t, p, "echo '[x]' >> .git/config")
	assert.NotEqual(t, 0, code)
	assert.True(t, sandbox.Denied(code, out), out)
	data, err := os.ReadFile(filepath.Join(r.ws, ".git", "config"))
	require.NoError(t, err)
	assert.Equal(t, "[core]\n", string(data))

	code, out = sh(t, p, "mkdir .uagent/rules")
	assert.NotEqual(t, 0, code)
	assert.True(t, sandbox.Denied(code, out), out)
}

func TestLinuxReadOnly(t *testing.T) {
	r := newLinuxRun(t)
	p := r.policy(sandbox.ReadOnly, false)

	code, out := sh(t, p, "cat .git/config")
	require.Equal(t, 0, code, out)
	assert.Equal(t, "[core]\n", out)

	code, out = sh(t, p, "echo hi > a")
	assert.NotEqual(t, 0, code)
	assert.True(t, sandbox.Denied(code, out), out)
	assert.NoFileExists(t, filepath.Join(r.ws, "a"))
}

func TestLinuxNetwork(t *testing.T) {
	r := newLinuxRun(t)
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}

	// Connect by address, so the test does not depend on DNS.
	code, out := run(t, r.policy(sandbox.WorkspaceWrite, false), nil, bash, "-c", "exec 3<>/dev/tcp/1.1.1.1/443")
	assert.NotEqual(t, 0, code)
	assert.Contains(t, strings.ToLower(out), "network is unreachable")
	assert.True(t, sandbox.Denied(code, out), out)
}

func TestLinuxInheritedOutput(t *testing.T) {
	r := newLinuxRun(t)
	// A file outside every writable root, opened by the parent, as the
	// runner opens its output files.
	f, err := os.Create(filepath.Join(r.outside, "out.log"))
	require.NoError(t, err)
	defer f.Close()

	code, out := run(t, r.policy(sandbox.WorkspaceWrite, false), f, "/bin/sh", "-c", "echo through the fd")
	require.Equal(t, 0, code, out)
	data, err := os.ReadFile(f.Name())
	require.NoError(t, err)
	assert.Equal(t, "through the fd\n", string(data))
}

func TestLinuxExitCode(t *testing.T) {
	r := newLinuxRun(t)
	code, out := sh(t, r.policy(sandbox.WorkspaceWrite, false), "echo failing >&2; exit 7")
	assert.Equal(t, 7, code)
	assert.Equal(t, "failing\n", out)
	assert.False(t, sandbox.Denied(code, out))
}
