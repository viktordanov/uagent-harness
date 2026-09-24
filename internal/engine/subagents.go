package engine

import (
	"context"
	"time"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/approval"
)

// Subagents runs the child agents a session's agent spawns (see
// docs/design/subagents.md). The embedded engine offers the agent tools
// when its configuration has one; internal/agents implements it.
type Subagents interface {
	// Attach records the parent's current run: its settings, how its
	// children ask for approval, and where their progress goes. It reports
	// whether the session may spawn children at all (its depth).
	Attach(parent AgentParent) bool
	// Roles are the agent types spawn_agent offers.
	Roles() []AgentRole
	// Spawn starts a child with its first message and returns at once.
	Spawn(ctx context.Context, parentID string, req SpawnRequest) (AgentRef, error)
	// Send gives a running or finished child another message.
	Send(parentID, id, message string) error
	// Wait returns when any of the children reaches a final status, with
	// the final ones' statuses, or when the timeout passes (timedOut).
	Wait(ctx context.Context, parentID string, ids []string, timeout time.Duration) (statuses map[string]AgentStatus, timedOut bool, err error)
	// CloseAgent stops a child and returns its status before it stopped.
	CloseAgent(parentID, id string) (AgentStatus, error)
}

// AgentParent is a parent session's live run.
type AgentParent struct {
	SessionID string
	// Request is the parent run's request: the children's default
	// provider, model, effort, workspace, and host prompt.
	Request core.Request
	// Ask asks the parent's user (nil: no one can, so children are denied).
	Ask approval.Ask
	// Emit adds an event to the parent run's stream, such as AgentUpdated.
	Emit func(core.Event)
}

// AgentRole is an agent type from a role file.
type AgentRole struct {
	Name        string
	Description string
}

// SpawnRequest is a spawn_agent call. Empty fields take the role's, the
// configured, or the parent's values.
type SpawnRequest struct {
	Message   string
	AgentType string
	Model     string
	Effort    string
}

// AgentRef names a spawned child.
type AgentRef struct {
	ID       string `json:"id"`
	Nickname string `json:"nickname"`
}

// Agent states, as Codex reports them.
const (
	AgentRunning   = "running"
	AgentCompleted = "completed"
	AgentErrored   = "errored"
	AgentShutdown  = "shutdown"
	AgentNotFound  = "not_found"
)

// AgentStatus is a child's state; Message is a completed child's final
// answer or an errored child's error.
type AgentStatus struct {
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
}

// Final reports whether the child has stopped working.
func (s AgentStatus) Final() bool { return s.State != AgentRunning }

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

func (e AgentUpdated) OccurredAt() time.Time { return e.At }
