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

// DefaultMaxAttempts is how many times a model request is sent before
// the run fails, unless the request sets its own: the first attempt and
// nine retries, which the runner's client spaces 2 s apart, doubling to
// at most 30 s (about 3 minutes in all). The runner's own default is 5.
const DefaultMaxAttempts = 10

// ErrUnsupported means the engine cannot do this while a run is live.
var ErrUnsupported = errors.New("not supported by this engine while a run is live")

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

// Rewinder is an engine that can cut a session's context at an earlier
// message, as Codex's backtrack (Capabilities.Rewind).
type Rewinder interface {
	// Rewind makes the session's next model request end just before the
	// message messageID, and records the cut next to the session, which
	// keeps every item. The session must be idle. held are the texts that
	// went to the agent in the same batch before the message (a subagent's
	// notification, a shell command's record, an earlier queued message):
	// the cut drops them too, so they go again with the next message.
	Rewind(ctx context.Context, sessionID, messageID string) (ev Rewound, held []string, err error)
}

// Options are run settings that core.Request does not carry.
type Options struct {
	// ServiceTier is "" or "priority" (needs Capabilities.ServiceTier).
	ServiceTier string
	// Mode is the permission mode: the sandbox commands run in and who
	// decides what needs approval ("": the engine's configured sandbox).
	Mode approval.Mode
	// Compact compacts the context before the run's first model request
	// (needs Capabilities.Compaction). CompactFocus is what the summary
	// should focus on, as /compact <instructions>.
	Compact      bool
	CompactFocus string
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
	// Stream reports the model's text as it arrives, for the run's own
	// turn requests (needs Capabilities.Stream).
	Stream bool
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
	// Compact compacts the context before the next model request, with
	// the summary focused on focus when it is not empty (ErrUnsupported
	// without Compaction).
	Compact(focus string) error
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
