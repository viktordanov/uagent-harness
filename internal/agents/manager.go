package agents

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/engine"
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
	// MaxDepth is how deep children nest: 1 means children cannot spawn.
	MaxDepth int
	// Model and Effort are the configured defaults for children.
	Model  string
	Effort string
	Roles  []Role
	// SessionsDir holds the children's sidecars.
	SessionsDir string
	// Base carries what a run's request does not: the service tier, the
	// sandbox mode for display, and the context window.
	Base session.Settings
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

func (m *Manager) Attach(p engine.AgentParent) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.parents[p.SessionID] = p

	return m.depth(p.SessionID) < m.cfg.MaxDepth
}

func (m *Manager) Roles() []engine.AgentRole {
	out := make([]engine.AgentRole, 0, len(m.cfg.Roles))
	for _, r := range m.cfg.Roles {
		out = append(out, engine.AgentRole{Name: r.Name, Description: r.Description})
	}

	return out
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

func (m *Manager) Spawn(_ context.Context, parentID string, req engine.SpawnRequest) (engine.AgentRef, error) {
	c, parent, err := m.reserve(parentID, req.AgentType)
	if err != nil {
		return engine.AgentRef{}, err
	}
	role, _ := m.role(req.AgentType)
	// Children outlive the call that spawned them; Close stops them.
	s, err := session.Open(context.Background(), m.eng, session.Options{ //nolint:contextcheck // children outlive the spawning call
		ID: c.id, Settings: m.settings(parent, role, req), SessionsDir: m.cfg.SessionsDir,
		Source: session.SourceSubagent, Parent: parentID, Ask: m.askFor(parentID, c.nickname),
	})
	if err != nil {
		m.mu.Lock()
		delete(m.children, c.id)
		m.mu.Unlock()

		return engine.AgentRef{}, fmt.Errorf("failed to start the agent: %w", err)
	}
	m.mu.Lock()
	c.s = s
	m.mu.Unlock()
	go m.watch(c)
	if err := m.submit(c, req.Message); err != nil {
		return engine.AgentRef{}, err
	}

	return engine.AgentRef{ID: c.id, Nickname: c.nickname}, nil
}

// reserve checks the parent, the role, and the limit, and registers a new
// child without a session.
func (m *Manager) reserve(parentID, agentType string) (*child, engine.AgentParent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	parent, ok := m.parents[parentID]
	if !ok || m.eng == nil {
		return nil, parent, errors.New("subagents are not available in this session")
	}
	role, err := m.role(agentType)
	if err != nil {
		return nil, parent, err
	}
	if open := m.openIn(m.root(parentID)); open >= m.cfg.MaxThreads {
		return nil, parent, fmt.Errorf("agent limit reached: %d agents are open; close one with close_agent first", open)
	}
	c := &child{
		id: uuid.NewString(), parent: parentID, role: role.Name, nickname: m.nickname(parentID, role),
		started: time.Now(), status: engine.AgentStatus{State: engine.AgentRunning},
	}
	m.children[c.id] = c

	return c, parent, nil
}

// role finds an agent type; "" and "default" are the default agent. It
// holds m.mu.
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

// settings are a child's: the parent run's, then the configured defaults,
// the role's, and the call's model and effort, and the role's instructions
// after the host prompt.
func (m *Manager) settings(p engine.AgentParent, role Role, req engine.SpawnRequest) session.Settings {
	r := p.Request
	s := m.cfg.Base
	s.Provider, s.Workspace, s.BaseURL, s.Timeout, s.AllowDotenv = r.Provider, r.Workspace, r.BaseURL, r.Timeout, r.AllowDotenv
	s.Model = first(req.Model, role.Model, m.cfg.Model, r.Model)
	s.Effort = first(req.Effort, role.Effort, m.cfg.Effort, r.Effort)
	s.SystemPrompt = r.SystemPrompt
	if role.DeveloperInstructions != "" {
		s.SystemPrompt = first(s.SystemPrompt, instructions.RunnerHostPrompt) + "\n\n" + strings.TrimSpace(role.DeveloperInstructions)
	}

	return s
}

// askFor asks a child's approvals through its parent's current run, with
// the child's nickname in the justification.
func (m *Manager) askFor(parentID, nickname string) approval.Ask {
	return func(ctx context.Context, p approval.Prompt) approval.Answer {
		m.mu.Lock()
		ask := m.parents[parentID].Ask
		m.mu.Unlock()
		if ask == nil {
			return approval.DeclineBecause("no one can approve commands of agent " + nickname + " in this run")
		}
		p.Justification = strings.TrimSpace("agent " + nickname + ": " + p.Justification)

		return ask(ctx, p)
	}
}

func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}

	return ""
}
