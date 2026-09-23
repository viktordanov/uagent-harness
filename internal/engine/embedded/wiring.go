package embedded

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/unreallabsai/unreal-agent/harness/contextbuilder"
	"github.com/unreallabsai/unreal-agent/harness/coordinator"
	"github.com/unreallabsai/unreal-agent/harness/inbox"
	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/llm/responsesapi"
	"github.com/unreallabsai/unreal-agent/harness/operation"
	"github.com/unreallabsai/unreal-agent/harness/session"
	"github.com/unreallabsai/unreal-agent/harness/sessionstore"
	"github.com/unreallabsai/unreal-agent/harness/sessionstore/localfile"
	"github.com/unreallabsai/unreal-agent/harness/tool"
	"github.com/unreallabsai/unreal-agent/harness/tool/bash"
	"github.com/unreallabsai/unreal-agent/harness/tool/viewimage"

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
	opts, _ := ctx.Value(optionsKey{}).(engine.Options)
	w := &wiring{e: b.e, l: l, getenv: b.e.cfg.Getenv}
	a, err := w.start(ctx, opts)
	if err != nil {
		w.cleanup()
		_ = l.Stdout.Close()

		return nil, err
	}

	return a, nil
}

// wiring holds what a starting run has opened, so a failure can close it.
type wiring struct {
	e       *Engine
	l       harness.Launch
	getenv  func(string) string
	closers []func() error
}

func (w *wiring) cleanup() {
	for _, c := range slices.Backward(w.closers) {
		_ = c()
	}
	w.closers = nil
}

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

	store, err := localfile.New(w.l.SessionsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to open the session store: %w", err)
	}
	sessionID, restored, err := openSession(ctx, store, req.SessionID)
	if err != nil {
		return nil, err
	}
	logFile, err := openDatetimeLog(w.l.LogsDir, time.Now())
	if err != nil {
		return nil, err
	}
	w.closers = append(w.closers, logFile.Close)

	// The run stops through the inbox; the harness cancels only after the grace period.
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	w.closers = append(w.closers, func() error { cancel(); return nil })

	registry, err := w.tools(req, sessionID)
	if err != nil {
		return nil, err
	}
	operations := operation.NewLocalOperationManager(runCtx)
	inputs, err := inbox.New(runCtx, restored.ExternalInputIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to open the inbox: %w", err)
	}
	a := &agent{ctx: runCtx, cancel: cancel, inputs: inputs, llm: sw, done: make(chan struct{})}
	if err := a.SetEffort(req.Effort); err != nil {
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

	obs := &observer{sessionID: sessionID, out: io.MultiWriter(logFile, w.l.Stdout), cancel: cancel}
	observerID := store.AddObserver(obs.observe)
	coord := coordinator.New(coordinator.Dependencies{
		ToolHeartbeatInterval: toolHeartbeatInterval,
		SessionID:             sessionID,
		Inbox:                 inputs,
		Restored:              restored,
		Sessions:              store,
		ContextBuilder:        builder,
		LLM:                   sw,
		Tools:                 registry,
		Operations:            operations,
	})
	closers := w.closers
	w.closers = nil
	go func() {
		err := runCoordinator(runCtx, coord)
		store.RemoveObserver(observerID)
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

	return a, nil
}

// client resolves the provider, model, and credentials as the runner does
// and returns the model and the switching adapter.
func (w *wiring) client(req core.Request, opts engine.Options) (string, *switcher, error) {
	p, err := w.e.provider(req.Provider)
	if err != nil {
		return "", nil, err
	}
	baseURL := strings.TrimSpace(req.BaseURL)
	if baseURL == "" {
		baseURL = p.BaseURL
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = p.DefaultModel
	}
	if model == "" {
		return "", nil, errors.New("the model must be set for provider " + p.Name)
	}
	var apiKey string
	if p.APIKeyEnv != "" {
		apiKey = strings.TrimSpace(w.getenv("UNREAL_HARNESS_LLM_API_KEY"))
		if apiKey == "" {
			apiKey = strings.TrimSpace(w.getenv(p.APIKeyEnv))
		}
		if apiKey == "" {
			return "", nil, fmt.Errorf("UNREAL_HARNESS_LLM_API_KEY or %s must be set", p.APIKeyEnv)
		}
	}
	maxAttempts, err := w.maxAttempts(req)
	if err != nil {
		return "", nil, err
	}
	sw, err := newSwitcher(model, opts.ServiceTier == tierPriority, func(priority bool) (Client, error) {
		if priority && !p.Priority {
			return nil, errNoPriority
		}
		c, err := p.NewClient(ClientConfig{APIKey: apiKey, BaseURL: baseURL, MaxAttempts: maxAttempts, Priority: priority, Getenv: w.getenv})
		if err != nil {
			return nil, fmt.Errorf("failed to create the %s client: %w", p.Name, err)
		}

		return c, nil
	})

	return model, sw, err
}

func (w *wiring) maxAttempts(req core.Request) (int, error) {
	n := responsesapi.DefaultMaxAttempts
	if req.MaxAttempts > 0 {
		n = req.MaxAttempts
	} else if v := strings.TrimSpace(w.getenv("UNREAL_HARNESS_LLM_MAX_ATTEMPTS")); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed <= 0 {
			return 0, fmt.Errorf("invalid UNREAL_HARNESS_LLM_MAX_ATTEMPTS %q", v)
		}
		n = parsed
	}

	return n, nil
}

// tools registers Bash, ViewImage, and workspace skills, as the runner does.
func (w *wiring) tools(req core.Request, sessionID session.ID) (tool.Registry, error) {
	opsDir := filepath.Join(w.l.SessionsDir, "operations", string(sessionID))
	if err := os.MkdirAll(opsDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create the operation directory: %w", err)
	}
	shell := strings.TrimSpace(w.getenv("SHELL"))
	if shell == "" {
		shell = "/bin/sh"
	}
	skills, skillErrs := tool.DiscoverSkills(filepath.Join(req.Workspace, ".harness", "skills"))
	names := []string{tool.BashName, tool.ViewImageName}
	if len(skills) > 0 {
		names = append(names, tool.SkillUseName)
	}
	names = slices.DeleteFunc(names, func(n string) bool { return slices.Contains(req.DisallowedTools, n) })
	registry := tool.NewRegistry(tool.StaticTranslators{
		Bash:      bash.New(bash.Config{Shell: shell, Directory: req.Workspace, BaseDirectory: opsDir}),
		ViewImage: viewimage.New(viewimage.Config{Directory: req.Workspace}),
	}, names...)
	if _, ok := registry.Resolve(tool.SkillUseName); ok {
		for _, s := range skills {
			if _, err := registry.RegisterSkill(s); err != nil {
				return nil, fmt.Errorf("failed to register skill %q: %w", s.Path, err)
			}
		}
	}
	for _, err := range skillErrs {
		_, _ = fmt.Fprintf(w.l.Stderr, "skill error> %s\n", err)
	}

	return registry, nil
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

// openSession resumes the session, or creates it when it does not exist.
func openSession(ctx context.Context, store *localfile.Store, requested string) (session.ID, sessionstore.ResumeState, error) {
	id := session.ID(strings.TrimSpace(requested))
	if id == "" {
		id = session.ID(uuid.NewString())
	}
	restored, err := store.Resume(ctx, id)
	if err == nil {
		return id, restored, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", sessionstore.ResumeState{}, fmt.Errorf("failed to open session %q: %w", id, err)
	}
	snapshot, err := store.Create(ctx, id)
	if err != nil {
		return "", sessionstore.ResumeState{}, fmt.Errorf("failed to create session %q: %w", id, err)
	}

	return id, sessionstore.ResumeState{Snapshot: snapshot}, nil
}

// openDatetimeLog opens the runner's per-invocation copy of its output.
func openDatetimeLog(dir string, now time.Time) (*os.File, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create the log directory: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, now.UTC().Format("20060102-150405")+".jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("failed to open the session log: %w", err)
	}

	return f, nil
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

// writeError writes the runner's error event.
func writeError(out io.Writer, err error) {
	line, merr := json.Marshal(struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}{"error", err.Error()})
	if merr == nil {
		_, _ = out.Write(append(line, '\n'))
	}
}

// observer writes each persisted session item as one JSON line, exactly as
// the runner prints it.
type observer struct {
	sessionID session.ID
	out       io.Writer
	cancel    context.CancelFunc

	mu      sync.Mutex
	failure error
}

func (o *observer) observe(id session.ID, item sessionstore.Item) {
	if id != o.sessionID {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.failure != nil {
		return
	}
	line, err := json.Marshal(item)
	if err == nil {
		_, err = o.out.Write(append(line, '\n'))
	}
	if err != nil {
		o.failure = fmt.Errorf("failed to write session item %d: %w", item.Sequence, err)
		o.cancel()
	}
}

func (o *observer) err() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	return o.failure
}
