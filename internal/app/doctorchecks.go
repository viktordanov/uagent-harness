package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/viktordanov/uagent/core"
	"github.com/viktordanov/uagent/harness"

	"github.com/viktordanov/uagent-harness/internal/config"
	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/engine/embedded"
	"github.com/viktordanov/uagent-harness/internal/home"
	"github.com/viktordanov/uagent-harness/internal/hooks"
	"github.com/viktordanov/uagent-harness/internal/mcp"
	"github.com/viktordanov/uagent-harness/internal/sandbox"
	"github.com/viktordanov/uagent-harness/internal/store"
)

const sandboxProbeTimeout = 10 * time.Second

// checkRunner finds unreal-agent-runner, which only the process engine needs.
func checkRunner(engine, explicit string) Check {
	path, err := harness.FindRunner(explicit)
	switch {
	case err == nil:
		return ok("runner", path)
	case engine == EngineProcess:
		return fail("runner", err.Error(), "install unreal-agent-runner as the error says, or pass --runner")
	default:
		return ok("runner", "unreal-agent-runner not found; only --engine process needs it")
	}
}

// checkCredentials runs uagent's preflight for the provider (credentials,
// token expiry, and the workspace checks every run makes) and, for the
// embedded engine, builds the provider's client as a run would.
func checkCredentials(r Resolved, stateDir string, getenv func(string) string) []Check {
	s := r.Settings
	h := harness.New(harness.Config{StateDir: stateDir, Getenv: getenv, Logger: slog.New(slog.DiscardHandler)})
	findings, err := h.Preflight(core.Request{Workspace: s.Workspace, Provider: s.Provider, Model: s.Model})
	if err != nil {
		return []Check{fail("preflight", err.Error(), "check that the workspace and the state directory can be read")}
	}
	var auth, other []core.Finding
	for _, f := range findings {
		if strings.HasPrefix(f.Code, "auth_") {
			auth = append(auth, f)
		} else {
			other = append(other, f)
		}
	}
	name := "credentials"
	checks := []Check{workspaceCheck(other, s.Workspace, s.AllowDotenv)}
	switch {
	case len(auth) > 0 && auth[0].Severity == core.SeverityWarning:
		return append(checks, warn(name, auth[0].Message, "run `codex login`"))
	case len(auth) > 0:
		return append(checks, fail(name, auth[0].Message, credentialFix(s.Provider)))
	}
	if r.Engine == EngineEmbedded {
		if err := embedded.CheckCredentials(s.Provider, getenv); err != nil {
			return append(checks, fail(name, err.Error(), credentialFix(s.Provider)))
		}
	}
	if s.Provider == "ollama" {
		return append(checks, ok(name, "ollama needs none"))
	}

	return append(checks, ok(name, s.Provider+" credentials found and not expired"))
}

func credentialFix(provider string) string {
	if provider == CodexProvider {
		return "run `codex login` (or set OPENAI_CODEX_ACCESS_TOKEN)"
	}

	return "export the provider's API key variable, or UNREAL_HARNESS_LLM_API_KEY"
}

// workspaceCheck reports preflight's other findings: the workspace, the
// state directory's place, a risky .env, and a missing model.
func workspaceCheck(findings []core.Finding, workspace string, allowDotenv bool) Check {
	blocking, warnings := core.Triage(findings, allowDotenv)
	fixes := map[string]string{
		core.FindingWorkspaceMissing: "pass an existing directory with --workspace",
		core.FindingStateInWorkspace: "use a --state-dir outside the workspace",
		core.FindingDotenvRisky:      "remove those variables from the workspace .env, or pass --allow-dotenv",
		core.FindingModelMissing:     "pass --model or set model in config.toml",
	}
	switch {
	case len(blocking) > 0:
		return fail("workspace", core.Messages(blocking), fixes[blocking[0].Code])
	case len(warnings) > 0:
		return warn("workspace", core.Messages(warnings), fixes[warnings[0].Code])
	}

	return ok("workspace", workspace)
}

// checkSandbox runs `true` under the policy, as commands would run.
func checkSandbox(ctx context.Context, p sandbox.Policy) Check {
	if p.Mode == sandbox.FullAccess {
		return warn("sandbox", "danger-full-access: commands run without a sandbox", "use --sandbox workspace-write (or sandbox_mode) to sandbox them")
	}
	bin, err := exec.LookPath("true")
	if err != nil {
		return fail("sandbox", "cannot find `true` to test the sandbox: "+err.Error(), "check PATH")
	}
	argv, err := p.Wrap([]string{bin}) //nolint:contextcheck // on Linux, Wrap probes bwrap once per process, with its own timeout
	if errors.Is(err, sandbox.ErrUnavailable) {
		return warn("sandbox", "no sandbox is available on this system; commands run without one", "on Linux, install bubblewrap (bwrap)")
	}
	if err != nil {
		return fail("sandbox", err.Error(), "check the workspace and writable_roots")
	}
	ctx, cancel := context.WithTimeout(ctx, sandboxProbeTimeout)
	defer cancel()
	if out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).CombinedOutput(); err != nil {
		return fail("sandbox", fmt.Sprintf("a test command failed in the sandbox: %v %s", err, strings.TrimSpace(string(out))),
			"check that sandbox-exec (macOS) or bwrap (Linux) works here; bwrap needs unprivileged user namespaces")
	}

	return ok("sandbox", string(p.Mode)+": a test command ran in the sandbox")
}

// checkSystemPrompt reads model_instructions_file as a session would.
func checkSystemPrompt(cfg config.Config) Check {
	if cfg.ModelInstructionsFile == "" {
		return ok("system prompt", "the runner's host prompt")
	}
	text, err := readModelInstructions(cfg)
	if err != nil {
		return fail("system prompt", err.Error(), "fix model_instructions_file or the file it names")
	}

	return ok("system prompt", fmt.Sprintf("%s (%d bytes)", cfg.ModelInstructionsFile, len(text)))
}

// checkInstructions finds the instruction files a session would load.
func checkInstructions(r Resolved, cfg config.Config, workspace string) Check {
	if !r.Instructions {
		return ok("instructions", "turned off")
	}
	ev, _, err := loadInstructions(workspace, cfg)
	switch {
	case err != nil:
		return fail("instructions", err.Error(), "fix or remove the file named in the error")
	case ev == nil:
		return ok("instructions", "no AGENTS.md files found")
	case ev.Truncated:
		return warn("instructions", fmt.Sprintf("%s: cut at %d bytes", strings.Join(ev.Files, ", "), ev.Bytes), "raise project_doc_max_bytes or shorten the files")
	}

	return ok("instructions", strings.Join(ev.Files, ", "))
}

// checkHooks lists the configured hooks and each project hook that will not
// run until it is trusted.
func checkHooks(cfg config.Config, workspace, trustFile string) []Check {
	list, err := cfg.HookList()
	if err != nil {
		return []Check{fail("hooks", err.Error(), "fix the [[hooks.<Event>]] entry")}
	}
	if len(list) == 0 {
		return []Check{ok("hooks", "none configured")}
	}
	trust, err := hooks.LoadTrust(trustFile)
	if err != nil {
		return []Check{fail("hooks", err.Error(), "fix or remove "+trustFile+", then trust the project hooks again")}
	}
	runner, err := hooks.New(list, trust, workspace)
	if err != nil {
		return []Check{fail("hooks", err.Error(), "fix the [[hooks.<Event>]] entry")}
	}
	project := 0
	var untrusted []Check
	for _, h := range list {
		if h.Source != hooks.SourceProject {
			continue
		}
		project++
		if trusted, why := runner.TrustState(h); !trusted {
			untrusted = append(untrusted, warn("hook", fmt.Sprintf("%s project hook %q does not run: %s", h.Event, h.Command, why),
				"review it, then run `uah hooks trust -C "+workspace+"`"))
		}
	}
	summary := ok("hooks", fmt.Sprintf("%d configured: %d user, %d project (%d trusted)", len(list), len(list)-project, project, project-len(untrusted)))

	return append([]Check{summary}, untrusted...)
}

// checkMCP starts the configured MCP servers, each within its startup
// timeout, and reports how many tools each offers. A server that needs an
// OAuth login is a warning (a failure when it is required).
func checkMCP(ctx context.Context, cfg config.Config, caps engine.Capabilities, workspace string, stderr io.Writer) []Check {
	if len(cfg.MCPServers) == 0 {
		return []Check{ok("mcp", "no servers configured")}
	}
	if !caps.MCP {
		return []Check{warn("mcp", fmt.Sprintf("%d servers configured; this engine does not start them", len(cfg.MCPServers)), "use --engine embedded")}
	}
	m, err := mcpManager(cfg, workspace, slog.New(slog.NewTextHandler(stderr, nil)), "")
	if err != nil {
		return []Check{fail("mcp", err.Error(), "fix the [mcp_servers] entry")}
	}
	defer m.Close()
	_, _ = m.Tools(ctx) // waits for each server; a failed one is reported below
	checks := make([]Check, 0, len(cfg.MCPServers))
	for _, st := range m.Status() { //nolint:contextcheck // Tools already started them
		checks = append(checks, mcpCheck(st, cfg.MCPServers[st.Name]))
	}

	return checks
}

func mcpCheck(st mcp.ServerStatus, c mcp.ServerConfig) Check {
	name, timeout := "mcp "+st.Name, c.StartupTimeout()
	switch st.State {
	case mcp.StateReady:
		auth := ""
		if st.Transport == mcp.TransportHTTP {
			auth = ", auth " + st.Auth.Text()
		}

		return ok(name, fmt.Sprintf("started, %d tools%s (startup timeout %s)", len(st.Tools), auth, timeout))
	case mcp.StateDisabled:
		return ok(name, "disabled")
	case mcp.StateNeedsLogin:
		check := warn
		if c.Required {
			check = fail
		}

		return check(name, "needs login", "run `uah mcp login "+st.Name+"`")
	case mcp.StateStarting, mcp.StateFailed:
	}

	return fail(name, fmt.Sprintf("%s (startup timeout %s)", st.Error, timeout),
		"check its command or url; a slow server needs a larger startup_timeout_sec")
}

// checkState makes sure the state directory is writable and the session
// index opens.
func checkState(ctx context.Context, stateDir string) Check {
	fix := "use a writable --state-dir (or " + home.EnvStateDir + ")"
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fail("state", err.Error(), fix)
	}
	f, err := os.CreateTemp(stateDir, ".doctor-*")
	if err != nil {
		return fail("state", stateDir+" is not writable: "+err.Error(), fix)
	}
	_ = f.Close()
	_ = os.Remove(f.Name())
	ix, err := store.Open(ctx, stateDir)
	if err != nil {
		return fail("state", "the session index does not open: "+err.Error(), "remove "+stateDir+"/uah.db; uah rebuilds it from the run records")
	}
	defer ix.Close()
	infos, err := ix.Sessions(ctx)
	if err != nil {
		return fail("state", "the session index cannot be read: "+err.Error(), "remove "+stateDir+"/uah.db; uah rebuilds it from the run records")
	}

	return ok("state", fmt.Sprintf("%s is writable; the session index has %d sessions", stateDir, len(infos)))
}
