package contextprep

import (
	"context"
	"strings"
)

// SandboxNotes, the "sandbox" block, says what sandboxed commands may
// write, where the session's private $TMPDIR is and what it is for, and
// what the sandbox blocks that a model does not expect: local sockets and
// process listings. The Bash tool's description already says that a command
// needing the network runs with require_escalated from the first try; this
// block does not repeat it.
type SandboxNotes struct{}

// Name implements Adapter.
func (SandboxNotes) Name() string { return "sandbox" }

// Prepare implements Adapter. It says nothing without a sandbox.
func (SandboxNotes) Prepare(_ context.Context, f Facts) string {
	s, mac := f.Sandbox, f.GOOS == "darwin"
	var lines []string
	switch s.Mode {
	case "read-only":
		writes := "write nothing"
		if s.TempDir != "" {
			writes = "write only $TMPDIR"
		}
		lines = append(lines, "Sandbox: read-only. Commands can read any file and "+writes+".")
	case "workspace-write":
		lines = append(lines, "Sandbox: workspace-write. Commands can read any file and write the workspace, /tmp, and $TMPDIR; "+
			".git, .uah, .agents, and .codex stay read-only.")
	default:
		return ""
	}
	if s.TempDir != "" {
		lines = append(lines, "$TMPDIR ("+s.TempDir+") is this session's private scratch directory, writable in every mode: "+
			"put temporary files there, and point a tool cache that cannot be written elsewhere at it, such as GOCACHE=$TMPDIR/go-build.")
		if s.Mode == "read-only" && mac && f.Shell == "/bin/bash" {
			lines = append(lines, "macOS's /bin/bash ignores $TMPDIR for heredoc files, so heredocs fail here; "+
				"write the text with printf to a file in $TMPDIR instead.")
		}
	}
	if mac {
		lines = append(lines, sandboxBlocksOnMac(s.Network)...)
	}

	return strings.Join(lines, "\n")
}

// sandboxBlocksOnMac names what Seatbelt blocks beyond file writes and the
// network the Bash tool's description covers. Linux has no such list:
// bwrap leaves Unix sockets on disk reachable without network, and what ps
// sees depends on whether bwrap could mount /proc.
func sandboxBlocksOnMac(network bool) []string {
	lines := []string{"ps and pgrep fail in the sandbox (lsof works); request escalation to list processes."}
	if !network {
		lines = append(lines, "Local sockets count as network: docker and other clients of a daemon's socket fail in the sandbox, "+
			"so request escalation for them from the first try.")
	}

	return lines
}
