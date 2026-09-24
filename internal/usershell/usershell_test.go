package usershell_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/rules"
	"github.com/viktordanov/uagent-harness/internal/sandbox"
	"github.com/viktordanov/uagent-harness/internal/usershell"
)

// TestRecordText pins Codex's format, from its test in
// codex-rs/core/src/user_shell_command_tests.rs, and that Parse reads it.
func TestRecordText(t *testing.T) {
	r := usershell.Record{Command: "echo hi", ExitCode: 0, Duration: time.Second, Output: "hi"}

	assert.Equal(t, "<user_shell_command>\n<command>\necho hi\n</command>\n<result>\nExit code: 0\nDuration: 1.0000 seconds\nOutput:\nhi\n</result>\n</user_shell_command>", r.Text())

	for _, want := range []usershell.Record{
		r,
		{Command: "go test ./...\n# two lines", ExitCode: 1, Duration: 2500 * time.Millisecond, Output: "FAIL\n</command>\n"},
		{Command: "true", ExitCode: -1, Output: ""},
	} {
		got, ok := usershell.Parse(want.Text())
		require.True(t, ok, want.Command)
		assert.Equal(t, want, got)
	}
	_, ok := usershell.Parse("hello")
	assert.False(t, ok, "an ordinary message")
}

func runner(t *testing.T) (*usershell.Runner, string) {
	t.Helper()
	ws, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	return &usershell.Runner{Dir: t.TempDir(), Policy: sandbox.Policy{Workspace: ws}, Shell: "/bin/sh"}, ws
}

func TestRun(t *testing.T) {
	r, ws := runner(t)

	t.Run("it runs in the workspace, with stderr in the output", func(t *testing.T) {
		res := r.Run(t.Context(), usershell.Request{Command: "pwd; echo oops >&2", Mode: approval.ModeWorkspace})

		assert.Equal(t, 0, res.ExitCode)
		assert.Equal(t, ws+"\noops\n", res.Output)
		assert.Equal(t, sandbox.FullAccess, res.Sandbox, "outside the sandbox, as Codex runs it")
	})

	t.Run("a failing command keeps its exit code and output", func(t *testing.T) {
		res := r.Run(t.Context(), usershell.Request{Command: "echo no; exit 3"})

		assert.Equal(t, 3, res.ExitCode)
		assert.Equal(t, "no\n", res.Output)
	})

	t.Run("output streams as it arrives", func(t *testing.T) {
		var chunks []string
		res := r.Run(t.Context(), usershell.Request{Command: "echo a; echo b", Stream: func(c string) { chunks = append(chunks, c) }})

		assert.Equal(t, res.Output, strings.Join(chunks, ""))
	})

	t.Run("long output keeps its head and tail", func(t *testing.T) {
		res := r.Run(t.Context(), usershell.Request{Command: "echo first; yes 0123456789 | head -n 100000; echo last"})

		assert.True(t, strings.HasPrefix(res.Output, "first\n"))
		assert.True(t, strings.HasSuffix(res.Output, "last\n"))
		// 6 + 1,100,000 + 5 bytes, less the first and last 20,000.
		assert.Contains(t, res.Output, "...1060011 bytes truncated...", "the count covers what the capture dropped too")
		assert.Len(t, res.Output, usershell.OutputLimit+len("...1060011 bytes truncated..."))
	})

	t.Run("a timeout stops it", func(t *testing.T) {
		slow := *r
		slow.Timeout = 100 * time.Millisecond
		res := slow.Run(t.Context(), usershell.Request{Command: "echo started; sleep 10"})

		assert.True(t, res.TimedOut)
		assert.Equal(t, usershell.ExitNotRun, res.ExitCode)
		assert.Equal(t, "command timed out after 100 milliseconds\nstarted\n", res.Output)
		assert.Less(t, res.Duration, 5*time.Second)
	})

	t.Run("canceling stops it", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		res := r.Run(ctx, usershell.Request{Command: "sleep 10"})

		assert.True(t, res.Canceled)
		assert.False(t, res.TimedOut, "the caller stopped it")
		assert.Equal(t, "command aborted by user\n", res.Output)
		assert.Equal(t, usershell.ExitNotRun, res.ExitCode)
	})
}

// TestRun_Sandboxed runs commands like the agent's (user_shell_sandbox):
// a write outside the workspace fails in workspace mode, and a forbid rule
// refuses a command before it runs. By default neither applies.
func TestRun_Sandboxed(t *testing.T) {
	r, ws := runner(t)
	requireSandbox(t, ws)
	outside := outsideDir(t)
	forbid, err := rules.FromPrefixes([]string{"rm"}, rules.Forbidden, "[approvals] forbid")
	require.NoError(t, err)
	r.Approver = approval.New(approval.Config{Rules: forbid})
	target := filepath.Join(outside, "out.txt")
	write := "echo x > " + sandbox.Quote(target)

	t.Run("a write outside the workspace fails in workspace mode", func(t *testing.T) {
		boxed := *r
		boxed.Sandboxed = true
		res := boxed.Run(t.Context(), usershell.Request{Command: write, Mode: approval.ModeWorkspace})

		assert.NotEqual(t, 0, res.ExitCode, res.Output)
		assert.Equal(t, sandbox.WorkspaceWrite, res.Sandbox)
		assert.NoFileExists(t, target)

		res = boxed.Run(t.Context(), usershell.Request{Command: "echo x > inside.txt", Mode: approval.ModeWorkspace})
		assert.Equal(t, 0, res.ExitCode, res.Output)
		assert.FileExists(t, filepath.Join(ws, "inside.txt"))
	})

	t.Run("a forbid rule refuses the command", func(t *testing.T) {
		keep := filepath.Join(ws, "keep.txt")
		require.NoError(t, os.WriteFile(keep, []byte("x"), 0o600))
		boxed := *r
		boxed.Sandboxed = true
		res := boxed.Run(t.Context(), usershell.Request{Command: "rm keep.txt", Mode: approval.ModeFullAccess})

		assert.Equal(t, "not run: a rule forbids this command.", res.Refused)
		assert.Equal(t, res.Refused, res.Output, "the agent hears why")
		assert.Equal(t, usershell.ExitNotRun, res.ExitCode)
		assert.FileExists(t, keep)
	})

	t.Run("by default the user's command runs as their own", func(t *testing.T) {
		res := r.Run(t.Context(), usershell.Request{Command: write + " && rm " + sandbox.Quote(target), Mode: approval.ModeReadOnly})

		assert.Equal(t, 0, res.ExitCode, res.Output)
		assert.Equal(t, sandbox.FullAccess, res.Sandbox)
	})
}

// requireSandbox skips the test when this system cannot sandbox commands
// (Linux without a working bwrap); CI sets UAH_REQUIRE_BWRAP to fail.
func requireSandbox(t *testing.T, ws string) {
	t.Helper()
	skip := t.Skipf
	if os.Getenv("UAH_REQUIRE_BWRAP") != "" {
		skip = t.Fatalf
	}
	argv, err := sandbox.Policy{Mode: sandbox.WorkspaceWrite, Workspace: ws}.Wrap([]string{"/bin/sh", "-c", "true"})
	if err != nil {
		skip("no sandbox: %v", err)

		return
	}
	if out, err := exec.CommandContext(t.Context(), argv[0], argv[1:]...).CombinedOutput(); err != nil {
		skip("the sandbox does not run here: %v: %s", err, out)
	}
}

// outsideDir is a directory no policy makes writable: under the user cache
// directory, since t.TempDir is under $TMPDIR, which is writable.
func outsideDir(t *testing.T) string {
	t.Helper()
	cache, err := os.UserCacheDir()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(cache, 0o700))
	dir, err := os.MkdirTemp(cache, "uah-usershell-test-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	dir, err = filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	return dir
}
