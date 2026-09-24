package embedded

import (
	"encoding/json"
	"fmt"
	"maps"

	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/operation"
	"github.com/unreallabsai/unreal-agent/harness/tool"

	"github.com/viktordanov/uagent-harness/internal/sandbox"
)

// Escalation arguments, as Codex's shell tool names them.
const (
	argSandboxPermissions = "sandbox_permissions"
	argJustification      = "justification"
	permEscalated         = "require_escalated"
)

// sandboxedBash is the runner's Bash translator running commands through a
// sandboxing shell. The runner's translator reads only its own arguments,
// so the escalation arguments pass through it untouched.
type sandboxedBash struct {
	tool.Translator

	mode sandbox.Mode
}

// Translate refuses escalations: this version of uah cannot ask for
// approval yet, so the model hears why and can work within the sandbox.
func (b sandboxedBash) Translate(ctx tool.Context, call llm.ToolCall) tool.CallStatus {
	var args struct {
		Permissions   string `json:"sandbox_permissions"`
		Justification string `json:"justification"`
	}
	_ = json.Unmarshal([]byte(call.Arguments), &args) // the runner's translator reports malformed arguments
	if args.Permissions == permEscalated {
		return tool.CallStatus{Error: fmt.Sprintf(
			"not run: running outside the %s sandbox needs the user's approval, which this version of uah cannot ask for. "+
				"Work within the sandbox, or tell the user the command and why it needs more access (%q).",
			b.mode, args.Justification)}
	}

	return b.Translator.Translate(ctx, call)
}

// TranslateResult adds a hint when the sandbox likely blocked the command.
func (b sandboxedBash) TranslateResult(callID string, status tool.CallStatus, ops []operation.Operation) (llm.ToolResult, error) {
	result, err := b.Translator.TranslateResult(callID, status, ops)
	if err != nil || len(ops) != 1 {
		return result, err //nolint:wrapcheck // the coordinator wraps tool errors
	}
	state, derr := operation.DecodeShellState(ops[0])
	if derr != nil || state.Result == nil || !sandbox.Denied(state.Result.ExitCode, state.Result.Out+"\n"+state.Result.Err) {
		return result, nil
	}
	result.Output = append(result.Output, llm.ToolResultOutput{Kind: llm.ToolResultText, Value: fmt.Sprintf(
		"\nuah: the %s sandbox likely blocked this. If the command needs more access, run it again with %s %q and a %s.",
		b.mode, argSandboxPermissions, permEscalated, argJustification)})

	return result, nil
}

// sandboxRegistry offers the model Bash with the escalation arguments and a
// description of the sandbox.
type sandboxRegistry struct {
	tool.Registry

	policy sandbox.Policy
}

func (r sandboxRegistry) StaticDefinitions() []tool.Definition {
	defs := r.Registry.StaticDefinitions()
	for i, d := range defs {
		if d.Tool.Name == tool.BashName {
			defs[i].Tool = bashWithEscalation(d.Tool, r.policy)
		}
	}

	return defs
}

func bashWithEscalation(t llm.Tool, p sandbox.Policy) llm.Tool {
	params := maps.Clone(t.Parameters)
	props, _ := params["properties"].(map[string]any)
	props = maps.Clone(props)
	props[argSandboxPermissions] = map[string]any{
		"type": "string",
		"enum": []any{"use_default", permEscalated},
		"description": "use_default runs the command in the sandbox. require_escalated asks the user to run it outside the sandbox; " +
			"use it only when the command needs access the sandbox blocks, and explain why in justification.",
	}
	props[argJustification] = map[string]any{
		"type":        "string",
		"description": "With require_escalated: one sentence the user reads to approve the command, such as why it needs the network.",
	}
	params["properties"] = props
	t.Parameters = params
	t.Description += " " + sandboxNote(p)

	return t
}

// sandboxNote tells the model what its commands may do.
func sandboxNote(p sandbox.Policy) string {
	network := "no network access"
	if p.Network {
		network = "network access"
	}
	switch p.Mode {
	case sandbox.ReadOnly:
		return "Commands run in a read-only sandbox: they can read files but write nothing, with " + network + "."
	case sandbox.WorkspaceWrite:
		return "Commands run in a sandbox: they can read any file, write only the workspace and temporary directories " +
			"(.git, .uagent, .agents, and .codex stay read-only), and have " + network + "."
	case sandbox.FullAccess:
	}

	return ""
}
