// Package app turns what the CLI collected into a session ready to open:
// Resolve decides the settings without I/O, and Setup loads the files and
// builds the engine around them.
package app

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/compaction"
	"github.com/viktordanov/uagent-harness/internal/config"
	"github.com/viktordanov/uagent-harness/internal/review"
	"github.com/viktordanov/uagent-harness/internal/rules"
	"github.com/viktordanov/uagent-harness/internal/sandbox"
	"github.com/viktordanov/uagent-harness/internal/session"
)

// Defaults when neither a flag, the resumed session, nor the configuration
// sets a value.
const (
	CodexProvider     = "openai-codex"
	DefaultCodexModel = "gpt-6-sol"
	DefaultEffort     = "high"

	EngineEmbedded = "embedded"
	EngineProcess  = "process"
)

// Engines are the engine names --engine accepts.
var Engines = []string{EngineEmbedded, EngineProcess}

// LogLevels are the names --log-level accepts.
var LogLevels = map[string]slog.Level{"debug": slog.LevelDebug, "info": slog.LevelInfo, "warn": slog.LevelWarn, "error": slog.LevelError}

// Inputs are the values the CLI collected, flags already folded with the
// environment. An empty string means not set; the Set fields say whether a
// flag with a default value was given.
type Inputs struct {
	ConfigPath string
	StateDir   string
	SessionRef string // a session to resume, by ID or unique prefix
	LogLevel   string

	Provider  string
	Model     string
	Effort    string
	Workspace string
	Engine    string
	Runner    string
	BaseURL   string

	Timeout    time.Duration
	TimeoutSet bool
	MaxDisk    string
	MaxDiskSet bool
	Fast       bool
	FastSet    bool
	// Sandbox is the --sandbox mode.
	Sandbox string
	// Ask is the --ask approval policy.
	Ask string

	AllowDotenv    bool
	NoInstructions bool
}

// Resolved is what Resolve decides.
type Resolved struct {
	// Settings has no SystemPrompt yet; Setup adds it from the instructions.
	Settings session.Settings
	Engine   string
	MaxDisk  int64
	// Instructions reports whether to load AGENTS.md and CLAUDE.md files.
	Instructions bool
	// Sandbox is the policy commands run under. Its Workspace and
	// WritableRoots are as given; Setup makes them absolute.
	Sandbox sandbox.Policy
	// Env is which environment variables commands get.
	Env sandbox.EnvPolicy
	// Compaction is when the embedded engine compacts and how it
	// summarizes. Its Prompt is compact_prompt; Setup reads
	// CompactPromptFile into it when that is empty.
	Compaction        compaction.Settings
	CompactPromptFile string
	// Approval is when the user is asked to approve a command.
	Approval approval.Policy
	// Rules are the configured [approvals] prefixes; Setup adds the rules
	// files.
	Rules []rules.Rule
	// ApprovalsReviewer is auto_review or user.
	ApprovalsReviewer string
	// Review is the auto-reviewer's model, effort, and timeout.
	Review review.Config
	// Agents are the subagent settings.
	Agents Agents
}

// UsageError is an error in what the user asked for, such as an invalid
// flag or configuration value.
type UsageError struct{ Err error }

func (e *UsageError) Error() string { return e.Err.Error() }

func (e *UsageError) Unwrap() error { return e.Err }

func usage(err error) error { return &UsageError{Err: err} }

// Resolve combines the inputs, the resumed session (the zero Info for a new
// one), and the configuration, in that order, then the defaults. It does no
// I/O.
func Resolve(in Inputs, resumed session.Info, cfg config.Config) (Resolved, error) {
	s := session.Settings{
		Provider:    first(in.Provider, resumed.Provider, cfg.Provider, CodexProvider),
		Effort:      first(in.Effort, resumed.Effort, cfg.Effort, DefaultEffort),
		Workspace:   workspaceFor(in, resumed),
		BaseURL:     in.BaseURL,
		AllowDotenv: in.AllowDotenv,
	}
	s.Model = pickModel(in, resumed, cfg, s.Provider)
	timeout, err := pickTimeout(in, cfg)
	if err != nil {
		return Resolved{}, err
	}
	s.Timeout = timeout
	if in.Fast || (!in.FastSet && cfg.Fast) {
		s.ServiceTier = "priority"
	}
	if err := s.Validate(); err != nil {
		return Resolved{}, usage(err)
	}
	maxDisk, err := pickMaxDisk(in, cfg)
	if err != nil {
		return Resolved{}, err
	}
	eng, err := pickEngine(in, cfg, s.ServiceTier != "")
	if err != nil {
		return Resolved{}, err
	}
	policy, err := pickSandbox(in, cfg, s.Workspace)
	if err != nil {
		return Resolved{}, err
	}
	s.Sandbox = string(policy.Mode)

	envPolicy, err := pickEnv(cfg.ShellEnvironmentPolicy)
	if err != nil {
		return Resolved{}, err
	}
	compact, promptFile, err := pickCompaction(cfg, &s)
	if err != nil {
		return Resolved{}, err
	}
	approvalPolicy, configured, err := pickApprovals(in, cfg)
	if err != nil {
		return Resolved{}, err
	}
	reviewer, reviewCfg, err := pickReview(cfg, s)
	if err != nil {
		return Resolved{}, err
	}
	agentSettings, err := pickAgents(cfg.Agents)
	if err != nil {
		return Resolved{}, err
	}

	return Resolved{
		Settings: s, Engine: eng, MaxDisk: maxDisk, Instructions: !in.NoInstructions && cfg.InstructionsEnabled(),
		Sandbox: policy, Env: envPolicy, Compaction: compact, CompactPromptFile: promptFile, Approval: approvalPolicy, Rules: configured,
		ApprovalsReviewer: reviewer, Review: reviewCfg, Agents: agentSettings,
	}, nil
}

// pickEnv checks the configured environment policy.
func pickEnv(c config.ShellEnvironmentPolicy) (sandbox.EnvPolicy, error) {
	switch c.Inherit {
	case "", sandbox.InheritAll, sandbox.InheritCore, sandbox.InheritNone:
	default:
		return sandbox.EnvPolicy{}, usage(fmt.Errorf("invalid shell_environment_policy.inherit %q (want all, core, or none)", c.Inherit))
	}

	return sandbox.EnvPolicy{
		Inherit: c.Inherit, IgnoreDefaultExcludes: c.IgnoreDefaultExcludes,
		Exclude: c.Exclude, IncludeOnly: c.IncludeOnly, Set: c.Set,
	}, nil
}

// pickSandbox is the --sandbox flag, the configured sandbox_mode, or
// workspace-write, Codex's default for trusted projects.
func pickSandbox(in Inputs, cfg config.Config, workspace string) (sandbox.Policy, error) {
	mode, err := sandbox.ParseMode(first(in.Sandbox, cfg.SandboxMode, string(sandbox.WorkspaceWrite)))
	if err != nil {
		return sandbox.Policy{}, usage(err)
	}

	return sandbox.Policy{
		Mode: mode, Workspace: workspace,
		WritableRoots: cfg.SandboxWorkspaceWrite.WritableRoots, Network: cfg.SandboxWorkspaceWrite.NetworkAccess,
	}, nil
}

// pickModel is the model for provider. A provider flag that changes the
// provider drops the resumed and configured models, which belong to the
// other provider.
func pickModel(in Inputs, resumed session.Info, cfg config.Config, provider string) string {
	fallback := ""
	if provider == CodexProvider {
		fallback = DefaultCodexModel
	}
	if !providerChanged(in, resumed, cfg) {
		return first(in.Model, resumed.Model, cfg.Model, fallback)
	}

	return first(in.Model, fallback)
}

// providerChanged reports whether the provider flag picks another provider
// than the one that would apply without it: the resumed session's, the
// configured one, or the default.
func providerChanged(in Inputs, resumed session.Info, cfg config.Config) bool {
	return in.Provider != "" && in.Provider != first(resumed.Provider, cfg.Provider, CodexProvider)
}

// pickTimeout is the timeout flag when given, else the configured timeout,
// else the flag's default.
func pickTimeout(in Inputs, cfg config.Config) (time.Duration, error) {
	if in.TimeoutSet {
		return in.Timeout, nil
	}
	d, ok, err := cfg.TimeoutValue()
	if err != nil {
		return 0, usage(err)
	}
	if !ok {
		return in.Timeout, nil
	}

	return d, nil
}

// pickMaxDisk is the max-disk flag when given, else the configured limit,
// else the flag's default.
func pickMaxDisk(in Inputs, cfg config.Config) (int64, error) {
	text := in.MaxDisk
	if !in.MaxDiskSet && cfg.MaxDisk != "" {
		text = cfg.MaxDisk
	}
	n, err := ParseSize(text)
	if err != nil {
		return 0, usage(errors.New("max_disk: " + err.Error()))
	}

	return n, nil
}

// pickEngine is the engine flag, the configured engine, or embedded; fast
// (priority processing) needs the embedded engine.
func pickEngine(in Inputs, cfg config.Config, fast bool) (string, error) {
	eng := first(in.Engine, cfg.Engine, EngineEmbedded)
	switch eng {
	case EngineProcess:
		if fast {
			return "", usage(errors.New("--fast needs the embedded engine"))
		}
	case EngineEmbedded:
	default:
		return "", usage(errors.New("invalid engine " + eng + " (want embedded or process)"))
	}

	return eng, nil
}

// workspaceFor is the workspace flag, the resumed session's, or the current
// directory, possibly relative.
func workspaceFor(in Inputs, resumed session.Info) string {
	return first(in.Workspace, resumed.Workspace, ".")
}

// first returns the first non-empty value.
func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}

	return ""
}

// ParseSize parses sizes like 500M, 5G, or 1024 (bytes). "0" disables the limit.
func ParseSize(s string) (int64, error) {
	s = strings.ToUpper(strings.TrimSuffix(strings.TrimSpace(s), "B"))
	mult := int64(1)
	if n := len(s); n > 0 {
		switch s[n-1] {
		case 'K':
			mult, s = 1<<10, s[:n-1]
		case 'M':
			mult, s = 1<<20, s[:n-1]
		case 'G':
			mult, s = 1<<30, s[:n-1]
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0, errors.New("invalid size " + strconv.Quote(s))
	}

	return int64(v * float64(mult)), nil
}
