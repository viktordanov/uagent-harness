package contextprep_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uah/internal/contextprep"
)

func TestSandboxNotesName(t *testing.T) {
	var a contextprep.Adapter = contextprep.SandboxNotes{}
	assert.Equal(t, "sandbox", a.Name())
}

const sandboxTemp = "/state/sessions/operations/s1/tmp"

func TestSandboxNotesPrepare(t *testing.T) {
	scratch := "$TMPDIR (" + sandboxTemp + ") is this session's private scratch directory, writable in every mode: " +
		"put temporary files there, and point a tool cache that cannot be written elsewhere at it, such as GOCACHE=$TMPDIR/go-build."
	ps := "ps and pgrep fail in the sandbox (lsof works); request escalation to list processes."
	sockets := "Local sockets count as network: docker and other clients of a daemon's socket fail in the sandbox, " +
		"so request escalation for them from the first try."
	for _, tc := range []struct {
		name string
		f    contextprep.Facts
		want string
	}{
		{
			name: "no sandbox",
			f:    contextprep.Facts{GOOS: "darwin", Sandbox: contextprep.Sandbox{TempDir: sandboxTemp}},
		},
		{
			name: "read-only on macOS",
			f:    contextprep.Facts{GOOS: "darwin", Shell: "/bin/zsh", Sandbox: contextprep.Sandbox{Mode: "read-only", TempDir: sandboxTemp}},
			want: "Sandbox: read-only. Commands can read any file and write only $TMPDIR.\n" + scratch + "\n" + ps + "\n" + sockets,
		},
		{
			name: "workspace-write with network on macOS",
			f: contextprep.Facts{GOOS: "darwin", Shell: "/bin/zsh", Sandbox: contextprep.Sandbox{
				Mode: "workspace-write", Network: true, TempDir: sandboxTemp,
			}},
			want: "Sandbox: workspace-write. Commands can read any file and write the workspace, /tmp, and $TMPDIR; " +
				".git, .uah, .agents, and .codex stay read-only.\n" + scratch + "\n" + ps,
		},
		{
			name: "workspace-write on Linux",
			f:    contextprep.Facts{GOOS: "linux", Shell: "/bin/bash", Sandbox: contextprep.Sandbox{Mode: "workspace-write", TempDir: sandboxTemp}},
			want: "Sandbox: workspace-write. Commands can read any file and write the workspace, /tmp, and $TMPDIR; " +
				".git, .uah, .agents, and .codex stay read-only.\n" + scratch,
		},
		{
			// A sandboxed session always has one; without, the $TMPDIR line has no value.
			name: "read-only without a temp dir",
			f:    contextprep.Facts{GOOS: "linux", Sandbox: contextprep.Sandbox{Mode: "read-only"}},
			want: "Sandbox: read-only. Commands can read any file and write only $TMPDIR.",
		},
		{
			name: "read-only with macOS's bash",
			f:    contextprep.Facts{GOOS: "darwin", Shell: "/bin/bash", Sandbox: contextprep.Sandbox{Mode: "read-only", Network: true, TempDir: sandboxTemp}},
			want: "Sandbox: read-only. Commands can read any file and write only $TMPDIR.\n" + scratch + "\n" +
				"macOS's /bin/bash ignores $TMPDIR for heredoc files, so heredocs fail here; " +
				"write the text with printf to a file in $TMPDIR instead.\n" + ps,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, contextprep.SandboxNotes{}.Prepare(context.Background(), tc.f))
		})
	}
}
