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
	"log/slog"
	"os"

	"github.com/viktordanov/uagent/core"
	"github.com/viktordanov/uagent/harness"

	"github.com/viktordanov/uagent-harness/internal/engine"
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
}

// Engine runs the agent in process.
type Engine struct {
	cfg Config
	h   *harness.Harness
}

func New(cfg Config) *Engine {
	if cfg.Getenv == nil {
		cfg.Getenv = os.Getenv
	}
	if cfg.Providers == nil {
		cfg.Providers = DefaultProviders()
	}
	e := &Engine{cfg: cfg}
	e.h = harness.New(harness.Config{
		Backend: backend{e}, StateDir: cfg.StateDir, MaxDisk: cfg.MaxDisk, Logger: cfg.Logger, Getenv: cfg.Getenv,
	})

	return e
}

func (e *Engine) Name() string { return "embedded" }

func (e *Engine) Capabilities() engine.Capabilities {
	p, err := e.provider(e.cfg.Provider)

	return engine.Capabilities{LiveInput: true, LiveEffort: true, LiveModel: true, ServiceTier: err == nil && p.Priority}
}

type optionsKey struct{}

func (e *Engine) Start(ctx context.Context, req core.Request, opts engine.Options, sink core.Sink) (engine.Run, error) {
	r, err := e.h.Start(context.WithValue(ctx, optionsKey{}, opts), req, sink)
	if err != nil {
		return nil, fmt.Errorf("failed to start run: %w", err)
	}
	a, ok := r.Process().(*agent)
	if !ok {
		r.Kill()

		return nil, fmt.Errorf("unexpected process %T", r.Process())
	}

	return &run{run: r, agent: a}, nil
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
	run   *harness.Run
	agent *agent
}

func (r *run) Send(in core.UserInput) error     { return r.agent.Send(in) }
func (r *run) SetEffort(effort string) error    { return r.agent.SetEffort(effort) }
func (r *run) SetModel(model string) error      { return r.agent.SetModel(model) }
func (r *run) SetServiceTier(tier string) error { return r.agent.SetServiceTier(tier) }
func (r *run) Interrupt()                       { r.run.Interrupt() }
func (r *run) Kill()                            { r.run.Kill() }

func (r *run) Wait() (core.Result, error) {
	result, err := r.run.Wait()
	if err != nil {
		return result, fmt.Errorf("failed to finish run: %w", err)
	}

	return result, nil
}
