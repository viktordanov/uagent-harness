package app

import (
	"errors"
	"fmt"

	"github.com/viktordanov/uah/internal/approval"
	"github.com/viktordanov/uah/internal/config"
	"github.com/viktordanov/uah/internal/sandbox"
	"github.com/viktordanov/uah/internal/session"
)

// pickMode is the permission mode and the sandbox policy it runs commands
// under: yolo with --yolo, else the --sandbox flag's mode, the resumed
// session's mode, the configured permission_mode, the configured
// sandbox_mode's mode, or workspace (workspace-write, Codex's default for
// trusted projects). Only --yolo gives yolo: a resumed session that was in
// yolo mode opens in the next mode down this list without it.
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
	case in.Yolo && (in.Sandbox != "" || in.Ask != ""):
		return "", errors.New("--yolo runs without a sandbox and approvals, so it takes no --sandbox or --ask")
	case in.Yolo:
		return approval.ModeYolo, nil
	case in.Sandbox != "":
		return sandboxMode(in.Sandbox)
	case resumed.Mode != "" && !resumed.Mode.AsksNoOne():
		return approval.ParseMode(string(resumed.Mode)) // ParseMode names the value
	case cfg.PermissionMode != "":
		return permissionMode(cfg.PermissionMode)
	}

	return sandboxMode(first(cfg.SandboxMode, string(sandbox.WorkspaceWrite)))
}

// permissionMode reads permission_mode: read-only, workspace, or auto.
// Yolo is not a value for a file; only --yolo gives it.
func permissionMode(name string) (approval.Mode, error) {
	if m, err := approval.ParseMode(name); err == nil && !m.AsksNoOne() {
		return m, nil
	}

	return "", fmt.Errorf("invalid permission mode %q (want read-only, workspace, or auto)", name)
}

// sandboxMode is the mode of a sandbox mode name: read-only or
// workspace-write. No sandbox is yolo mode, which only --yolo gives.
func sandboxMode(name string) (approval.Mode, error) {
	if m, err := sandbox.ParseMode(name); err == nil && m != sandbox.FullAccess {
		return approval.ModeFor(m), nil
	}

	return "", fmt.Errorf("invalid sandbox mode %q (want read-only or workspace-write)", name)
}

// pickFast is the --fast flag when given, else the resumed session's fast
// mode, else the configured one. The session's does not carry over to
// another provider, which may not serve it.
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
	return resumed.Fast != nil && !providerChanged(in, resumed, cfg)
}
