package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/viktordanov/uagent-harness/internal/config"
	"github.com/viktordanov/uagent-harness/internal/models"
	"github.com/viktordanov/uagent-harness/internal/session"
)

// CheckStatus is how a doctor check came out.
type CheckStatus string

const (
	CheckOK   CheckStatus = "ok"
	CheckWarn CheckStatus = "warn"
	CheckFail CheckStatus = "fail"
)

// Check is one line of `uah doctor`: what was checked, what was found, and
// for a warning or a failure, how to fix it.
type Check struct {
	Name   string      `json:"name"`
	Status CheckStatus `json:"status"`
	Detail string      `json:"detail"`
	Fix    string      `json:"fix,omitempty"`
}

func ok(name, detail string) Check { return Check{Name: name, Status: CheckOK, Detail: detail} }

func warn(name, detail, fix string) Check {
	return Check{Name: name, Status: CheckWarn, Detail: detail, Fix: fix}
}

func fail(name, detail, fix string) Check {
	return Check{Name: name, Status: CheckFail, Detail: detail, Fix: fix}
}

// Healthy reports whether no check failed.
func Healthy(checks []Check) bool {
	for _, c := range checks {
		if c.Status == CheckFail {
			return false
		}
	}

	return true
}

// DoctorOptions are what the checks read besides the inputs.
type DoctorOptions struct {
	// Getenv reads credentials (default os.Getenv).
	Getenv func(string) string
	// HookTrustFile is the trusted hooks file (default HookTrustFile()).
	HookTrustFile string
	// Stderr receives MCP servers' standard error (default discarded).
	Stderr io.Writer
	// Models is the model catalog (default NewModels for the settings).
	Models *models.Manager
}

// Doctor checks what a session in the workspace would need: the
// configuration, the runner, credentials, the model list, the sandbox, instructions, hooks,
// MCP servers, and the state directory. It starts no session and calls no
// model; it runs `true` in the sandbox and starts the MCP servers.
func Doctor(ctx context.Context, in Inputs, opts DoctorOptions) []Check {
	if opts.Getenv == nil {
		opts.Getenv = os.Getenv
	}
	if opts.HookTrustFile == "" {
		opts.HookTrustFile = HookTrustFile()
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	stateDir, err := filepath.Abs(in.StateDir)
	if err == nil {
		in.Workspace, err = filepath.Abs(workspaceFor(in, session.Info{}))
	}
	if err != nil {
		return []Check{fail("paths", err.Error(), "check --state-dir and --workspace")}
	}

	cfg, checks := checkConfig(in)
	r, err := Resolve(in, session.Info{}, cfg)
	if err != nil {
		checks = append(checks, fail("settings", err.Error(), "fix the flag or the configuration value it names"))
		if r, err = Resolve(in, session.Info{}, config.Config{}); err != nil {
			return checks // the flags themselves are invalid
		}
		cfg = config.Config{}
	}
	r.Sandbox = absPolicy(r.Sandbox, in.Workspace)
	caps := engineCapabilities(r, in.Gate)
	checks = append(checks, checkRunner(r.Engine, in.Runner), checkEngine(r, cfg, in.Workspace, caps))
	checks = append(checks, checkCredentials(r, stateDir, opts.Getenv)...)
	if opts.Models == nil {
		opts.Models = NewModels(stateDir, r.Settings, opts.Getenv)
	}
	checks = append(checks, checkModels(ctx, opts.Models, r.Settings.Model), checkSandbox(ctx, r.Sandbox), checkInstructions(r, cfg, in.Workspace))
	checks = append(checks, checkHooks(cfg, in.Workspace, opts.HookTrustFile)...)
	checks = append(checks, checkMCP(ctx, cfg, caps, in.Workspace, opts.Stderr)...)

	return append(checks, checkState(ctx, stateDir))
}

// checkConfig loads the user and project files and reports which were read
// and whether the project file is ignored because the workspace is not
// trusted.
func checkConfig(in Inputs) (config.Config, []Check) {
	cfg, loaded, err := config.Load(in.ConfigPath, in.Workspace)
	if err != nil {
		return config.Config{}, []Check{fail("config", err.Error(), "fix the file named in the error")}
	}
	detail := "no configuration files; using the defaults"
	if len(loaded) > 0 {
		detail = "read " + strings.Join(loaded, ", ")
	}
	checks := []Check{ok("config", detail)}
	project := config.ProjectFile(in.Workspace)
	if _, err := os.Stat(project); err == nil && !cfg.Projects[in.Workspace].Trusted {
		checks = append(checks, warn("project config", project+" is ignored: the workspace is not trusted",
			fmt.Sprintf("to use it, add [projects.%q] with trusted = true to %s", in.Workspace, in.ConfigPath)))
	}

	return cfg, checks
}
