// Package process is the engine that spawns unreal-agent-runner through
// uagent. Messages can only reach the runner when a run starts, so sessions
// queue them while a run is live.
package process

import (
	"context"
	"fmt"
	"sync"

	"github.com/viktordanov/uagent/core"
	"github.com/viktordanov/uagent/harness"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/sandbox"
)

// Engine runs the runner as a subprocess with every uagent guard.
type Engine struct {
	// build is the harness configuration for a sandbox mode (nil: h
	// serves every mode).
	build func(sandbox.Mode) (harness.Config, error)

	mu sync.Mutex
	h  *harness.Harness
	// byMode are the harnesses built for the modes runs asked for.
	byMode map[sandbox.Mode]*harness.Harness
}

// New returns an engine whose runs all use cfg.
func New(cfg harness.Config) *Engine {
	return &Engine{h: harness.New(cfg)}
}

// NewSandboxed returns an engine whose runs use build's configuration for
// their permission mode's sandbox (the runner's $SHELL sandboxes each
// command), and mode's when a run names none. A mode change applies from
// the next run.
func NewSandboxed(mode sandbox.Mode, build func(sandbox.Mode) (harness.Config, error)) (*Engine, error) {
	cfg, err := build(mode)
	if err != nil {
		return nil, err
	}
	h := harness.New(cfg)

	return &Engine{build: build, h: h, byMode: map[sandbox.Mode]*harness.Harness{mode: h}}, nil
}

func (e *Engine) Name() string { return "process" }

func (e *Engine) Capabilities() engine.Capabilities { return engine.Capabilities{} }

func (e *Engine) Start(ctx context.Context, req core.Request, opts engine.Options, sink core.Sink) (engine.Run, error) {
	h, err := e.harness(opts)
	if err != nil {
		return nil, err
	}
	run, err := h.Start(ctx, req, sink)
	if err != nil {
		return nil, fmt.Errorf("failed to start run: %w", err)
	}

	return &processRun{run: run}, nil
}

// harness is the harness for the run's permission mode.
func (e *Engine) harness(opts engine.Options) (*harness.Harness, error) {
	if e.build == nil || opts.Mode == "" {
		return e.h, nil
	}
	mode := opts.Mode.Sandbox()
	e.mu.Lock()
	defer e.mu.Unlock()
	if h, ok := e.byMode[mode]; ok {
		return h, nil
	}
	cfg, err := e.build(mode)
	if err != nil {
		return nil, fmt.Errorf("failed to set up the %s sandbox: %w", mode, err)
	}
	h := harness.New(cfg)
	e.byMode[mode] = h

	return h, nil
}

type processRun struct {
	run *harness.Run
}

func (r *processRun) Send(core.UserInput) error { return engine.ErrUnsupported }
func (r *processRun) SetEffort(string) error    { return engine.ErrUnsupported }
func (r *processRun) SetModel(string) error     { return engine.ErrUnsupported }
func (r *processRun) SetServiceTier(string) error {
	return engine.ErrUnsupported
}
func (r *processRun) SetMode(approval.Mode) error { return engine.ErrUnsupported }
func (r *processRun) Compact() error              { return engine.ErrUnsupported }
func (r *processRun) Clear() error                { return engine.ErrUnsupported }
func (r *processRun) Interrupt()                  { r.run.Interrupt() }
func (r *processRun) Kill()                       { r.run.Kill() }

func (r *processRun) Wait() (core.Result, error) {
	result, err := r.run.Wait()
	if err != nil {
		return result, fmt.Errorf("failed to finish run: %w", err)
	}

	return result, nil
}
