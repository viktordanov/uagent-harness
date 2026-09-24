package embedded

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/unreallabsai/unreal-agent/harness/session"
	"github.com/unreallabsai/unreal-agent/harness/tool"
	"github.com/unreallabsai/unreal-agent/harness/tool/bash"
	"github.com/unreallabsai/unreal-agent/harness/tool/viewimage"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/mcp"
	"github.com/viktordanov/uagent-harness/internal/sandbox"
)

// tools builds the registry the coordinator runs: Bash, ViewImage, and
// workspace skills, as the runner registers them, and MCP tools, with
// PreToolUse hooks around them. Its static definitions are the tools the model is offered,
// so a tool added or changed here reaches both.
func (w *wiring) tools(ctx context.Context, req core.Request, sessionID session.ID) (tool.Registry, error) {
	translators, err := w.translators(req, sessionID) //nolint:contextcheck // on Linux, the sandbox probes bwrap once per process, with its own timeout
	if err != nil {
		return nil, err
	}
	b, sandboxed := translators.Bash.(sandboxedBash)
	if sandboxed {
		b.ctx = ctx // approvals wait on the run
		translators.Bash = b
	}
	skills, skillErrs := discoverSkills(req.Workspace, w.getenv)
	registry := tool.NewRegistry(translators, toolNames(req, len(skills) > 0)...)
	if sandboxed && b.canEscalate() {
		registry = sandboxRegistry{Registry: registry, policy: w.policy(req, b.mode)}
	}
	if err := registerSkills(registry, skills); err != nil {
		return nil, err
	}
	for _, err := range skillErrs {
		_, _ = fmt.Fprintf(w.l.Stderr, "skill error> %s\n", err)
	}
	mcpTools, err := w.mcpTools(ctx)
	if err != nil {
		return nil, err
	}
	never := w.e.cfg.Approver != nil && w.e.cfg.Approver.Policy() == approval.Never
	registry = withMCP(registry, mcpTools, req.DisallowedTools, mcpGate{ctx: ctx, ask: w.ask, never: never})
	req.SessionID = string(sessionID)
	registry = w.withAgents(registry, req)

	return withPreToolUse(ctx, registry, w.e.cfg.Hooks, req, w.l.SessionsDir), nil
}

// withAgents attaches the run to the subagents and adds the agent tools.
func (w *wiring) withAgents(registry tool.Registry, req core.Request) tool.Registry {
	a := w.e.cfg.Subagents
	if a == nil {
		return withAgents(registry, false, nil, req.DisallowedTools)
	}
	emit := w.notify
	if emit == nil {
		emit = w.emit
	}
	if emit == nil {
		emit = func(core.Event) {}
	}
	offer := a.Attach(engine.AgentParent{SessionID: req.SessionID, Request: req, Ask: w.userAsk, Emit: emit})

	return withAgents(registry, offer, a.Roles(), req.DisallowedTools)
}

// translators returns the built-in tools. Bash runs in the workspace with
// the user's shell and keeps its operation output under the session's
// operation directory.
func (w *wiring) translators(req core.Request, sessionID session.ID) (tool.StaticTranslators, error) {
	opsDir := filepath.Join(w.l.SessionsDir, "operations", string(sessionID))
	if err := os.MkdirAll(opsDir, 0o700); err != nil {
		return tool.StaticTranslators{}, fmt.Errorf("failed to create the operation directory: %w", err)
	}
	shell := strings.TrimSpace(w.getenv("SHELL"))
	if shell == "" {
		shell = "/bin/sh"
	}
	run := bash.New(bash.Config{Shell: shell, Directory: req.Workspace, BaseDirectory: opsDir})
	if w.e.cfg.Sandbox != nil {
		var err error
		if run, err = w.sandboxedBash(req, opsDir, shell); err != nil {
			return tool.StaticTranslators{}, err
		}
	}

	return tool.StaticTranslators{Bash: run, ViewImage: viewimage.New(viewimage.Config{Directory: req.Workspace})}, nil
}

// mcpTools starts the MCP servers on the first run and returns their
// tools; a server that fails to start is reported and left out.
func (w *wiring) mcpTools(ctx context.Context) ([]mcp.Tool, error) {
	m := w.e.cfg.MCP
	if m == nil {
		return nil, nil
	}
	tools, err := m.Tools(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start MCP servers: %w", err)
	}
	for _, s := range m.Status() { //nolint:contextcheck // started above; servers outlive the run
		if s.State == mcp.StateFailed || s.State == mcp.StateNeedsLogin {
			_, _ = fmt.Fprintf(w.l.Stderr, "mcp> %s: %s\n", s.Name, s.Error)
		}
	}

	return tools, nil
}

// policy is the configured sandbox policy for the request's workspace.
func (w *wiring) policy(req core.Request, mode sandbox.Mode) sandbox.Policy {
	p := *w.e.cfg.Sandbox
	p.Mode, p.Workspace = mode, req.Workspace

	return p
}

// toolNames returns the tools to enable: Bash, ViewImage, and SkillUse when
// the workspace has skills, less the request's disallowed tools.
func toolNames(req core.Request, hasSkills bool) []string {
	names := []string{tool.BashName, tool.ViewImageName}
	if hasSkills {
		names = append(names, tool.SkillUseName)
	}

	return slices.DeleteFunc(names, func(n string) bool { return slices.Contains(req.DisallowedTools, n) })
}

// registerSkills registers the skills when SkillUse is enabled.
func registerSkills(registry tool.Registry, skills []tool.Skill) error {
	if _, ok := registry.Resolve(tool.SkillUseName); !ok {
		return nil
	}
	for _, s := range skills {
		if _, err := registry.RegisterSkill(s); err != nil {
			return fmt.Errorf("failed to register skill %q: %w", s.Path, err)
		}
	}

	return nil
}
