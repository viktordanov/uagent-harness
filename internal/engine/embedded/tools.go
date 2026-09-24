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

	"github.com/viktordanov/uagent-harness/internal/sandbox"
)

// tools builds the registry the coordinator runs: Bash, ViewImage, and
// workspace skills, as the runner registers them, with PreToolUse hooks
// around them. Its static definitions are the tools the model is offered,
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
	skills, skillErrs := tool.DiscoverSkills(filepath.Join(req.Workspace, ".harness", "skills"))
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
	req.SessionID = string(sessionID)

	return withPreToolUse(ctx, registry, w.e.cfg.Hooks, req, w.l.SessionsDir), nil
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
