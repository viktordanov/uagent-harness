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
	Call(ctx context.Context, call AgentCall) (string, error)
	// Interrupt stops the live runs of the parent's children and their
	// own children, because the user interrupted the parent.
	Interrupt(parentID string)
}

// AgentCall is one tool call of a parent session.
type AgentCall struct {
	ParentID string
	// CallID is the model's ID for the call; spawn_agent's fork_context
	// finds the request that made it by this ID. Empty for calls from before
	// uah recorded it.
	CallID string
	Tool   string
	Args   json.RawMessage
}

// Forker is an engine whose sessions can start from a copy of another
// session's history, and share its prompt cache. The embedded engine
// implements it; internal/agents uses it for spawn_agent's fork_context.
type Forker interface {
	// Fork creates the session childID from the parent's history as it was
	// when the model made the call callID: every item before that model
	// request, and the compaction that applied to it. The child's first
	// model request then starts with the parent's.
	Fork(ctx context.Context, parentID, childID, callID string) error
	// SetCacheKey makes the session's model requests use key as their
	// prompt cache key (the provider's cache affinity) instead of the
	// session's ID. Codex keys every agent of a tree by the root session.
	SetCacheKey(sessionID, key string)
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
	// Inject gives the parent's agent a message without a turn of its own,
	// as a child's <subagent_notification> (nil: none).
	Inject func(text string)
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

// AgentUpdated reports a child's progress in its parent's stream. Every
// update carries the whole picture, so the latest one is enough to draw
// the child.
type AgentUpdated struct {
	At       time.Time
	ID       string
	Nickname string
	Role     string
	State    string
	// Message says why an errored child failed, in one line: the
	// provider's message when it gave one.
	Message string
	// Started is when the child's current work began.
	Started time.Time
	// CallID is the parent's spawn_agent call that started the child
	// ("" for a resumed child), Task that call's message, and Model and
	// Effort the child's settings as it runs.
	CallID string
	Task   string
	Model  string
	Effort string
	// Forked is a child started with fork_context.
	Forked bool
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
