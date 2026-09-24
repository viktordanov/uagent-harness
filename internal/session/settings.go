package session

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/approval"
)

// Efforts are the thinking levels the runner accepts.
var Efforts = []string{"low", "medium", "high", "xhigh", "max"}

// Providers are the backends the runner supports.
var Providers = []string{"openai", "openai-codex", "openrouter", "fireworks", "ollama"}

// Settings are what a session sends with every run. Changing them applies
// live when the engine supports it, and otherwise from the next run.
type Settings struct {
	Provider    string
	Model       string
	Effort      string
	ServiceTier string // "" or "priority"
	Workspace   string
	BaseURL     string
	Timeout     time.Duration
	AllowDotenv bool
	// SystemPrompt replaces the runner's host prompt when set.
	SystemPrompt string
	// Mode is the permission mode: the sandbox commands run in and who
	// decides what needs approval ("": the engine's configured sandbox).
	// Change it with WithMode, which keeps Sandbox in step.
	Mode approval.Mode
	// Sandbox is Mode's sandbox mode, for display.
	Sandbox string
	// ContextWindow overrides the model table's context window (tokens), for
	// the context meter; the engine was built with the same value.
	ContextWindow int64
}

// Validate checks values without asking the provider which models exist.
func (s Settings) Validate() error {
	if !slices.Contains(Providers, s.Provider) {
		return fmt.Errorf("invalid provider %q (want %s)", s.Provider, strings.Join(Providers, ", "))
	}
	if s.Effort != "" && !slices.Contains(Efforts, s.Effort) {
		return fmt.Errorf("invalid effort %q (want %s)", s.Effort, strings.Join(Efforts, ", "))
	}
	if err := validateModel(s.Model); err != nil {
		return err
	}
	if s.ServiceTier != "" && s.ServiceTier != "priority" {
		return fmt.Errorf("invalid service tier %q (want priority or empty)", s.ServiceTier)
	}
	if s.Workspace == "" {
		return errors.New("the workspace is not set")
	}
	if s.Mode != "" {
		if _, err := approval.ParseMode(string(s.Mode)); err != nil {
			return err
		}
	}

	return nil
}

// WithMode returns the settings with the permission mode m and its sandbox
// mode.
func (s Settings) WithMode(m approval.Mode) Settings {
	s.Mode, s.Sandbox = m, string(m.Sandbox())

	return s
}

// validateModel checks syntax only; the provider decides which models exist.
func validateModel(model string) error {
	if strings.HasPrefix(model, "-") {
		return fmt.Errorf("invalid model %q: it starts with a dash", model)
	}
	if strings.IndexFunc(model, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 {
		return fmt.Errorf("invalid model %q: it contains spaces or control characters", model)
	}

	return nil
}

// request builds the runner request for one run of the session.
func (s Settings) request(sessionID string, messages []core.UserInput) core.Request {
	return core.Request{
		SessionID:    sessionID,
		Messages:     messages,
		Provider:     s.Provider,
		Model:        s.Model,
		Effort:       s.Effort,
		BaseURL:      s.BaseURL,
		SystemPrompt: s.SystemPrompt,
		Workspace:    s.Workspace,
		Timeout:      s.Timeout,
		AllowDotenv:  s.AllowDotenv,
	}
}

// WithRequest returns the settings with the fields a run's request carries
// taken from req: the inverse of request, so a subagent starts from its
// parent's run as it is.
func (s Settings) WithRequest(req core.Request) Settings {
	s.Provider, s.Model, s.Effort, s.BaseURL = req.Provider, req.Model, req.Effort, req.BaseURL
	s.SystemPrompt, s.Workspace, s.Timeout, s.AllowDotenv = req.SystemPrompt, req.Workspace, req.Timeout, req.AllowDotenv

	return s
}
