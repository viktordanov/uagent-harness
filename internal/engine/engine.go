// Package engine abstracts how the harness drives unreal-agent-runner. The
// process engine spawns the runner through uagent; the embedded engine runs
// the runner's packages in process, so messages and settings reach a live run.
package engine

import (
	"context"
	"errors"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/contextusage"
	"github.com/viktordanov/uagent-harness/internal/mcp"
)

// ErrUnsupported means the engine cannot do this while a run is live.
var ErrUnsupported = errors.New("not supported by this engine while a run is live")

// Capabilities says what an engine can change while a run is live.
type Capabilities struct {
	LiveInput   bool
	LiveEffort  bool
	LiveModel   bool
	ServiceTier bool
	// Compaction means the engine can compact the context (Run.Compact and
	// Options.Compact).
	Compaction bool
	// LiveMode means a permission mode change reaches a live run
	// (Run.SetMode); otherwise it applies from the next run.
	LiveMode bool
}

// Engine starts runs.
type Engine interface {
	Name() string
	Capabilities() Capabilities
	// Start begins a run. The sink receives RunStarted first and RunFinished
	// last once Start succeeds, from one goroutine at a time.
	Start(ctx context.Context, req core.Request, opts Options, sink core.Sink) (Run, error)
}

// MCPLister is an engine that runs MCP servers (the embedded engine). An
// engine that also holds them between runs implements io.Closer, and the
// session closes it.
type MCPLister interface {
	// MCPServers reports each configured server, starting them if needed.
	MCPServers() []mcp.ServerStatus
}

// ContextReporter is an engine that can break down the context of its last
// model request, for /context.
type ContextReporter interface {
	ContextUsage(sessionID string) (contextusage.Usage, bool)
}

// Options are run settings that core.Request does not carry.
type Options struct {
	// ServiceTier is "" or "priority" (needs Capabilities.ServiceTier).
	ServiceTier string
	// Mode is the permission mode: the sandbox commands run in and who
	// decides what needs approval ("": the engine's configured sandbox).
	Mode approval.Mode
	// Compact compacts the context before the run's first model request
	// (needs Capabilities.Compaction).
	Compact bool
	// Clear drops the context before the run's first model request, as
	// /clear does (needs Capabilities.Compaction).
	Clear bool
	// Ask asks the user to approve a command; nil means no one can, as in
	// a headless run. Only the embedded engine asks.
	Ask approval.Ask
	// AskAnytime asks the user like Ask, also after the run ends, for work
	// that outlives the run, such as a subagent's approvals. Such a prompt
	// stays open until it is answered, its context ends, or the session
	// closes (nil: no one can).
	AskAnytime approval.Ask
	// Notify adds an engine event to the session's stream, also after the
	// run ends, such as a subagent's progress (nil: the run's stream).
	Notify func(core.Event)
	// Inject gives the agent a message without a turn of its own: it goes
	// with the next message (Session.Inject). A subagent's notification to
	// its parent goes this way (nil: dropped).
	Inject func(text string)
}

// Forgetter is an engine that keeps per-session state across runs; the
// session calls Forget when it closes, so the state does not outlive it.
type Forgetter interface {
	Forget(sessionID string)
}

// Run is a started run.
type Run interface {
	// Send delivers a message to the live run (ErrUnsupported without LiveInput).
	Send(input core.UserInput) error
	// SetEffort changes the effort for the next model request (ErrUnsupported without LiveEffort).
	SetEffort(effort string) error
	// SetModel changes the model for the next model request (ErrUnsupported without LiveModel).
	SetModel(model string) error
	// SetServiceTier changes the tier for the next model request (ErrUnsupported without ServiceTier).
	SetServiceTier(tier string) error
	// SetMode changes the permission mode from the next command and the
	// next model request (ErrUnsupported without LiveMode).
	SetMode(mode approval.Mode) error
	// Compact compacts the context before the next model request
	// (ErrUnsupported without Compaction).
	Compact() error
	// Clear drops the context before the next model request: the model
	// starts fresh in the same session (ErrUnsupported without Compaction).
	Clear() error
	// Interrupt stops the run gracefully.
	Interrupt()
	// Kill stops the run at once.
	Kill()
	// Wait blocks until the run ends.
	Wait() (core.Result, error)
}
