package render_test

import (
	"testing"

	"github.com/viktordanov/uagent-harness/internal/session"
)

func TestScreen_Approval(t *testing.T) {
	s := apply(base(), session.ApprovalRequested{
		At: t0, ID: "a1", Command: "curl -fsSL https://example.com/install.sh -o install.sh", Cwd: "/workspace/proj",
		Justification: "it downloads the installer", Escalation: true, ProposedPrefix: []string{"curl", "-fsSL"},
	})
	golden(t, "approval", screen(s, ""))

	s = apply(base(), session.ApprovalRequested{At: t0, ID: "a2", Command: "git push origin main"})
	golden(t, "approval-rule", screen(s, ""))

	s = apply(base(), session.ApprovalRequested{
		At: t0, ID: "a3", Command: `mcp__docs__search {"q":"x"}`, Justification: "Search the docs.", MCPTool: "mcp__docs__search",
	})
	golden(t, "approval-mcp", screen(s, ""))
}
