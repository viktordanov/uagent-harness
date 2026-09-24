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
// each command runs through the sandboxing shell, through the real shell
// when a rule or the user allows it outside the sandbox, or not at all. The
// runner's translator reads only its own arguments, so the escalation
// arguments pass through it untouched.
type sandboxedBash struct {
	// Translator runs commands in the sandbox; it is the unsandboxed one
	// when there is no sandbox.
	tool.Translator

	unsandboxed tool.Translator
	// sandboxShell is the sandboxing shell, empty without a sandbox.
	sandboxShell string
	mode         sandbox.Mode
	approver     *approval.Approver
	ask          approval.Ask
	// ctx is the run's context, which bounds a wait for the user.
	ctx  context.Context
	cwd  string
	warn io.Writer
}

// sandboxedBash builds the Bash translator for the configured sandbox. The
// unsandboxed shell still applies the environment policy. Without a
// sandbox on this system, every command no rule allows asks first.
func (w *wiring) sandboxedBash(req core.Request, opsDir, realShell string) (tool.Translator, error) {
	p := w.policy(req, w.e.cfg.Sandbox.Mode)
	newBash := func(shell string) tool.Translator {
		return bash.New(bash.Config{Shell: shell, Directory: req.Workspace, BaseDirectory: opsDir})
	}
	full := p
	full.Mode = sandbox.FullAccess
	plain, err := sandbox.Shell(w.e.cfg.SandboxDir, full, w.e.cfg.Env, realShell)
	if err != nil {
		return nil, err
	}
	b := sandboxedBash{
		unsandboxed: newBash(plain), mode: p.Mode, approver: w.e.cfg.Approver, ask: w.ask,
		ctx: context.Background(), cwd: req.Workspace, warn: w.l.Stderr,
	}
	b.Translator = b.unsandboxed
	if p.Mode == sandbox.FullAccess {
		return b, nil
	}
	shell, err := sandbox.Shell(w.e.cfg.SandboxDir, p, w.e.cfg.Env, realShell)
	switch {
	case errors.Is(err, sandbox.ErrUnavailable):
		_, _ = fmt.Fprintf(w.l.Stderr, "embedded: %v; each command asks for approval unless a rule allows it\n", err)
	case err != nil:
		return nil, err
	default:
		b.Translator, b.sandboxShell = newBash(shell), shell
	}

	return b, nil
}

// canEscalate reports whether the model can ask to leave a sandbox.
func (b sandboxedBash) canEscalate() bool { return b.sandboxShell != "" }

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
	d := b.approver.Decide(b.ctx, approval.Request{
		Command: args.Command, Cwd: b.cwd, Justification: args.Justification, PrefixRule: args.PrefixRule,
		Escalated: args.Permissions == permEscalated && b.canEscalate(),
		NoSandbox: !b.canEscalate() && b.mode != sandbox.FullAccess,
	}, b.ask)
	if d.Run != approval.Deny && d.Reason != "" {
		_, _ = fmt.Fprintf(b.warn, "embedded: %s\n", d.Reason)
	}
	switch d.Run {
	case approval.Unsandboxed:
		return b.unsandboxed.Translate(ctx, call)
	case approval.Sandboxed:
		return b.Translator.Translate(ctx, call)
	case approval.Deny:
	}

	return tool.CallStatus{Error: d.Reason}
}

// TranslateResult adds a hint when the sandbox likely blocked the command.
func (b sandboxedBash) TranslateResult(callID string, status tool.CallStatus, ops []operation.Operation) (llm.ToolResult, error) {
	result, err := b.Translator.TranslateResult(callID, status, ops)
	if err != nil || len(ops) != 1 || b.sandboxShell == "" {
		return result, err //nolint:wrapcheck // the coordinator wraps tool errors
	}
	state, derr := operation.DecodeShellState(ops[0])
	if derr != nil || state.Input.Shell != b.sandboxShell || state.Result == nil ||
		!sandbox.Denied(state.Result.ExitCode, state.Result.Out+"\n"+state.Result.Err) {
		return result, nil
	}
	result.Output = append(result.Output, llm.ToolResultOutput{Kind: llm.ToolResultText, Value: fmt.Sprintf(
		"\nuah: the %s sandbox likely blocked this. If the command needs more access, run it again with %s %q and a %s.",
		b.mode, argSandboxPermissions, permEscalated, argJustification)})

	return result, nil
}
