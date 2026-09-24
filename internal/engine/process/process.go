// Package process is the engine that spawns unreal-agent-runner through
// uagent. Messages can only reach the runner when a run starts, so sessions
// queue them while a run is live.
package process

import (
	"context"
	"fmt"

	"github.com/viktordanov/uagent/core"
	"github.com/viktordanov/uagent/harness"

	"github.com/viktordanov/uagent-harness/internal/engine"
)

// Engine runs the runner as a subprocess with every uagent guard.
type Engine struct {
	h *harness.Harness
}

func New(cfg harness.Config) *Engine {
	return &Engine{h: harness.New(cfg)}
}

func (e *Engine) Name() string { return "process" }

func (e *Engine) Capabilities() engine.Capabilities { return engine.Capabilities{} }

func (e *Engine) Start(ctx context.Context, req core.Request, _ engine.Options, sink core.Sink) (engine.Run, error) {
	run, err := e.h.Start(ctx, req, sink)
	if err != nil {
		return nil, fmt.Errorf("failed to start run: %w", err)
	}

	return &processRun{run: run}, nil
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
func (r *processRun) Compact() error { return engine.ErrUnsupported }
func (r *processRun) Interrupt()     { r.run.Interrupt() }
func (r *processRun) Kill()          { r.run.Kill() }

func (r *processRun) Wait() (core.Result, error) {
	result, err := r.run.Wait()
	if err != nil {
		return result, fmt.Errorf("failed to finish run: %w", err)
	}

	return result, nil
}
