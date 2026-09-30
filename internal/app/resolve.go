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

	"github.com/viktordanov/uah/internal/approval"
	"github.com/viktordanov/uah/internal/compaction"
	"github.com/viktordanov/uah/internal/config"
	"github.com/viktordanov/uah/internal/engine"
	"github.com/viktordanov/uah/internal/engine/embedded"
	"github.com/viktordanov/uah/internal/review"
	"github.com/viktordanov/uah/internal/rules"
	"github.com/viktordanov/uah/internal/sandbox"
	"github.com/viktordanov/uah/internal/session"
)

// Defaults when neither a flag, the resumed session, nor the configuration
// sets a value.
const (
	CodexProvider = "openai-codex"
	DefaultEffort = "high"
)

// LogLevels are the names --log-level accepts.
var LogLevels = map[string]slog.Level{"debug": slog.LevelDebug, "info": slog.LevelInfo, "warn": slog.LevelWarn, "error": slog.LevelError}

// Inputs are the values the CLI collected, flags already folded with the
// environment. An empty string means not set; the Set fields say whether a
// flag with a default value was given.
type Inputs struct {
	ConfigPath string
	StateDir   string
	SessionRef string // a session to resume, by ID or unique prefix
	// NewSessionID is --session-id: the ID of a new session (a UUID).
	NewSessionID string
	LogLevel     string

	Provider  string
	Model     string
	Effort    string
	Workspace string
	BaseURL   string

	MaxDisk    string
	MaxDiskSet bool
	// MaxAttempts is --max-attempts or its environment variable (0: unset).
	MaxAttempts int
	Fast        bool
	FastSet     bool
	// Sandbox is the --sandbox mode.
	Sandbox string
	// Ask is the --ask approval policy.
	Ask string
	// Yolo is --yolo: yolo mode, and yolo in the shift+tab cycle.
	Yolo bool

	AllowDotenv    bool
	NoInstructions bool

	// RunStateDir, when set, takes the session's files and run records in
	// place of StateDir: `uah exec --ephemeral` passes a temporary directory.
	RunStateDir string
}

// Resolved is what Resolve decides.
type Resolved struct {
	// Settings has no SystemPrompt yet; Setup adds it from the instructions.
	Settings session.Settings
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
	// WebSearch is live or disabled; the engine offers live search only
	// on a provider that has it.
	WebSearch string
	// DefaultModel reports that no flag, resumed session, or file named the
	// model on openai-codex or openai: Settings.Model is then the provider's
	// fallback until SettleModel sees the provider's list.
	DefaultModel bool
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
	s.Model = pickModel(in, resumed, cfg)
	defaulted := s.Model == "" && fallbackModels[s.Provider] != ""
	if defaulted {
		s.Model = fallbackModels[s.Provider]
	}
	var err error
	if s.MaxAttempts, err = pickMaxAttempts(in, cfg); err != nil {
		return Resolved{}, err
	}
	if pickFast(in, resumed, cfg) {
		if !embedded.Priority(s.Provider) {
			return Resolved{}, usage(errors.New("--fast needs the openai or openai-codex provider"))
		}
		s.ServiceTier = "priority"
	}
	if err := s.Validate(); err != nil {
		return Resolved{}, usage(err)
	}
	maxDisk, err := pickMaxDisk(in, cfg)
	if err != nil {
		return Resolved{}, err
	}
	mode, policy, err := pickMode(in, resumed, cfg, s.Workspace)
	if err != nil {
		return Resolved{}, err
	}
	s = s.WithMode(mode)

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
	webSearch, err := pickWebSearch(cfg)
	if err != nil {
		return Resolved{}, err
	}

	return Resolved{
		Settings: s, MaxDisk: maxDisk, Instructions: !in.NoInstructions && cfg.InstructionsEnabled(),
		Sandbox: policy, Env: envPolicy, Compaction: compact, CompactPromptFile: promptFile, Approval: approvalPolicy, Rules: configured,
		ApprovalsReviewer: reviewer, Review: reviewCfg, Agents: agentSettings, WebSearch: webSearch, DefaultModel: defaulted,
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

// pickModel is the named model, empty when nothing names one. A
// provider flag that changes the provider drops the resumed and configured
// models, which belong to the other provider.
func pickModel(in Inputs, resumed session.Info, cfg config.Config) string {
	if !providerChanged(in, resumed, cfg) {
		return first(in.Model, resumed.Model, cfg.Model)
	}

	return in.Model
}

// providerChanged reports whether the provider flag picks another provider
// than the one that would apply without it: the resumed session's, the
// configured one, or the default.
func providerChanged(in Inputs, resumed session.Info, cfg config.Config) bool {
	return in.Provider != "" && in.Provider != first(resumed.Provider, cfg.Provider, CodexProvider)
}

// pickMaxAttempts is the max-attempts flag or its environment variable,
// else request_max_attempts, else uah's default.
func pickMaxAttempts(in Inputs, cfg config.Config) (int, error) {
	switch {
	case in.MaxAttempts < 0:
		return 0, usage(fmt.Errorf("invalid --max-attempts %d (want 1 or more)", in.MaxAttempts))
	case in.MaxAttempts > 0:
		return in.MaxAttempts, nil
	case cfg.RequestMaxAttempts < 0:
		return 0, usage(fmt.Errorf("invalid request_max_attempts %d (want 1 or more)", cfg.RequestMaxAttempts))
	case cfg.RequestMaxAttempts > 0:
		return cfg.RequestMaxAttempts, nil
	}

	return engine.DefaultMaxAttempts, nil
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
