package app

import (
	"log/slog"
	"path/filepath"

	"github.com/viktordanov/uagent/harness"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/engine/process"
	"github.com/viktordanov/uagent-harness/internal/sandbox"
	"github.com/viktordanov/uagent-harness/internal/session"
)

// newProcess builds the process engine. The runner runs each command with
// $SHELL, so each permission mode's sandbox gets a shell that applies the
// command rules (with p.gate) and the sandbox without changing the runner.
func newProcess(r Resolved, runnerPath, stateDir string, logger *slog.Logger, p parts, opts *session.Options) (engine.Engine, error) {
	runner, err := harness.FindRunner(runnerPath)
	if err != nil {
		return nil, usage(err)
	}
	shells := process.Shells{
		Dir: filepath.Join(stateDir, "sandbox"), Policy: r.Sandbox, Env: r.Env, Real: RealShell(),
		Gate: p.gate, Rules: p.approver.Rules(), Approval: p.approver.Policy(),
	}
	unavailable := false
	eng, err := process.NewSandboxed(r.Sandbox.Mode, p.gate != "", func(mode sandbox.Mode) (harness.Config, error) {
		shell, none, err := shells.For(mode)
		if err != nil {
			return harness.Config{}, err
		}
		unavailable = unavailable || none
		backend := harness.RunnerBackend{Path: runner, Env: []string{"SHELL=" + shell}}

		return harness.Config{Backend: backend, StateDir: stateDir, MaxDisk: r.MaxDisk, Logger: logger}, nil
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // the sandbox's errors name the file
	}
	if unavailable {
		opts.Notices = append(opts.Notices, "no sandbox is available on this system; commands run without one")
	}

	return eng, nil
}
