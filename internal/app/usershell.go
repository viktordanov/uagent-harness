package app

import (
	"path/filepath"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/config"
	"github.com/viktordanov/uagent-harness/internal/usershell"
)

// userShell runs the commands the user types in the TUI's shell mode, in
// uah itself, so both engines have them: with the sandbox scripts, the
// environment policy, and the shell the agent's commands use, and with the
// sandbox and the rules only when user_shell_sandbox asks for them.
func userShell(r Resolved, cfg config.Config, stateDir string, approver *approval.Approver) *usershell.Runner {
	return &usershell.Runner{
		Dir: filepath.Join(stateDir, "sandbox"), Policy: r.Sandbox, Env: r.Env, Shell: RealShell(),
		Sandboxed: cfg.UserShellSandbox, Approver: approver,
	}
}
