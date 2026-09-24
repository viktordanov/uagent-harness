package engine

import (
	"context"
	"encoding/json"
	"time"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/approval"
)

// Subagents runs the child agents a session's agent starts; internal/agents
// implements it (see its README). The embedded engine offers the tools
// Attach returns and runs each call in the background as a remote job; it
// knows nothing of what the tools do.
type Subagents interface {
	// Attach records the parent's current run and returns the tools to
	// offer it: none when the session may not start children.
	Attach(parent AgentParent) []AgentTool
	// ToolNames are every tool name Call accepts, offered or not, so a
	// session with past calls resumes where the tools are not offered.
	ToolNames() []string
	// Call runs one of the parent's tool calls and returns the result for
	// the model. It blocks as long as the tool needs (a wait); ctx ends
	// when the call is canceled or the run stops.
	Call(ctx context.Context, parentID, tool string, args json.RawMessage) (string, error)
	// Interrupt stops the live runs of the parent's children and their
	// own children, because the user interrupted the parent.
	Interrupt(parentID string)
}

// AgentParent is a parent session's live run.
type AgentParent struct {
	SessionID string
	// Request and ServiceTier are the parent run's: its children start
	// with the same settings.
	Request     core.Request
	ServiceTier string
	// Ask asks the parent's user, also after the parent's run ends (nil:
	// no one can, so children are declined).
	Ask approval.Ask
	// Emit adds an event to the parent session's stream, also after the
	// run ends, such as AgentUpdated.
	Emit func(core.Event)
}

// AgentTool is a tool a run is offered, with its JSON Schema parameters.
type AgentTool struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// Agent states, as Codex reports them.
const (
	AgentPendingInit = "pending_init"
	AgentRunning     = "running"
	AgentInterrupted = "interrupted"
	AgentCompleted   = "completed"
	AgentErrored     = "errored"
	AgentShutdown    = "shutdown"
	AgentNotFound    = "not_found"
)

// AgentUpdated reports a child's progress in its parent's stream.
type AgentUpdated struct {
	At       time.Time
	ID       string
	Nickname string
	Role     string
	State    string
	// Started is when the child's current work began.
	Started time.Time
}

// AgentActivity is one of a child's tool events (core.ToolCalled,
// core.ToolStarted, or core.ToolFinished) in its parent's stream, for the
// detailed view.
type AgentActivity struct {
	At    time.Time
	ID    string
	Event core.Event
}

func (e AgentUpdated) OccurredAt() time.Time  { return e.At }
func (e AgentActivity) OccurredAt() time.Time { return e.At }
