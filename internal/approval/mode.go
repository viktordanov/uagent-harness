package approval

import (
	"fmt"
	"slices"

	"github.com/viktordanov/uagent-harness/internal/sandbox"
)

// Mode is a permission mode: a sandbox mode and who decides what needs
// approval. shift+tab cycles ReadOnly, Workspace, and Auto, as Codex's
// permission shortcut cycles its read-only and auto presets
// (codex-rs/tui/src/chatwidget/permission_shortcuts.rs), and Yolo after
// them only in a session started with --yolo.
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
	// ModeYolo runs commands without a sandbox, and whatever would need
	// approval runs without asking anyone, the auto-reviewer included;
	// only forbid rules still refuse. Codex's
	// --dangerously-bypass-approvals-and-sandbox (--yolo): sandbox
	// danger-full-access with approval_policy never. Only --yolo starts it.
	ModeYolo Mode = "yolo"
)

// Modes are the modes in cycle order, Yolo last.
var Modes = []Mode{ModeReadOnly, ModeWorkspace, ModeAuto, ModeYolo}

// cycle is what shift+tab steps through without --yolo.
var cycle = []Mode{ModeReadOnly, ModeWorkspace, ModeAuto}

// ParseMode reads a mode name; empty is ModeWorkspace.
func ParseMode(s string) (Mode, error) {
	if s == "" {
		return ModeWorkspace, nil
	}
	if m := Mode(s); slices.Contains(Modes, m) {
		return m, nil
	}

	return "", fmt.Errorf("invalid permission mode %q (want read-only, workspace, auto, or yolo)", s)
}

// ModeFor is the mode of a sandbox mode without auto-approval; no sandbox
// is Yolo.
func ModeFor(s sandbox.Mode) Mode {
	switch s {
	case sandbox.ReadOnly:
		return ModeReadOnly
	case sandbox.FullAccess:
		return ModeYolo
	case sandbox.WorkspaceWrite:
	}

	return ModeWorkspace
}

// Next is the next mode in the cycle, which has Yolo after Auto when yolo
// is offered (--yolo); otherwise Yolo moves to ReadOnly.
func (m Mode) Next(yolo bool) Mode {
	modes := cycle
	if yolo {
		modes = Modes
	}
	i := slices.Index(modes, m.orDefault())
	if i < 0 {
		return modes[0]
	}

	return modes[(i+1)%len(modes)]
}

// Sandbox is the sandbox mode commands run in.
func (m Mode) Sandbox() sandbox.Mode {
	switch m.orDefault() {
	case ModeReadOnly:
		return sandbox.ReadOnly
	case ModeYolo:
		return sandbox.FullAccess
	case ModeWorkspace, ModeAuto:
	}

	return sandbox.WorkspaceWrite
}

// ReviewerDecides reports whether the auto-reviewer decides alone: the
// user is never asked, and a decline goes to the model with the
// reviewer's reason.
func (m Mode) ReviewerDecides() bool { return m == ModeAuto }

// AsksNoOne reports whether whatever needs approval runs without asking
// the user or the auto-reviewer (Yolo).
func (m Mode) AsksNoOne() bool { return m == ModeYolo }

// Label is the mode's name as the TUI shows it.
func (m Mode) Label() string {
	switch m.orDefault() {
	case ModeReadOnly:
		return "read only"
	case ModeAuto:
		return "auto"
	case ModeYolo:
		return "yolo"
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
