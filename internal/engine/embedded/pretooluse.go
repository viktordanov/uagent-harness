package embedded

import (
	"context"
	"encoding/json"
	"path/filepath"

	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/tool"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/hooks"
)

// hookedRegistry runs PreToolUse hooks before each tool call is translated.
// A block becomes the tool's error result, and updatedInput replaces the
// arguments. Hooks run on the coordinator's goroutine, as Claude Code's do,
// so a slow hook delays the agent up to its timeout.
type hookedRegistry struct {
	tool.Registry

	ctx   context.Context
	hooks *hooks.Runner
	base  hooks.Input
}

func withPreToolUse(ctx context.Context, r tool.Registry, runner *hooks.Runner, req core.Request, sessionsDir string) tool.Registry {
	if !runner.Has(hooks.PreToolUse, "") {
		return r
	}

	return hookedRegistry{Registry: r, ctx: ctx, hooks: runner, base: hooks.Input{
		Event: hooks.PreToolUse, SessionID: req.SessionID, RunID: req.RunID, Cwd: req.Workspace,
		Model: req.Model, Effort: req.Effort, TranscriptPath: filepath.Join(sessionsDir, req.SessionID+".session.jsonl"),
	}}
}

func (r hookedRegistry) Resolve(name string) (tool.Translator, bool) {
	t, ok := r.Registry.Resolve(name)
	if !ok || !r.hooks.Has(hooks.PreToolUse, name) {
		return t, ok
	}

	return hookedTranslator{Translator: t, name: name, r: r}, true
}

type hookedTranslator struct {
	tool.Translator

	name string
	r    hookedRegistry
}

// hookShaper is a tool whose hook tool_input differs from its arguments,
// such as apply_patch's {"command": patch}.
type hookShaper interface {
	hookInput(arguments string) json.RawMessage
	fromHookInput(updated json.RawMessage) (string, error)
}

func (t hookedTranslator) Translate(ctx tool.Context, call llm.ToolCall) tool.CallStatus {
	in := t.r.base
	in.ToolName, in.ToolUseID = t.name, call.CallID
	shaper, shaped := t.Translator.(hookShaper)
	switch {
	case shaped:
		in.ToolInput = shaper.hookInput(call.Arguments)
	case json.Valid([]byte(call.Arguments)):
		in.ToolInput = json.RawMessage(call.Arguments)
	}
	d := t.r.hooks.Run(t.r.ctx, in)
	if d.Block {
		return tool.CallStatus{Error: "blocked by a PreToolUse hook: " + d.Reason}
	}
	switch {
	case len(d.UpdatedInput) == 0:
	case shaped:
		args, err := shaper.fromHookInput(d.UpdatedInput)
		if err != nil {
			return tool.CallStatus{Error: "a PreToolUse hook's updatedInput is invalid: " + err.Error()}
		}
		call.Arguments = args
	default:
		call.Arguments = string(d.UpdatedInput)
	}

	return t.Translator.Translate(ctx, call)
}
