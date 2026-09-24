package app

import (
	"fmt"
	"path/filepath"
	"slices"

	"github.com/viktordanov/uagent-harness/internal/agents"
	"github.com/viktordanov/uagent-harness/internal/config"
	"github.com/viktordanov/uagent-harness/internal/session"
)

// Agents are the resolved [agents] settings.
type Agents struct {
	Enabled bool
	// MaxThreads is how many subagents a session keeps open at once.
	MaxThreads int
	// MaxDepth is how deep subagents nest (1: children cannot spawn).
	MaxDepth int
	// Model and Effort are the subagents' defaults ("": the parent's).
	Model  string
	Effort string
}

// pickAgents checks the [agents] keys and applies Codex's defaults.
func pickAgents(c config.Agents) (Agents, error) {
	a := Agents{
		Enabled: c.Enabled == nil || *c.Enabled, MaxThreads: agents.DefaultMaxThreads, MaxDepth: agents.DefaultMaxDepth,
		Model: c.DefaultSubagentModel, Effort: c.DefaultSubagentReasoningEffort,
	}
	if n := c.MaxThreadsValue(); n != nil {
		if *n < 1 {
			return Agents{}, usage(fmt.Errorf("invalid agents.max_concurrent_threads_per_session %d (want 1 or more)", *n))
		}
		a.MaxThreads = *n
	}
	if c.MaxDepth != nil {
		if *c.MaxDepth < 0 {
			return Agents{}, usage(fmt.Errorf("invalid agents.max_depth %d (want 0 or more)", *c.MaxDepth))
		}
		a.MaxDepth = *c.MaxDepth
	}
	if a.Effort != "" && !slices.Contains(session.Efforts, a.Effort) {
		return Agents{}, usage(fmt.Errorf("invalid agents.default_subagent_reasoning_effort %q", a.Effort))
	}

	return a, nil
}

// newAgents builds the subagent manager with the user's and, in a trusted
// workspace, the project's role files; it is nil when agents are off or
// the engine is the process engine, which cannot run them.
// Role file warnings become notices.
func newAgents(r Resolved, cfg config.Config, workspace, stateDir string, opts *session.Options) *agents.Manager {
	if !r.Agents.Enabled || r.Agents.MaxDepth == 0 || r.Engine != EngineEmbedded {
		return nil
	}
	dirs := []string{config.AgentsDir()}
	if cfg.Projects[workspace].Trusted {
		dirs = append(dirs, config.ProjectAgentsDir(workspace))
	}
	roles, warnings := agents.LoadRoles(dirs...)
	opts.Notices = append(opts.Notices, warnings...)

	return agents.New(agents.Config{
		MaxThreads: r.Agents.MaxThreads, MaxDepth: r.Agents.MaxDepth, Model: r.Agents.Model, Effort: r.Agents.Effort,
		Roles: roles, SessionsDir: filepath.Join(stateDir, "sessions"), Base: r.Settings,
	})
}
