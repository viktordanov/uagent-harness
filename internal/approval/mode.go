package approval

import (
	"fmt"
	"slices"

	"github.com/viktordanov/uagent-harness/internal/sandbox"
)

// Mode is a permission mode: a sandbox mode and who decides what needs
// approval. shift+tab cycles ReadOnly, Workspace, and Auto, as Codex's
// permission shortcut cycles its read-only and auto presets
// (codex-rs/tui/src/chatwidget/permission_shortcuts.rs); FullAccess is set
// only by --sandbox or the configuration, as Codex keeps Full Access out
// of that cycle.
type Mode string

const (
	// ModeReadOnly runs commands in the read-only sandbox; escalations and
	// prompt rules ask (the auto-reviewer first). Codex's "read-only".
	ModeReadOnly Mode = "read-only"
	// ModeWorkspace runs commands in the workspace-write sandbox;
	// escalations and prompt rules ask (the auto-reviewer first). Codex's
	// "auto" preset ("Default") with approvals_reviewer = user or
	// auto_review. It is the default.
	ModeWorkspace Mode = "workspace"
	// ModeAuto runs commands in the workspace-write sandbox, and the
	// auto-reviewer decides escalations and prompt rules without asking
	// the user. Codex's "Approve for me"; Claude Code's auto mode.
	ModeAuto Mode = "auto"
	// ModeFullAccess runs commands without a sandbox. Codex's
	// "full-access".
	ModeFullAccess Mode = "full-access"
)

// Modes are the names permission_mode accepts, in cycle order, with
// FullAccess last.
var Modes = []Mode{ModeReadOnly, ModeWorkspace, ModeAuto, ModeFullAccess}

// cycle is what shift+tab steps through.
var cycle = []Mode{ModeReadOnly, ModeWorkspace, ModeAuto}

// ParseMode reads a mode name; empty is ModeWorkspace.
func ParseMode(s string) (Mode, error) {
	if s == "" {
		return ModeWorkspace, nil
	}
	if m := Mode(s); slices.Contains(Modes, m) {
		return m, nil
	}

	return "", fmt.Errorf("invalid permission mode %q (want read-only, workspace, auto, or full-access)", s)
}

// ModeFor is the mode of a sandbox mode without auto-approval.
func ModeFor(s sandbox.Mode) Mode {
	switch s {
	case sandbox.ReadOnly:
		return ModeReadOnly
	case sandbox.FullAccess:
		return ModeFullAccess
	case sandbox.WorkspaceWrite:
	}

	return ModeWorkspace
}

// Next is the next mode in the cycle; FullAccess moves to ReadOnly.
func (m Mode) Next() Mode {
	i := slices.Index(cycle, m.orDefault())
	if i < 0 {
		return cycle[0]
	}

	return cycle[(i+1)%len(cycle)]
}

// Sandbox is the sandbox mode commands run in.
func (m Mode) Sandbox() sandbox.Mode {
	switch m.orDefault() {
	case ModeReadOnly:
		return sandbox.ReadOnly
	case ModeFullAccess:
		return sandbox.FullAccess
	case ModeWorkspace, ModeAuto:
	}

	return sandbox.WorkspaceWrite
}

// ReviewerDecides reports whether the auto-reviewer decides alone: the
// user is never asked, and a decline goes to the model with the
// reviewer's reason.
func (m Mode) ReviewerDecides() bool { return m == ModeAuto }

// Label is the mode's name as the TUI shows it.
func (m Mode) Label() string {
	switch m.orDefault() {
	case ModeReadOnly:
		return "read only"
	case ModeAuto:
		return "auto"
	case ModeFullAccess:
		return "full access"
	case ModeWorkspace:
	}

	return "workspace"
}

func (m Mode) orDefault() Mode {
	if m == "" {
		return ModeWorkspace
	}

	return m
}
