package app

import (
	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/config"
	"github.com/viktordanov/uagent-harness/internal/sandbox"
	"github.com/viktordanov/uagent-harness/internal/session"
)

// pickMode is the permission mode and the sandbox policy it runs commands
// under: the --sandbox flag's mode, the resumed session's mode, the
// configured permission_mode, the configured sandbox_mode's mode, or
// workspace (workspace-write, Codex's default for trusted projects).
func pickMode(in Inputs, resumed session.Info, cfg config.Config, workspace string) (approval.Mode, sandbox.Policy, error) {
	mode, err := modeOf(in, resumed, cfg)
	if err != nil {
		return "", sandbox.Policy{}, usage(err)
	}

	return mode, sandbox.Policy{
		Mode: mode.Sandbox(), Workspace: workspace,
		WritableRoots: cfg.SandboxWorkspaceWrite.WritableRoots, Network: cfg.SandboxWorkspaceWrite.NetworkAccess,
	}, nil
}

func modeOf(in Inputs, resumed session.Info, cfg config.Config) (approval.Mode, error) {
	switch {
	case in.Sandbox != "":
		return sandboxMode(in.Sandbox)
	case resumed.Mode != "":
		return approval.ParseMode(string(resumed.Mode)) //nolint:wrapcheck // ParseMode names the value
	case cfg.PermissionMode != "":
		return approval.ParseMode(cfg.PermissionMode) //nolint:wrapcheck // ParseMode names the value
	}

	return sandboxMode(first(cfg.SandboxMode, string(sandbox.WorkspaceWrite)))
}

func sandboxMode(name string) (approval.Mode, error) {
	m, err := sandbox.ParseMode(name)
	if err != nil {
		return "", err //nolint:wrapcheck // ParseMode names the value
	}

	return approval.ModeFor(m), nil
}

// pickFast is the --fast flag when given, else the resumed session's fast
// mode, else the configured one. The session's does not carry over to
// another provider or to the process engine, which cannot serve it.
func pickFast(in Inputs, resumed session.Info, cfg config.Config) bool {
	switch {
	case in.FastSet:
		return in.Fast
	case sessionFast(in, resumed, cfg):
		return *resumed.Fast
	}

	return in.Fast || cfg.Fast
}

// sessionFast reports whether the resumed session's fast mode applies.
func sessionFast(in Inputs, resumed session.Info, cfg config.Config) bool {
	return resumed.Fast != nil && !providerChanged(in, resumed, cfg) && first(in.Engine, cfg.Engine, EngineEmbedded) == EngineEmbedded
}
