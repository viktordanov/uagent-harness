package app

import (
	"errors"
	"fmt"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/config"
	"github.com/viktordanov/uagent-harness/internal/sandbox"
	"github.com/viktordanov/uagent-harness/internal/session"
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
		return approval.ParseMode(string(resumed.Mode)) //nolint:wrapcheck // ParseMode names the value
	case cfg.PermissionMode != "":
		m, err := approval.ParseMode(cfg.PermissionMode)
		if err == nil && m.AsksNoOne() {
			err = errYoloFlagOnly
		}

		return m, err //nolint:wrapcheck // ParseMode names the value
	}

	return sandboxMode(first(cfg.SandboxMode, string(sandbox.WorkspaceWrite)))
}

// errYoloFlagOnly refuses yolo mode from a file: it needs --yolo each time.
var errYoloFlagOnly = errors.New("yolo mode (no sandbox, no approvals) needs --yolo each time; no file sets it")

// sandboxMode is the mode of a sandbox mode name; danger-full-access, no
// sandbox, is yolo mode, which only --yolo gives.
func sandboxMode(name string) (approval.Mode, error) {
	m, err := sandbox.ParseMode(name)
	if err != nil {
		return "", fmt.Errorf("invalid sandbox mode %q (want read-only or workspace-write; no sandbox is --yolo)", name)
	}
	if m == sandbox.FullAccess {
		return "", errYoloFlagOnly
	}

	return approval.ModeFor(m), nil
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
