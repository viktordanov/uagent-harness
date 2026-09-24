package embedded

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"fmt"
	"slices"

	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/operation"
	"github.com/unreallabsai/unreal-agent/harness/tool"

	"github.com/viktordanov/uagent-harness/internal/engine"
)

// The remote job plan a subagent tool call runs as.
const (
	agentPlanType    operation.RemoteJobPlanType    = "uah.agent"
	agentPlanVersion operation.RemoteJobPlanVersion = 1
)

// agentPlan is the remote job's plan: which tool, with what.
type agentPlan struct {
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
}

// agentRegistry offers the subagent tools of the run, and resolves every
// name the subagents accept, so a session with past calls resumes where
// the tools are not offered.
type agentRegistry struct {
	tool.Registry

	offered []engine.AgentTool
	names   []string
}

func withAgents(r tool.Registry, offered []engine.AgentTool, names, disallowed []string) tool.Registry {
	offered = slices.DeleteFunc(slices.Clone(offered), func(t engine.AgentTool) bool { return slices.Contains(disallowed, t.Name) })

	return agentRegistry{Registry: r, offered: offered, names: names}
}

func (r agentRegistry) StaticDefinitions() []tool.Definition {
	defs := r.Registry.StaticDefinitions()
	for _, t := range r.offered {
		defs = append(defs, tool.Definition{Tool: llm.Tool{Type: llm.ToolFunction, Name: t.Name, Description: t.Description, Parameters: t.Parameters}})
	}

	return defs
}

func (r agentRegistry) Resolve(name string) (tool.Translator, bool) {
	if t, ok := r.Registry.Resolve(name); ok || !slices.Contains(r.names, name) {
		return t, ok
	}
	offered := slices.ContainsFunc(r.offered, func(t engine.AgentTool) bool { return t.Name == name })

	return agentTranslator{name: name, offered: offered}, true
}

// agentTranslator submits a subagent tool call as a remote job and reads
// its result.
type agentTranslator struct {
	name    string
	offered bool
}

func (t agentTranslator) Translate(ctx tool.Context, call llm.ToolCall) tool.CallStatus {
	if !t.offered {
		return tool.ErrorStatus(fmt.Sprintf("tool %q is not available in this session", t.name), 0)
	}
	args := bytes.TrimSpace([]byte(call.Arguments))
	if len(args) == 0 {
		args = []byte("{}")
	}
	if !json.Valid(args) || args[0] != '{' {
		return tool.ErrorStatus("the arguments must be a JSON object", 0)
	}
	data, err := json.Marshal(agentPlan{Tool: t.name, Arguments: args})
	if err != nil {
		return tool.ErrorStatus(fmt.Sprintf("failed to encode the agent call: %v", err), 0)
	}
	spec, err := operation.NewRemoteJobSpec(operation.RemoteJobPlan{Type: agentPlanType, Version: agentPlanVersion, Data: jsontext.Value(data)})
	if err != nil {
		return tool.ErrorStatus(fmt.Sprintf("failed to build the agent call: %v", err), 0)
	}

	return tool.CallStatus{WaitingFor: []operation.ID{ctx.Submit(spec)}}
}

func (t agentTranslator) TranslateResult(callID string, status tool.CallStatus, ops []operation.Operation) (llm.ToolResult, error) {
	result := llm.ToolResult{CallID: callID}
	text := func(s string) {
		result.Output = append(result.Output, llm.ToolResultOutput{Kind: llm.ToolResultText, Value: s})
	}
	if status.Error != "" {
		text("Error: " + status.Error)

		return result, nil
	}
	if len(ops) != 1 {
		return result, fmt.Errorf("agent call %q has %d operations, want 1", callID, len(ops))
	}
	state, err := operation.DecodeRemoteJobState(ops[0])
	if err != nil {
		return result, fmt.Errorf("failed to decode agent call %q: %w", callID, err)
	}
	switch ops[0].Status {
	case operation.StatusReady, operation.StatusAwaiting, operation.StatusCanceling:
		text("The agent call is still running.")
	case operation.StatusFailed:
		text("Error: " + state.TerminalError)
	case operation.StatusCanceled:
		text("Error: the agent call was canceled.")
	case operation.StatusCompleted:
		text(state.TerminalResult)
	}

	return result, nil
}
