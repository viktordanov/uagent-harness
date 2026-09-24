package embedded

import (
	"sync"

	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/tool"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/sandbox"
)

// modeCell is a run's permission mode. The Bash tool reads it for each
// command, the switcher for each model request, and the ask for each
// approval, so Run.SetMode applies from the next of each.
type modeCell struct {
	mu   sync.Mutex
	mode approval.Mode
}

// newModeCell starts with the run's mode, else the configured sandbox's.
func newModeCell(opts engine.Options, cfg Config) *modeCell {
	m := opts.Mode
	if m == "" && cfg.Sandbox != nil {
		m = approval.ModeFor(cfg.Sandbox.Mode)
	}

	return &modeCell{mode: m}
}

func (c *modeCell) get() approval.Mode {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.mode
}

func (c *modeCell) set(m approval.Mode) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mode = m
}

// SetMode changes the permission mode from the next command, model
// request, and approval.
func (a *agent) SetMode(m approval.Mode) error {
	select {
	case <-a.done:
		return errStopped
	default:
	}
	if _, err := approval.ParseMode(string(m)); err != nil {
		return err //nolint:wrapcheck // ParseMode names the value
	}
	a.mode.set(m)

	return nil
}

// sandboxRegistry offers the model Bash with the escalation arguments and a
// description of the sandbox of the run's mode at the start. The switcher
// keeps that definition in step with later mode changes (bashTools).
type sandboxRegistry struct {
	tool.Registry

	policy func() sandbox.Policy
}

func (r sandboxRegistry) StaticDefinitions() []tool.Definition {
	defs := r.Registry.StaticDefinitions()
	for i, d := range defs {
		if d.Tool.Name == tool.BashName {
			defs[i].Tool = bashFor(d.Tool, r.policy())
		}
	}

	return defs
}

// bashTools returns the rewrite the switcher applies to each model
// request: Bash as the current mode describes it, from base, the runner's
// definition.
func bashTools(base llm.Tool, policy func() sandbox.Policy) func([]llm.Tool) []llm.Tool {
	return func(tools []llm.Tool) []llm.Tool {
		for i, t := range tools {
			if t.Name == tool.BashName {
				out := append([]llm.Tool(nil), tools...)
				out[i] = bashFor(base, policy())

				return out
			}
		}

		return tools
	}
}

// bashFor is Bash as the policy describes it: with the escalation
// arguments and a note on the sandbox, or as the runner defines it in full
// access.
func bashFor(base llm.Tool, p sandbox.Policy) llm.Tool {
	if p.Mode == sandbox.FullAccess {
		return base
	}

	return bashWithEscalation(base, p)
}
