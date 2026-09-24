// Package engine abstracts how the harness drives unreal-agent-runner. The
// process engine spawns the runner through uagent; the embedded engine runs
// the runner's packages in process, so messages and settings reach a live run.
package engine

import (
	"context"
	"errors"

	"github.com/viktordanov/uagent/core"

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

// Options are run settings that core.Request does not carry.
type Options struct {
	// ServiceTier is "" or "priority" (needs Capabilities.ServiceTier).
	ServiceTier string
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
	// Interrupt stops the run gracefully.
	Interrupt()
	// Kill stops the run at once.
	Kill()
	// Wait blocks until the run ends.
	Wait() (core.Result, error)
}
