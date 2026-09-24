package sandbox_test

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/sandbox"
)

var update = flag.Bool("update", false, "rewrite golden files")

func TestSeatbeltProfileGolden(t *testing.T) {
	t.Setenv("TMPDIR", "/fixed/tmpdir")
	// /tmp is /private/tmp on macOS; the golden files use /tmp everywhere.
	tmp, err := filepath.EvalSymlinks("/tmp")
	require.NoError(t, err)

	cases := []struct {
		name   string
		policy sandbox.Policy
	}{
		{"read-only", sandbox.Policy{Mode: sandbox.ReadOnly, Workspace: "/work/project"}},
		{"workspace-write", sandbox.Policy{
			Mode: sandbox.WorkspaceWrite, Workspace: "/work/project", WritableRoots: []string{"/work/cache"},
		}},
		{"workspace-write-network", sandbox.Policy{Mode: sandbox.WorkspaceWrite, Workspace: "/work/project", Network: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			profile, params := sandbox.SeatbeltProfile(tc.policy)
			got := profile + "\n; parameters:\n"
			for _, p := range params {
				got += "; " + p + "\n"
			}
			if tmp != "/tmp" {
				got = strings.ReplaceAll(got, "="+tmp, "=/tmp")
			}
			path := filepath.Join("testdata", "seatbelt-"+tc.name+".sbpl")
			if *update {
				require.NoError(t, os.WriteFile(path, []byte(got), 0o600))
			}
			want, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, string(want), got)
		})
	}
}

func TestSeatbeltProfileGitdir(t *testing.T) {
	ws, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	gitdir := filepath.Join(filepath.Dir(ws), "main", ".git", "worktrees", "x")
	require.NoError(t, os.WriteFile(filepath.Join(ws, ".git"), []byte("gitdir: "+gitdir+"\n"), 0o600))

	_, params := sandbox.SeatbeltProfile(sandbox.Policy{Mode: sandbox.WorkspaceWrite, Workspace: ws})
	assert.Contains(t, params, "-DWRITABLE_ROOT_0="+ws)
	assert.Contains(t, params, "-DWRITABLE_ROOT_0_EXCLUDED_0="+filepath.Join(ws, ".git"))
	// The gitdir target is outside the workspace but inside $TMPDIR, so the
	// $TMPDIR root excludes it.
	excluded := false
	for _, p := range params {
		excluded = excluded || strings.HasSuffix(p, "="+gitdir) && strings.Contains(p, "_EXCLUDED_")
	}
	assert.True(t, excluded, "%q", params)
}

func TestSeatbeltProfileReadOnlyWritesNothing(t *testing.T) {
	profile, params := sandbox.SeatbeltProfile(sandbox.Policy{Mode: sandbox.ReadOnly, Workspace: "/work/project"})
	assert.Empty(t, params)
	assert.NotContains(t, profile, "WRITABLE_ROOT")
	assert.NotContains(t, profile, "(allow network-outbound)")
}
