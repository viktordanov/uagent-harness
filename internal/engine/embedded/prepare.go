package embedded

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/google/uuid"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/contextprep"
	"github.com/viktordanov/uah/internal/sandbox"
	"github.com/viktordanov/uah/internal/session"
)

// Context preparation: a new session, a subagent's included, starts with
// one more user message before its first, the prepared context
// (internal/contextprep) that the adapters gather from the session's
// facts. The system prompt stays the same, byte for byte, and the message
// is recorded with the session, so the prompt cache holds. Resumed and
// forked sessions get none: theirs is in their history.

// prepared adds the prepared context before the messages of a new session,
// when context preparation is on. req has the session's ID.
func (w *wiring) prepared(ctx context.Context, req core.Request, messages []core.UserInput) []core.UserInput {
	if !w.e.cfg.ContextPreparation {
		return messages
	}
	_, err := os.Stat(filepath.Join(w.l.SessionsDir, req.SessionID+".session.jsonl"))
	if !errors.Is(err, fs.ErrNotExist) {
		return messages // resumed or forked
	}
	text := contextprep.Prepare(ctx, w.facts(req), w.adapters(req)...) //nolint:contextcheck,nolintlint // on Linux, the sandbox probes bwrap once per process, with its own timeout; not on darwin
	if text == "" {
		return messages
	}

	return append([]core.UserInput{{ID: uuid.NewString(), Text: text}}, messages...)
}

// facts are what the adapters know about the session: its workspace and
// system prompt, the shell its commands run in, and the sandbox of its
// permission mode when it starts.
func (w *wiring) facts(req core.Request) contextprep.Facts {
	f := contextprep.Facts{
		Workspace: req.Workspace, SystemPrompt: req.SystemPrompt, Shell: w.shell(), GOOS: runtime.GOOS,
		Subagent: strings.HasPrefix(req.SessionID, session.SubagentIDPrefix),
	}
	if w.e.cfg.Sandbox == nil {
		return f
	}
	mode := w.mode.get().Sandbox()
	p := w.policy(req, mode)
	if mode == sandbox.FullAccess {
		return f
	}
	if _, err := p.Wrap([]string{f.Shell}); err != nil {
		return f // no sandbox on this system: commands ask instead
	}
	f.Sandbox = contextprep.Sandbox{Mode: string(mode), Network: p.Network}

	return f
}

// adapters are the blocks of the prepared context, in order.
func (w *wiring) adapters(req core.Request) []contextprep.Adapter {
	skills, _ := discoverSkills(req.Workspace, w.getenv) // tools reports their errors
	files := contextprep.AgentFiles{}
	for _, s := range skills {
		files.Skills = append(files.Skills, contextprep.Skill{Name: s.Name, Description: s.Description, Path: s.Path})
	}

	return []contextprep.Adapter{contextprep.Workspace{}, files, contextprep.Harness{}}
}

// shell is the user's shell, which commands run in: $SHELL, or /bin/sh.
func (w *wiring) shell() string {
	if shell := strings.TrimSpace(w.getenv("SHELL")); shell != "" {
		return shell
	}

	return "/bin/sh"
}
