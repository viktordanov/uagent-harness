package state

import (
	"time"

	"github.com/viktordanov/uagent-harness/internal/contextusage"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/mcp"
)

// Kind is what a transcript item shows.
type Kind int

const (
	KindUser Kind = iota
	KindRun
	KindTurn
	KindTool
	KindAssistant
	KindReasoning
	KindNotice
	// KindAgent is a subagent: Name is its nickname, Label its role, Text
	// its ID, Detail its state, and Started when its current work began.
	KindAgent
	// KindContext is a /context breakdown in Context.
	KindContext
	// KindMCP is the /mcp panel: MCP holds the servers, and Final asks for
	// the verbose form.
	KindMCP
)

// InputState tracks a user message from the queue to the runner.
type InputState string

const (
	InputQueued    InputState = "queued"
	InputSent      InputState = "sent"
	InputDelivered InputState = "delivered"
	InputFailed    InputState = "failed"
)

// ToolState tracks one tool call.
type ToolState string

const (
	ToolCalled  ToolState = "called"
	ToolRunning ToolState = "running"
	ToolOK      ToolState = "ok"
	ToolFailed  ToolState = "failed"
	ToolStopped ToolState = "stopped" // still running when its run ended
)

// Item is one transcript entry. Fields apply by Kind. Version grows on every
// change, so renderers can cache by (Key, Version).
type Item struct {
	Kind    Kind
	Key     string
	Version int

	// KindUser, KindAssistant, KindReasoning, KindNotice
	Text  string
	Input InputState
	Final bool
	Level string // KindNotice: "info", "warning", "error"

	// KindRun
	RunID  string
	Status core.Status
	Wall   time.Duration
	Tokens int64

	// KindTurn
	Turn     int
	In, Out  int64
	Pending  bool
	Started  time.Time
	Duration time.Duration

	// KindMCP
	MCP []mcp.ServerStatus

	// KindTool
	Name   string
	Label  string
	Tool   ToolState
	Detail string

	// KindContext
	Context *contextusage.Usage
}

// Live reports whether an item changes with time (spinners, elapsed times)
// and must not be served from a cache.
func (it Item) Live() bool {
	return (it.Kind == KindTurn && it.Pending) || (it.Kind == KindTool && (it.Tool == ToolRunning || it.Tool == ToolCalled)) ||
		(it.Kind == KindRun && it.Status == core.StatusRunning) || (it.Kind == KindAgent && it.Detail == "running")
}
