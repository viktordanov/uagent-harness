package embedded

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	"fmt"
	"slices"
	"strings"

	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/operation"
	"github.com/unreallabsai/unreal-agent/harness/tool"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/mcp"
)

// The remote job plan an MCP call runs as (see docs/design/mcp.md).
const (
	mcpPlanType    operation.RemoteJobPlanType    = "uah.mcp_call"
	mcpPlanVersion operation.RemoteJobPlanVersion = 1
)

// mcpPlan is the remote job's plan: which tool to call, with what.
type mcpPlan struct {
	Server    string          `json:"server"`
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
}

// mcpOutputs is what a completed call keeps in the job's handle, which the
// runner stores untruncated: its images, as data: URLs.
type mcpOutputs struct {
	Images []string `json:"images,omitempty"`
}

// mcpRegistry offers the MCP tools besides the runner's and resolves every
// mcp__ name, so a session with past MCP calls resumes even when the server
// is gone: the stored results need no server.
type mcpRegistry struct {
	tool.Registry

	tools []mcp.Tool
	gate  mcpGate
}

// mcpGate asks the user about calls whose approval_mode needs it, through
// the same prompt as sandbox escalations.
type mcpGate struct {
	ctx   context.Context
	ask   approval.Ask // nil: no one can answer (headless)
	never bool         // approval_policy "never"
}

// withMCP adds the tools the request does not disallow.
func withMCP(r tool.Registry, tools []mcp.Tool, disallowed []string, gate mcpGate) tool.Registry {
	tools = slices.DeleteFunc(slices.Clone(tools), func(t mcp.Tool) bool { return slices.Contains(disallowed, t.Name) })

	return mcpRegistry{Registry: r, tools: tools, gate: gate}
}

func (r mcpRegistry) StaticDefinitions() []tool.Definition {
	defs := r.Registry.StaticDefinitions()
	for _, t := range r.tools {
		description := t.Description
		if description == "" {
			description = fmt.Sprintf("The %s tool of the %s MCP server.", t.Tool, t.Server)
		}
		defs = append(defs, tool.Definition{Tool: llm.Tool{Type: llm.ToolFunction, Name: t.Name, Description: description, Parameters: t.InputSchema}})
	}

	return defs
}

func (r mcpRegistry) Resolve(name string) (tool.Translator, bool) {
	if t, ok := r.Registry.Resolve(name); ok || !strings.HasPrefix(name, mcp.Prefix) {
		return t, ok
	}
	i := slices.IndexFunc(r.tools, func(t mcp.Tool) bool { return t.Name == name })
	if i < 0 {
		return mcpTranslator{name: name}, true
	}

	return mcpTranslator{name: name, tool: &r.tools[i], gate: r.gate}, true
}

// mcpTranslator submits an MCP call as a remote job and reads its result.
type mcpTranslator struct {
	name string
	tool *mcp.Tool // nil when no running server offers the name
	gate mcpGate
}

func (t mcpTranslator) Translate(ctx tool.Context, call llm.ToolCall) tool.CallStatus {
	if t.tool == nil {
		return tool.ErrorStatus(fmt.Sprintf("tool %q is not available: no running MCP server offers it", t.name), 0)
	}
	args := bytes.TrimSpace([]byte(call.Arguments))
	if reason := t.gate.check(*t.tool, string(args)); reason != "" {
		return tool.ErrorStatus(reason, 0)
	}
	if len(args) == 0 {
		args = []byte("{}")
	}
	if !json.Valid(args) || args[0] != '{' {
		return tool.ErrorStatus("the arguments must be a JSON object", 0)
	}
	data, err := json.Marshal(mcpPlan{Server: t.tool.Server, Tool: t.tool.Tool, Arguments: args})
	if err != nil {
		return tool.ErrorStatus(fmt.Sprintf("failed to encode the MCP call: %v", err), 0)
	}
	spec, err := operation.NewRemoteJobSpec(operation.RemoteJobPlan{Type: mcpPlanType, Version: mcpPlanVersion, Data: jsontext.Value(data)})
	if err != nil {
		return tool.ErrorStatus(fmt.Sprintf("failed to build the MCP call: %v", err), 0)
	}

	return tool.CallStatus{WaitingFor: []operation.ID{ctx.Submit(spec)}}
}

// needsApproval applies approval_mode as Codex does: prompt always asks,
// writes asks unless the tool is read-only, auto follows the tool's
// annotations, and approve never asks.
func needsApproval(t mcp.Tool) bool {
	switch t.Approval {
	case mcp.ApprovalApprove:
		return false
	case mcp.ApprovalAuto:
		return t.AutoAsks
	case mcp.ApprovalWrites:
		return !t.ReadOnly
	case mcp.ApprovalPrompt:
	}

	return true
}

// check returns why the call may not run, or "". It blocks while the user
// decides, as a sandbox escalation does.
func (g mcpGate) check(t mcp.Tool, args string) string {
	if !needsApproval(t) {
		return ""
	}
	why := fmt.Sprintf("the MCP tool %s needs the user's approval (approval_mode %q)", t.Name, t.Approval)
	if g.never {
		return "not run: " + why + ", and approval_policy is never. Tell the user what you wanted to do with it."
	}
	if g.ask == nil || g.ctx == nil {
		return "not run: " + why + ", and no one can approve it in this run. Tell the user what you wanted to do with it."
	}
	answer := g.ask(g.ctx, approval.Prompt{Command: t.Name + " " + args, Justification: t.Description})
	if reason, ok := answer.DeclineReason(); ok {
		return "not run: " + reason
	}
	if !answer.Approved() {
		return "not run: the user declined the MCP tool " + t.Name + ". Ask the user how to proceed."
	}

	return ""
}

func (t mcpTranslator) TranslateResult(callID string, status tool.CallStatus, ops []operation.Operation) (llm.ToolResult, error) {
	result := llm.ToolResult{CallID: callID}
	text := func(s string) {
		result.Output = append(result.Output, llm.ToolResultOutput{Kind: llm.ToolResultText, Value: s})
	}
	if status.Error != "" {
		text("Error: " + status.Error)

		return result, nil
	}
	if len(ops) != 1 {
		return result, fmt.Errorf("MCP call %q has %d operations, want 1", callID, len(ops))
	}
	state, err := operation.DecodeRemoteJobState(ops[0])
	if err != nil {
		return result, fmt.Errorf("failed to decode MCP call %q: %w", callID, err)
	}
	switch ops[0].Status {
	case operation.StatusReady, operation.StatusAwaiting, operation.StatusCanceling:
		text("The MCP call is still running.")
	case operation.StatusFailed:
		text("Error: " + state.TerminalError)
	case operation.StatusCanceled:
		text("Error: the MCP call was canceled.")
	case operation.StatusCompleted:
		var outputs mcpOutputs
		if len(state.Handle) > 0 {
			if err := json.Unmarshal(state.Handle, &outputs); err != nil {
				return result, fmt.Errorf("failed to decode MCP call %q images: %w", callID, err)
			}
		}
		if state.TerminalResult != "" || len(outputs.Images) == 0 {
			text(state.TerminalResult)
		}
		for _, img := range outputs.Images {
			result.Output = append(result.Output, llm.ToolResultOutput{Kind: llm.ToolResultImage, Value: img})
		}
	}

	return result, nil
}
