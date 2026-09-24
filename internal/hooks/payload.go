package hooks

import "encoding/json"

// Input is the JSON a hook reads on stdin. Field names follow Claude Code's
// hook contract, so existing hook scripts work unchanged.
type Input struct {
	Event          Event  `json:"hook_event_name"`
	SessionID      string `json:"session_id"`
	RunID          string `json:"run_id,omitempty"`
	Cwd            string `json:"cwd"`
	Model          string `json:"model,omitempty"`
	Effort         string `json:"effort,omitempty"`
	TranscriptPath string `json:"transcript_path,omitempty"`

	// UserPromptSubmit.
	Prompt string `json:"prompt,omitempty"`

	// PreToolUse and PostToolUse.
	ToolName     string          `json:"tool_name,omitempty"`
	ToolInput    json.RawMessage `json:"tool_input,omitempty"`
	ToolUseID    string          `json:"tool_use_id,omitempty"`
	ToolResponse *ToolResponse   `json:"tool_response,omitempty"`

	// Stop: true when the run was started by a Stop hook, so a hook can avoid looping.
	StopHookActive bool `json:"stop_hook_active,omitempty"`

	// SessionStart ("startup" or "resume") and SessionEnd ("exit").
	Source string `json:"source,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// ToolResponse is what PostToolUse hooks see of a finished tool.
type ToolResponse struct {
	Success bool   `json:"success"`
	Detail  string `json:"detail,omitempty"`
	Stdout  string `json:"stdout_path,omitempty"`
	Stderr  string `json:"stderr_path,omitempty"`
}

// Output is the optional JSON a hook prints on stdout with exit code 0.
type Output struct {
	// Continue false stops what the event is about: the prompt, the tool call,
	// or (for Stop) any continuation.
	Continue      *bool  `json:"continue,omitempty"`
	StopReason    string `json:"stopReason,omitempty"`
	SystemMessage string `json:"systemMessage,omitempty"`
	// Decision "block" with Reason blocks UserPromptSubmit, or keeps the agent
	// going after Stop with Reason as the next message.
	Decision string `json:"decision,omitempty"`
	Reason   string `json:"reason,omitempty"`

	HookSpecificOutput *SpecificOutput `json:"hookSpecificOutput,omitempty"`
}

// SpecificOutput carries event-specific fields.
type SpecificOutput struct {
	HookEventName Event `json:"hookEventName,omitempty"`
	// PreToolUse: "allow", "deny", or "ask" ("ask" is treated as deny: uah has
	// no approval prompt yet).
	PermissionDecision       string          `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string          `json:"permissionDecisionReason,omitempty"`
	UpdatedInput             json.RawMessage `json:"updatedInput,omitempty"`
	// UserPromptSubmit and SessionStart: text added to the prompt.
	AdditionalContext string `json:"additionalContext,omitempty"`
}
