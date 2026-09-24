package agents

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/hooks"
	"github.com/viktordanov/uagent-harness/internal/instructions"
	"github.com/viktordanov/uagent-harness/internal/session"
)

// Defaults, Codex's.
const (
	DefaultMaxThreads = 4
	DefaultMaxDepth   = 1
)

// Config configures the subagents of one uah process.
type Config struct {
	// MaxThreads is how many children a session tree keeps open at once.
	MaxThreads int
	// MaxDepth is how deep children nest: 1 means children cannot spawn,
	// and 0 offers no tools at all (subagents are off), while past calls
	// still get an answer.
	MaxDepth int
	// Model and Effort are the configured defaults for children.
	Model  string
	Effort string
	Roles  []Role
	// SessionsDir holds the children's sidecars and records.
	SessionsDir string
	// Base carries what a run's request does not: the service tier, the
	// sandbox mode for display, and the context window.
	Base session.Settings
	// Hooks, when set, runs SubagentStop hooks when a child finishes, and
	// the children's PostToolUse hooks.
	Hooks *hooks.Runner
}

// Manager implements engine.Subagents. Children are ordinary sessions on the
// parent's engine, recorded with SourceSubagent and their parent's ID.
type Manager struct {
	cfg Config

	mu       sync.Mutex
	eng      engine.Engine
	parents  map[string]engine.AgentParent
	children map[string]*child
	// changed is closed and replaced whenever a child's status changes.
	changed chan struct{}
	// emitMu keeps a child's updates in the order their states were taken.
	emitMu sync.Mutex
}

// New returns a manager; Bind gives it the engine children run on.
func New(cfg Config) *Manager {
	if cfg.MaxThreads <= 0 {
		cfg.MaxThreads = DefaultMaxThreads
	}

	return &Manager{
		cfg:     cfg,
		parents: map[string]engine.AgentParent{}, children: map[string]*child{}, changed: make(chan struct{}),
	}
}

// Bind sets the engine children run on: the parent's.
func (m *Manager) Bind(eng engine.Engine) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eng = childEngine{eng}
}

// childEngine hides the engine's Close, so closing a child leaves the
// shared MCP servers running.
type childEngine struct{ engine.Engine }

// Attach records the parent's run and offers the tools when the session
// may spawn: its depth is below MaxDepth.
func (m *Manager) Attach(p engine.AgentParent) []engine.AgentTool {
	m.mu.Lock()
	m.parents[p.SessionID] = p
	offer := m.depth(p.SessionID) < m.cfg.MaxDepth
	m.mu.Unlock()
	if !offer {
		return nil
	}

	return m.definitions()
}

// depth is how many ancestors a session has, from the live children and
// then the sidecars, so a resumed child keeps its depth. It holds m.mu.
func (m *Manager) depth(id string) int {
	n := 0
	for range 32 {
		parent := ""
		if c, ok := m.children[id]; ok {
			parent = c.parent
		} else if sc, found, err := session.ReadSidecar(m.cfg.SessionsDir, id); err == nil && found && sc.Source == session.SourceSubagent {
			parent = sc.Parent
		}
		if parent == "" {
			break
		}
		n, id = n+1, parent
	}

	return n
}

// root is the top session of id's tree. It holds m.mu.
func (m *Manager) root(id string) string {
	for c, ok := m.children[id]; ok; c, ok = m.children[id] {
		id = c.parent
	}

	return id
}

// openIn counts the open children in root's tree. It holds m.mu.
func (m *Manager) openIn(root string) int {
	n := 0
	for id, c := range m.children {
		if !c.closed && m.root(id) == root {
			n++
		}
	}

	return n
}

// role finds an agent type; "" and "default" are the default agent.
func (m *Manager) role(name string) (Role, error) {
	if name == "" || name == "default" {
		return Role{}, nil
	}
	i := slices.IndexFunc(m.cfg.Roles, func(r Role) bool { return r.Name == name })
	if i < 0 {
		names := []string{"default"}
		for _, r := range m.cfg.Roles {
			names = append(names, r.Name)
		}

		return Role{}, fmt.Errorf("unknown agent_type %q (want one of %s)", name, strings.Join(names, ", "))
	}

	return m.cfg.Roles[i], nil
}

// settings are a child's: the parent run's, then the configured defaults,
// the role's, and the call's model and effort, and the role's instructions
// after the host prompt.
func (m *Manager) settings(p engine.AgentParent, role Role, model, effort string) session.Settings {
	r := p.Request
	s := m.cfg.Base
	s.Provider, s.Workspace, s.BaseURL, s.Timeout, s.AllowDotenv = r.Provider, r.Workspace, r.BaseURL, r.Timeout, r.AllowDotenv
	s.Model = first(model, role.Model, m.cfg.Model, r.Model)
	s.Effort = first(effort, role.Effort, m.cfg.Effort, r.Effort)
	s.SystemPrompt = r.SystemPrompt
	if role.DeveloperInstructions != "" {
		s.SystemPrompt = first(s.SystemPrompt, instructions.RunnerHostPrompt) + "\n\n" + strings.TrimSpace(role.DeveloperInstructions)
	}

	return s
}

// subtree is c and its open descendants, parents first. It holds m.mu.
func (m *Manager) subtree(c *child) []*child {
	out := []*child{c}
	for i := 0; i < len(out); i++ {
		for _, k := range m.children {
			if k.parent == out[i].id && !k.closed {
				out = append(out, k)
			}
		}
	}

	return out
}

// Interrupt stops the live runs of the parent's children and their own
// children. They stay open: send_input starts them again.
func (m *Manager) Interrupt(parentID string) {
	m.mu.Lock()
	var stop []*child
	for _, c := range m.children {
		if c.parent == parentID && !c.closed {
			stop = append(stop, m.subtree(c)...)
		}
	}
	for _, c := range stop {
		c.cancelAsks()
	}
	m.mu.Unlock()
	for _, c := range stop {
		if s := c.session(m); s != nil {
			go func() { _ = s.Interrupt() }() // never wait on a child's loop from the parent's
		}
	}
}

// Close closes every child; the engine calls it when a session on it
// closes. The manager stays usable for the engine's next session.
func (m *Manager) Close() error {
	m.mu.Lock()
	var open []*child
	for _, c := range m.children {
		if !c.closed {
			c.closed = true
			c.cancelAsks()
			open = append(open, c)
		}
	}
	m.mu.Unlock()
	var errs []error
	for _, c := range open {
		if s := c.session(m); s != nil {
			if err := s.Close(); err != nil && !errors.Is(err, session.ErrClosed) {
				errs = append(errs, fmt.Errorf("failed to close agent %s: %w", c.nickname, err))
			}
		}
	}

	return errors.Join(errs...)
}

func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}

	return ""
}
