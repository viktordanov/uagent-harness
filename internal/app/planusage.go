package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/viktordanov/uagent-harness/internal/config"
	"github.com/viktordanov/uagent-harness/internal/session"
	planusage "github.com/viktordanov/uagent-harness/internal/usage"
)

// NewUsage is the usage reader for the settings' provider and base URL:
// the ChatGPT subscription's for openai-codex, else one that says usage is
// not available.
func NewUsage(s session.Settings, getenv func(string) string) planusage.Reader {
	return planusage.For(s.Provider, planusage.ReaderOptions{Getenv: getenv, BaseURL: s.BaseURL})
}

// UsageReport is what `uah usage` prints.
type UsageReport struct {
	Provider string
	Snapshot planusage.Snapshot
}

// ReadUsage resolves the provider as a session with these inputs would and
// reads its usage now. For a provider without usage the error wraps
// planusage.ErrUnsupported.
func ReadUsage(ctx context.Context, in Inputs, getenv func(string) string) (UsageReport, error) {
	_, r, err := resolveOnly(in)
	if err != nil {
		return UsageReport{}, err
	}
	s, err := NewUsage(r.Settings, getenv).Usage(ctx, 0)
	if err != nil {
		return UsageReport{Provider: r.Settings.Provider}, err //nolint:wrapcheck // the reader's errors say what failed
	}

	return UsageReport{Provider: r.Settings.Provider, Snapshot: s}, nil
}

// resolveOnly resolves the inputs as a new session would, without building
// anything: the absolute state directory and the settings.
func resolveOnly(in Inputs) (string, Resolved, error) {
	stateDir, err := filepath.Abs(in.StateDir)
	if err != nil {
		return "", Resolved{}, fmt.Errorf("failed to resolve state dir: %w", err)
	}
	if in.Workspace, err = filepath.Abs(workspaceFor(in, session.Info{})); err != nil {
		return "", Resolved{}, fmt.Errorf("failed to resolve workspace: %w", err)
	}
	cfg, _, err := config.Load(in.ConfigPath, in.Workspace)
	if err != nil {
		return "", Resolved{}, usage(err)
	}
	r, err := Resolve(in, session.Info{}, cfg)

	return stateDir, r, err
}

// usageWarnAt is the percent used from which the doctor warns.
const usageWarnAt = 90

// checkUsage reads the subscription's usage. It reports false, for no
// check, when the provider has none.
func checkUsage(ctx context.Context, r planusage.Reader, now time.Time) (Check, bool) {
	const name = "usage"
	s, err := r.Usage(ctx, 0)
	switch {
	case errors.Is(err, planusage.ErrUnsupported):
		return Check{}, false
	case errors.Is(err, planusage.ErrUnauthorized):
		return fail(name, err.Error(), "run `codex login`"), true
	case err != nil:
		return warn(name, "could not read the usage: "+err.Error(), "check the network; https://chatgpt.com/codex/settings/usage shows it too"), true
	}
	detail := s.Plan
	if detail == "" {
		detail = "unknown plan"
	}
	tight, found := s.Tightest()
	if !found {
		return ok(name, detail+" · no usage windows"), true
	}
	detail += " · " + tight.Text(now)
	switch {
	case s.Reached():
		return fail(name, detail+"; the usage limit is reached", "wait for the reset, or add credits at https://chatgpt.com/codex/settings/usage"), true
	case tight.Window.UsedPercent >= usageWarnAt:
		return warn(name, detail, "the limit is close; see https://chatgpt.com/codex/settings/usage"), true
	}

	return ok(name, detail), true
}
