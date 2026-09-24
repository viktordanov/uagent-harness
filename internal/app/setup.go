package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/viktordanov/uagent/harness"

	"github.com/viktordanov/uagent-harness/internal/compaction"
	"github.com/viktordanov/uagent-harness/internal/config"
	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/engine/embedded"
	"github.com/viktordanov/uagent-harness/internal/engine/process"
	"github.com/viktordanov/uagent-harness/internal/hooks"
	"github.com/viktordanov/uagent-harness/internal/instructions"
	"github.com/viktordanov/uagent-harness/internal/mcp"
	"github.com/viktordanov/uagent-harness/internal/sandbox"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/store"
)

// Result is everything needed to open a session.
type Result struct {
	StateDir string
	Engine   engine.Engine
	Options  session.Options
	Config   config.Config
}

// Setup resolves the inputs against the resumed session (in.SessionRef) and
// the configuration, loads instructions and hooks, and builds the engine.
// Diagnostic logs go to logOutput.
func Setup(ctx context.Context, in Inputs, logOutput io.Writer) (Result, error) {
	stateDir, err := filepath.Abs(in.StateDir)
	if err != nil {
		return Result{}, fmt.Errorf("failed to resolve state dir: %w", err)
	}
	var resumed session.Info
	opts := session.Options{}
	if in.SessionRef != "" {
		info, err := FindSession(ctx, stateDir, in.SessionRef)
		if err != nil {
			return Result{}, err
		}
		resumed, opts.ID, opts.Resumed = info, info.ID, true
	}
	// Resolve then takes the absolute workspace as if it were the flag.
	if in.Workspace, err = filepath.Abs(workspaceFor(in, resumed)); err != nil {
		return Result{}, fmt.Errorf("failed to resolve workspace: %w", err)
	}
	cfg, _, err := config.Load(in.ConfigPath, in.Workspace)
	if err != nil {
		return Result{}, usage(err)
	}
	r, err := Resolve(in, resumed, cfg)
	if err != nil {
		return Result{}, err
	}
	if r.Instructions {
		if opts.Instructions, r.Settings.SystemPrompt, err = loadInstructions(in.Workspace, cfg); err != nil {
			return Result{}, err
		}
	}
	opts.Settings = r.Settings
	opts.SessionsDir = filepath.Join(stateDir, "sessions")
	if opts.Hooks, err = loadHooks(cfg, in.Workspace); err != nil {
		return Result{}, err
	}
	r.Sandbox = absPolicy(r.Sandbox, in.Workspace)
	logger := slog.New(slog.NewTextHandler(logOutput, &slog.HandlerOptions{Level: LogLevels[in.LogLevel]}))
	servers, err := mcpManager(cfg, in.Workspace, logOutput)
	if err != nil {
		return Result{}, err
	}
	eng, err := newEngine(r, in.Runner, stateDir, logger, servers, &opts) //nolint:contextcheck // on Linux, the sandbox probes bwrap once per process, with its own timeout
	if err != nil {
		return Result{}, err
	}

	return Result{StateDir: stateDir, Engine: eng, Options: opts, Config: cfg}, nil
}

// loadHooks builds the hook runner for the configured hooks (nil when there
// are none).
func loadHooks(cfg config.Config, workspace string) (*hooks.Runner, error) {
	list, err := cfg.HookList()
	if err != nil {
		return nil, usage(err)
	}
	if len(list) == 0 {
		return nil, nil //nolint:nilnil // a nil runner runs no hooks
	}
	trust, err := hooks.LoadTrust(HookTrustFile())
	if err != nil {
		return nil, usage(err)
	}
	runner, err := hooks.New(list, trust, workspace)
	if err != nil {
		return nil, usage(err)
	}

	return runner, nil
}

// newEngine builds the resolved engine and adds its notices to opts.
func newEngine(r Resolved, runnerPath, stateDir string, logger *slog.Logger, servers *mcp.Manager, opts *session.Options) (engine.Engine, error) {
	sandboxDir := filepath.Join(stateDir, "sandbox")
	if r.Engine == EngineProcess {
		runner, err := harness.FindRunner(runnerPath)
		if err != nil {
			return nil, usage(err)
		}
		if opts.Hooks.Has(hooks.PreToolUse, "") {
			opts.Notices = append(opts.Notices, "PreToolUse hooks need the embedded engine; they do not run on the process engine")
		}
		if servers != nil {
			opts.Notices = append(opts.Notices, "MCP servers need the embedded engine; they do not start on the process engine")
		}
		// The runner runs each command with $SHELL, so a sandboxing shell
		// sandboxes every command without changing the runner.
		shell, err := sandbox.Shell(sandboxDir, r.Sandbox, r.Env, RealShell())
		if errors.Is(err, sandbox.ErrUnavailable) {
			opts.Notices = append(opts.Notices, "no sandbox is available on this system; commands run without one")
			shell = RealShell()
		} else if err != nil {
			return nil, err
		}
		backend := harness.RunnerBackend{Path: runner, Env: []string{"SHELL=" + shell}}

		return process.New(harness.Config{Backend: backend, StateDir: stateDir, MaxDisk: r.MaxDisk, Logger: logger}), nil
	}
	emb := embedded.New(embedded.Config{
		StateDir: stateDir, MaxDisk: r.MaxDisk, Logger: logger, Provider: r.Settings.Provider, Hooks: opts.Hooks,
		Sandbox: &r.Sandbox, SandboxDir: sandboxDir, Env: r.Env, MCP: servers,
		AutoCompactPercent: r.AutoCompactPercent, ContextWindow: r.Settings.ContextWindow,
		BeforeCompact: preCompactHook(opts.Hooks, r.Settings),
	})
	if r.Settings.ServiceTier != "" && !emb.Capabilities().ServiceTier {
		return nil, usage(errors.New("--fast needs the openai or openai-codex provider"))
	}

	return emb, nil
}

// preCompactHook runs PreCompact hooks before each compaction; a block
// cancels it. It is nil without such hooks.
func preCompactHook(runner *hooks.Runner, s session.Settings) func(context.Context, string, compaction.Trigger) error {
	if !runner.Has(hooks.PreCompact, "") {
		return nil
	}

	return func(ctx context.Context, sessionID string, t compaction.Trigger) error {
		d := runner.Run(ctx, hooks.Input{
			Event: hooks.PreCompact, SessionID: sessionID, Cwd: s.Workspace, Model: s.Model, Effort: s.Effort, Trigger: string(t),
		})
		if d.Block {
			return fmt.Errorf("a PreCompact hook stopped the compaction: %s", d.Reason)
		}

		return nil
	}
}

// RealShell is the user's shell for commands: $SHELL, or /bin/sh.
func RealShell() string {
	if s := strings.TrimSpace(os.Getenv("SHELL")); s != "" {
		return s
	}

	return "/bin/sh"
}

// absPolicy makes the policy's paths absolute: the workspace, and writable
// roots with ~ for the home directory and others relative to the workspace.
func absPolicy(p sandbox.Policy, workspace string) sandbox.Policy {
	p.Workspace = workspace
	roots := make([]string, 0, len(p.WritableRoots))
	for _, r := range p.WritableRoots {
		if rest, ok := strings.CutPrefix(r, "~"); ok && (rest == "" || strings.HasPrefix(rest, "/")) {
			if home, err := os.UserHomeDir(); err == nil {
				r = home + rest
			}
		}
		if !filepath.IsAbs(r) {
			r = filepath.Join(workspace, r)
		}
		roots = append(roots, filepath.Clean(r))
	}
	p.WritableRoots = roots

	return p
}

// HookTrustFile records the project hook commands the user approved.
func HookTrustFile() string { return filepath.Join(config.Dir(), "trusted-hooks.json") }

// FindSession finds a session by exact ID or unique prefix.
func FindSession(ctx context.Context, stateDir, ref string) (session.Info, error) {
	infos, err := store.List(ctx, stateDir)
	if err != nil {
		return session.Info{}, err
	}
	var matches []session.Info
	for _, info := range infos {
		if info.ID == ref {
			return info, nil
		}
		if strings.HasPrefix(info.ID, ref) {
			matches = append(matches, info)
		}
	}
	switch len(matches) {
	case 0:
		return session.Info{}, usage(fmt.Errorf("no session matches %q (see uah sessions)", ref))
	case 1:
		return matches[0], nil
	}

	return session.Info{}, usage(fmt.Errorf("%q matches %d sessions; use more of the ID", ref, len(matches)))
}

// loadInstructions discovers and assembles instruction files, returning the
// event to report and the host prompt ("" when there are none).
func loadInstructions(workspace string, cfg config.Config) (*session.InstructionsLoaded, string, error) {
	fallbacks, markers, maxBytes := cfg.InstructionOptions()
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		if home, err := os.UserHomeDir(); err == nil {
			codexHome = filepath.Join(home, ".codex")
		}
	}
	userFiles := []string{filepath.Join(config.Dir(), "AGENTS.md")}
	if codexHome != "" {
		userFiles = append(userFiles, filepath.Join(codexHome, "AGENTS.md"))
	}
	files, err := instructions.Discover(workspace, userFiles, instructions.Options{FallbackFilenames: fallbacks, RootMarkers: markers})
	if err != nil {
		return nil, "", fmt.Errorf("failed to find instructions: %w", err)
	}
	if len(files) == 0 {
		return nil, "", nil
	}
	text, used, truncated, err := instructions.Assemble(files, maxBytes)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read instructions: %w", err)
	}
	paths := make([]string, 0, len(used))
	for _, f := range used {
		paths = append(paths, f.Path)
	}

	return &session.InstructionsLoaded{Files: paths, Bytes: len(text), Truncated: truncated}, instructions.HostPrompt(text), nil
}
