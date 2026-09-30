package harnesstest

import (
	"context"
	"errors"
	"fmt"

	"github.com/viktordanov/uagent/core"
	"github.com/viktordanov/uagent/harness"

	"github.com/viktordanov/uah/internal/approval"
	"github.com/viktordanov/uah/internal/engine"
)

// errNotLive is what a runner run answers to every live change: the
// runner reads its request once, when it starts.
var errNotLive = errors.New("a runner subprocess takes nothing while it runs")

// RunnerEngine is an engine.Engine for tests that spawns a runner binary
// through uagent's harness with cfg: the real unreal-agent-runner
// (RealRunner), which the equivalence test compares the embedded engine
// with, or uagent's fake runner (FakeRunner), whose runs need no model.
// Every live change fails, so a session queues messages and applies
// settings from the next run.
func RunnerEngine(cfg harness.Config) engine.Engine {
	return runnerEngine{h: harness.New(cfg)}
}

type runnerEngine struct{ h *harness.Harness }

func (runnerEngine) Name() string   { return "runner" }
func (runnerEngine) Priority() bool { return false }

func (e runnerEngine) Start(ctx context.Context, req core.Request, _ engine.Options, sink core.Sink) (engine.Run, error) {
	run, err := e.h.Start(ctx, req, sink)
	if err != nil {
		return nil, fmt.Errorf("failed to start run: %w", err)
	}

	return runnerRun{run: run}, nil
}

type runnerRun struct{ run *harness.Run }

func (runnerRun) Send(core.UserInput) error   { return errNotLive }
func (runnerRun) SetEffort(string) error      { return errNotLive }
func (runnerRun) SetModel(string) error       { return errNotLive }
func (runnerRun) SetServiceTier(string) error { return errNotLive }
func (runnerRun) SetMode(approval.Mode) error { return errNotLive }
func (runnerRun) Compact(string) error        { return errNotLive }
func (runnerRun) Clear() error                { return errNotLive }
func (r runnerRun) Interrupt()                { r.run.Interrupt() }
func (r runnerRun) Kill()                     { r.run.Kill() }

func (r runnerRun) Wait() (core.Result, error) {
	result, err := r.run.Wait()
	if err != nil {
		return result, fmt.Errorf("failed to finish run: %w", err)
	}

	return result, nil
}
