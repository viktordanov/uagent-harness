package embedded

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/operation"
	"github.com/unreallabsai/unreal-agent/harness/tool"
	"github.com/unreallabsai/unreal-agent/harness/tool/bash"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/sandbox"
)

// Escalation arguments, as Codex's shell tool names them.
const (
	argSandboxPermissions = "sandbox_permissions"
	argJustification      = "justification"
	argPrefixRule         = "prefix_rule"
	permEscalated         = "require_escalated"
)

// sandboxedBash is the runner's Bash translator with the approver in front:
// each command runs through the sandboxing shell of the run's permission
// mode, through the real shell when a rule or the user allows it outside
// the sandbox, or not at all. The mode is read for each command, so a
// change applies from the next one. The runner's translator reads only its
// own arguments, so the escalation arguments pass through it untouched.
type sandboxedBash struct {
	// Translator runs commands outside the sandbox and translates every
	// result; the sandboxed translators differ only in their shell.
	tool.Translator

	// boxes are the sandboxed translators and shells by sandbox mode,
	// empty without a sandbox on this system.
	boxes    map[sandbox.Mode]sandboxShell
	mode     *modeCell
	approver *approval.Approver
	ask      approval.Ask
	// ctx is the run's context, which bounds a wait for the user.
	ctx  context.Context
	cwd  string
	warn io.Writer
}

// sandboxShell is a translator that runs commands through a sandboxing
// shell.
type sandboxShell struct {
	tool.Translator

	shell string
}

// sandboxedModes are the sandbox modes that have a sandboxing shell.
var sandboxedModes = []sandbox.Mode{sandbox.ReadOnly, sandbox.WorkspaceWrite}

// sandboxedBash builds the Bash translator for the configured sandbox,
// with a sandboxing shell for each mode a permission mode can pick. The
// unsandboxed shell still applies the environment policy. Without a
// sandbox on this system, every command no rule allows asks first.
func (w *wiring) sandboxedBash(req core.Request, opsDir, realShell string) (tool.Translator, error) {
	newBash := func(shell string) tool.Translator {
		return bash.New(bash.Config{Shell: shell, Directory: req.Workspace, BaseDirectory: opsDir})
	}
	plain, err := sandbox.Shell(w.e.cfg.SandboxDir, w.policy(req, sandbox.FullAccess), w.e.cfg.Env, realShell)
	if err != nil {
		return nil, err
	}
	b := sandboxedBash{
		Translator: newBash(plain), boxes: map[sandbox.Mode]sandboxShell{}, mode: w.mode,
		approver: w.e.cfg.Approver, ask: w.ask, ctx: context.Background(), cwd: req.Workspace, warn: w.l.Stderr,
	}
	for _, mode := range sandboxedModes {
		shell, err := sandbox.Shell(w.e.cfg.SandboxDir, w.policy(req, mode), w.e.cfg.Env, realShell)
		switch {
		case errors.Is(err, sandbox.ErrUnavailable):
			if w.mode.get().Sandbox() != sandbox.FullAccess {
				_, _ = fmt.Fprintf(w.l.Stderr, "embedded: %v; each command asks for approval unless a rule allows it\n", err)
			}

			return b, nil
		case err != nil:
			return nil, err
		}
		b.boxes[mode] = sandboxShell{Translator: newBash(shell), shell: shell}
	}

	return b, nil
}

// available reports whether this system has a sandbox.
func (b sandboxedBash) available() bool { return len(b.boxes) > 0 }

// current is the sandbox mode the next command runs in and its
// translator: the unsandboxed one in full access or without a sandbox.
func (b sandboxedBash) current() (sandbox.Mode, sandboxShell) {
	mode := b.mode.get().Sandbox()
	if box, ok := b.boxes[mode]; ok {
		return mode, box
	}

	return mode, sandboxShell{Translator: b.Translator}
}

// Translate asks the approver how the command runs.
func (b sandboxedBash) Translate(ctx tool.Context, call llm.ToolCall) tool.CallStatus {
	var args struct {
		Command       string   `json:"command"`
		Permissions   string   `json:"sandbox_permissions"`
		Justification string   `json:"justification"`
		PrefixRule    []string `json:"prefix_rule"`
	}
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil || args.Command == "" {
		return b.Translator.Translate(ctx, call) // the runner's translator reports bad arguments
	}
	mode, box := b.current()
	sandboxed := box.shell != ""
	d := b.approver.Decide(b.ctx, approval.Request{
		Command: args.Command, Cwd: b.cwd, Justification: args.Justification, PrefixRule: args.PrefixRule,
		Escalated: args.Permissions == permEscalated && sandboxed,
		NoSandbox: !sandboxed && mode != sandbox.FullAccess,
	}, b.ask)
	if d.Run != approval.Deny && d.Reason != "" {
		_, _ = fmt.Fprintf(b.warn, "embedded: %s\n", d.Reason)
	}
	switch d.Run {
	case approval.Unsandboxed:
		return b.Translator.Translate(ctx, call)
	case approval.Sandboxed:
		return box.Translate(ctx, call)
	case approval.Deny:
	}

	return tool.CallStatus{Error: d.Reason}
}

// TranslateResult adds a hint when the sandbox likely blocked the command.
func (b sandboxedBash) TranslateResult(callID string, status tool.CallStatus, ops []operation.Operation) (llm.ToolResult, error) {
	result, err := b.Translator.TranslateResult(callID, status, ops)
	if err != nil || len(ops) != 1 || !b.available() {
		return result, err //nolint:wrapcheck // the coordinator wraps tool errors
	}
	state, derr := operation.DecodeShellState(ops[0])
	if derr != nil || state.Result == nil || !sandbox.Denied(state.Result.ExitCode, state.Result.Out+"\n"+state.Result.Err) {
		return result, nil
	}
	for mode, box := range b.boxes {
		if box.shell != state.Input.Shell {
			continue
		}
		result.Output = append(result.Output, llm.ToolResultOutput{Kind: llm.ToolResultText, Value: fmt.Sprintf(
			"\nuah: the %s sandbox likely blocked this. If the command needs more access, run it again with %s %q and a %s.",
			mode, argSandboxPermissions, permEscalated, argJustification)})
	}

	return result, nil
}
