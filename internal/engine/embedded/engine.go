// Package embedded is the engine that runs unreal-agent-runner's packages in
// process. It reproduces the runner's wiring (cmd/internal/agentrunner/run.go
// in v0.1.1) behind uagent's harness, so runs keep every guard, the session
// lock, and the run records, and it adds what a subprocess cannot offer:
// messages, effort, model, and service tier changes that reach a live run.
package embedded

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"

	"github.com/viktordanov/uagent/core"
	"github.com/viktordanov/uagent/harness"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/compaction"
	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/hooks"
	"github.com/viktordanov/uagent-harness/internal/mcp"
	"github.com/viktordanov/uagent-harness/internal/review"
	"github.com/viktordanov/uagent-harness/internal/sandbox"
)

const tierPriority = "priority"

var errStopped = errors.New("the run has stopped")

// Config configures the engine.
type Config struct {
	StateDir string
	// MaxDisk stops a run whose tool output exceeds this many bytes (0 disables).
	MaxDisk int64
	Logger  *slog.Logger
	// Provider is the session's provider; it decides whether /fast is offered.
	Provider string
	// Getenv reads credentials and SHELL (default os.Getenv).
	Getenv func(string) string
	// Providers replaces the runner's provider table, for tests.
	Providers []Provider
	// Hooks, when set, runs PreToolUse hooks before each tool call.
	Hooks *hooks.Runner
	// Sandbox, when set, is the policy Bash commands run under; its
	// Workspace is replaced by each request's. SandboxDir holds the
	// sandboxing shells.
	Sandbox    *sandbox.Policy
	SandboxDir string
	// Env is which environment variables commands get (the zero value is
	// all of them). It applies when Sandbox is set.
	Env sandbox.EnvPolicy
	// AutoCompactPercent compacts the context before a model request once
	// the last response used this share of the model's window (0: never).
	AutoCompactPercent int
	// ContextWindow overrides the model table's context window (tokens).
	ContextWindow int64
	// BeforeCompact, when set, runs as each compaction starts; an error
	// cancels the compaction. A PreCompact hook attaches here.
	BeforeCompact func(ctx context.Context, sessionID string, trigger compaction.Trigger) error
	// MCP, when set, offers its servers' tools; the engine closes it.
	MCP *mcp.Manager
	// InstructionFiles are the instruction files in the host prompt, in
	// order, so /context can list them.
	InstructionFiles []string
	// AutoReview puts the auto-reviewer in front of the user for actions
	// that need approval (approvals_reviewer = "auto_review"), with Review's
	// model, effort, and timeout.
	AutoReview bool
	Review     review.Config
	// Approver decides how each command runs when Sandbox is set: the
	// rules and the approval policy. Nil applies no rules and asks for
	// escalations.
	Approver *approval.Approver
	// Subagents, when set, offers its tools to the runs it attaches and
	// hears when the user interrupts a run; the engine closes it when it is
	// an io.Closer.
	Subagents engine.Subagents
}

// Engine runs the agent in process.
type Engine struct {
	cfg Config
	h   *harness.Harness
	// transcripts feed the auto-reviewer across a session's runs, by
	// session ID, so a subagent's review sees its own session.
	transcripts sync.Map
	// last are each session's latest model request, for /context.
	last lastRequests
	// forks are the forked sessions whose first run has not started;
	// cacheKeys are the sessions whose prompt cache key is not their ID.
	forks, cacheKeys sync.Map
}

// Forget drops what the engine kept for a session that closed: the
// auto-reviewer's transcript, the last request for /context, and a fork or
// cache key it may have (engine.Forgetter).
func (e *Engine) Forget(sessionID string) {
	e.transcripts.Delete(sessionID)
	e.forks.Delete(sessionID)
	e.cacheKeys.Delete(sessionID)
	e.last.forget(sessionID)
}

func New(cfg Config) *Engine {
	if cfg.Getenv == nil {
		cfg.Getenv = os.Getenv
	}
	if cfg.Providers == nil {
		cfg.Providers = DefaultProviders()
	}
	if cfg.Approver == nil {
		cfg.Approver = approval.New(approval.Config{})
	}
	e := &Engine{cfg: cfg}
	e.h = harness.New(harness.Config{
		Backend: backend{e}, StateDir: cfg.StateDir, MaxDisk: cfg.MaxDisk, Logger: cfg.Logger, Getenv: cfg.Getenv,
	})

	return e
}

func (e *Engine) Name() string { return "embedded" }

// MCPServers reports the MCP servers, starting them if needed.
func (e *Engine) MCPServers() []mcp.ServerStatus {
	if e.cfg.MCP == nil {
		return nil
	}

	return e.cfg.MCP.Status()
}

// Close stops the subagents and the MCP servers; a later run starts the
// servers again.
func (e *Engine) Close() error {
	var errs []error
	if c, ok := e.cfg.Subagents.(io.Closer); ok {
		errs = append(errs, c.Close())
	}
	if e.cfg.MCP != nil {
		errs = append(errs, e.cfg.MCP.Close())
	}

	return errors.Join(errs...)
}

func (e *Engine) Capabilities() engine.Capabilities {
	p, err := e.provider(e.cfg.Provider)

	return engine.Capabilities{LiveInput: true, LiveEffort: true, LiveModel: true, ServiceTier: err == nil && p.Priority, Compaction: true, LiveMode: true}
}

// startKey carries a run's options and event sink to the backend.
type (
	startKey   struct{}
	startValue struct {
		opts engine.Options
		emit func(core.Event)
	}
)

func (e *Engine) Start(ctx context.Context, req core.Request, opts engine.Options, sink core.Sink) (engine.Run, error) {
	ls := &lockedSink{sink: sink, tap: e.transcript(req.SessionID).observe}
	r, err := e.h.Start(context.WithValue(ctx, startKey{}, startValue{opts: opts, emit: ls.emit}), req, ls.emit)
	if err != nil {
		return nil, fmt.Errorf("failed to start run: %w", err)
	}
	a, ok := r.Process().(*agent)
	if !ok {
		r.Kill()

		return nil, fmt.Errorf("unexpected process %T", r.Process())
	}

	return &run{run: r, agent: a, subagents: e.cfg.Subagents, sessionID: req.SessionID}, nil
}

// transcript is the session's auto-review transcript.
func (e *Engine) transcript(sessionID string) *transcript {
	t, _ := e.transcripts.LoadOrStore(sessionID, newTranscript())

	return t.(*transcript) //nolint:forcetypeassert // the map holds only transcripts
}

func (e *Engine) provider(name string) (Provider, error) {
	for _, p := range e.cfg.Providers {
		if p.Name == name {
			return p, nil
		}
	}

	return Provider{}, fmt.Errorf("unsupported provider %q", name)
}

// run is a live embedded run.
type run struct {
	run       *harness.Run
	agent     *agent
	subagents engine.Subagents
	sessionID string
}

func (r *run) Send(in core.UserInput) error     { return r.agent.Send(in) }
func (r *run) SetEffort(effort string) error    { return r.agent.SetEffort(effort) }
func (r *run) SetModel(model string) error      { return r.agent.SetModel(model) }
func (r *run) SetServiceTier(tier string) error { return r.agent.SetServiceTier(tier) }
func (r *run) SetMode(m approval.Mode) error    { return r.agent.SetMode(m) }
func (r *run) Compact() error                   { return r.agent.Compact() }
func (r *run) Clear() error                     { return r.agent.Clear() }

// Interrupt stops the run and its session's subagents' live runs, as the
// user expects of an interrupt.
func (r *run) Interrupt() {
	if r.subagents != nil {
		r.subagents.Interrupt(r.sessionID)
	}
	r.run.Interrupt()
}
func (r *run) Kill() { r.run.Kill() }

func (r *run) Wait() (core.Result, error) {
	result, err := r.run.Wait()
	if err != nil {
		return result, fmt.Errorf("failed to finish run: %w", err)
	}

	return result, nil
}

// lockedSink lets the engine add its own events to a run's stream: one
// goroutine at a time, and none after RunFinished.
type lockedSink struct {
	mu       sync.Mutex
	sink     core.Sink
	tap      func(core.Event) // sees every event first
	finished bool
}

func (s *lockedSink) emit(e core.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return
	}
	if _, ok := e.(core.RunFinished); ok {
		s.finished = true
	}
	if s.tap != nil {
		s.tap(e)
	}
	s.sink(e)
}

// Subagents is the engine's subagents, for a view that follows one.
func (e *Engine) Subagents() engine.Subagents { return e.cfg.Subagents }
