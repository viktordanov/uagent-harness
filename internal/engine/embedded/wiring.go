package embedded

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/unreallabsai/unreal-agent/harness/contextbuilder"
	"github.com/unreallabsai/unreal-agent/harness/coordinator"
	"github.com/unreallabsai/unreal-agent/harness/inbox"
	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/operation"
	"github.com/unreallabsai/unreal-agent/harness/sessionstore"
	"github.com/unreallabsai/unreal-agent/harness/tool"

	"github.com/viktordanov/uagent/core"
	"github.com/viktordanov/uagent/harness"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/instructions"
)

// The runner's defaults.
const (
	toolHeartbeatInterval = 10 * time.Minute
	exitInterrupted       = 130
)

// backend is the harness.Backend that starts the coordinator in process.
type backend struct{ e *Engine }

// Start reproduces the runner's Run: provider client, session store, tools,
// inbox, context builder, and coordinator. It never loads the workspace .env.
func (b backend) Start(ctx context.Context, l harness.Launch) (harness.Process, error) {
	start, _ := ctx.Value(startKey{}).(startValue)
	w := &wiring{e: b.e, l: l, getenv: b.e.cfg.Getenv, emit: start.emit}
	a, err := w.start(ctx, start.opts)
	if err != nil {
		w.cleanup()
		_ = l.Stdout.Close()

		return nil, err
	}

	return a, nil
}

// wiring holds what a starting run has opened, so a failure can close it.
// Each step that opens a resource adds its closer; once the coordinator
// starts, its goroutine owns them.
type wiring struct {
	e       *Engine
	l       harness.Launch
	getenv  func(string) string
	emit    func(core.Event)
	closers []func() error
}

func (w *wiring) cleanup() {
	for _, c := range slices.Backward(w.closers) {
		_ = c()
	}
	w.closers = nil
}

// start opens the run's resources in the runner's order and starts the
// coordinator.
func (w *wiring) start(ctx context.Context, opts engine.Options) (*agent, error) {
	req := w.l.Request
	messages, err := requestMessages(req)
	if err != nil {
		return nil, err
	}
	model, sw, err := w.client(req, opts)
	if err != nil {
		return nil, err
	}
	w.closers = append(w.closers, sw.Close)
	s, err := w.openStore(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}

	// The run stops through the inbox; the harness cancels only after the grace period.
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	w.closers = append(w.closers, func() error { cancel(); return nil })

	registry, err := w.tools(runCtx, req, s.id)
	if err != nil {
		return nil, err
	}
	operations := operation.NewLocalOperationManager(runCtx)
	comp, err := w.compactor(ctx, s, sw, opts.Compact)
	if err != nil {
		return nil, err
	}
	a, err := newAgent(runCtx, cancel, sw, s.restored, req.Effort, messages)
	if err != nil {
		return nil, err
	}
	a.compactor = comp

	builder := newContextBuilder(registry, model, req)
	obs := &observer{sessionID: s.id, out: io.MultiWriter(s.log, w.l.Stdout), cancel: cancel}
	observerID := s.store.AddObserver(obs.observe)
	coord := coordinator.New(coordinator.Dependencies{
		ToolHeartbeatInterval: toolHeartbeatInterval,
		SessionID:             s.id,
		Inbox:                 a.inputs,
		Restored:              s.restored,
		Sessions:              s.store,
		ContextBuilder:        builder,
		LLM:                   comp,
		Tools:                 registry,
		Operations:            operations,
	})
	w.launch(runCtx, a, coord, obs, func() { s.store.RemoveObserver(observerID) })

	return a, nil
}

// compactor wraps the switcher with the session's compactions and seeds the
// context in use from the session's last response.
func (w *wiring) compactor(ctx context.Context, s runStore, sw *switcher, compactFirst bool) (*compactor, error) {
	log := newCompactionLog(w.l.SessionsDir, s.id)
	rec, err := log.last()
	if err != nil {
		return nil, err
	}
	used, err := lastUsage(ctx, s.store, s.id)
	if err != nil {
		return nil, err
	}
	emit := w.emit
	if emit == nil {
		emit = func(core.Event) {}
	}
	cfg := w.e.cfg

	return &compactor{
		next: sw, log: log, emit: emit, before: cfg.BeforeCompact, window: cfg.ContextWindow, percent: cfg.AutoCompactPercent,
		record: rec, pending: compactFirst, used: used,
	}, nil
}

// newAgent opens the inbox and submits the initial settings and messages.
func newAgent(ctx context.Context, cancel context.CancelFunc, sw *switcher, restored sessionstore.ResumeState, effort string, messages []core.UserInput) (*agent, error) {
	inputs, err := inbox.New(ctx, restored.ExternalInputIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to open the inbox: %w", err)
	}
	a := &agent{ctx: ctx, cancel: cancel, inputs: inputs, llm: sw, done: make(chan struct{})}
	if err := a.SetEffort(effort); err != nil {
		return nil, err
	}
	for _, m := range messages {
		if err := a.Send(m); err != nil {
			return nil, err
		}
	}
	// Stop when idle, as the runner does: messages sent before the agent is
	// idle keep it running, so live input works until the run ends.
	if err := a.control(inbox.ControlMessage{Mode: inbox.StopWhenIdle}); err != nil {
		return nil, err
	}

	return a, nil
}

// newContextBuilder returns the builder with the model, the system prompt
// (the runner's host prompt by default), and the registry's skills and tools.
func newContextBuilder(registry tool.Registry, model string, req core.Request) contextbuilder.Builder {
	builder := contextbuilder.NewBuilder(registry.Skills()...)
	builder.SetModel(llm.Model{ID: model, ReasoningEffort: reasoningEffort(req.Effort)})
	prompt := req.SystemPrompt
	if prompt == "" {
		prompt = instructions.RunnerHostPrompt
	}
	builder.SetSystemPrompt(prompt)
	for _, d := range registry.StaticDefinitions() {
		builder.AddTool(d.Tool)
	}

	return builder
}

// launch hands the closers to a goroutine that runs the coordinator, records
// the exit code, and then closes them and the output.
func (w *wiring) launch(ctx context.Context, a *agent, coord coordinator.Coordinator, obs *observer, detach func()) {
	closers := w.closers
	w.closers = nil
	go func() {
		err := runCoordinator(ctx, coord)
		detach()
		if oerr := obs.err(); oerr != nil {
			err = oerr
		}
		switch {
		case a.interrupted.Load():
			a.code = exitInterrupted
		case err != nil:
			writeError(w.l.Stdout, err)
			_, _ = fmt.Fprintf(w.l.Stderr, "embedded: %v\n", err)
			a.code = 1
		}
		for _, c := range slices.Backward(closers) {
			_ = c()
		}
		_ = w.l.Stdout.Close()
		close(a.done)
	}()
}

// requestMessages returns the request's messages, or its prompt as one message.
func requestMessages(req core.Request) ([]core.UserInput, error) {
	if len(req.Messages) > 0 {
		return req.Messages, nil
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, errors.New("the request has no messages")
	}

	return []core.UserInput{{ID: uuid.NewString(), Text: req.Prompt}}, nil
}

// runCoordinator runs the coordinator and turns a panic into an error, so
// runner code cannot take down the TUI; the session file stays intact.
func runCoordinator(ctx context.Context, c coordinator.Coordinator) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("the agent panicked: %v", p)
		}
	}()
	if err := c.Run(ctx); err != nil {
		if ctx.Err() != nil {
			return nil // stopped by cancellation
		}

		return fmt.Errorf("the coordinator stopped: %w", err)
	}

	return nil
}
