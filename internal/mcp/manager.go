package mcp

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// State is where a server is in its lifecycle.
type State string

const (
	StateStarting State = "starting"
	StateReady    State = "ready"
	StateFailed   State = "failed"
	StateDisabled State = "disabled"
)

// Options configure a Manager.
type Options struct {
	// Workspace is a stdio server's directory unless it sets cwd.
	Workspace string
	// Getenv reads the variables servers get (default os.Getenv).
	Getenv func(string) string
	// Stderr receives stdio servers' standard error (default: discarded).
	Stderr io.Writer
}

// Tool is an MCP tool as the model sees it.
type Tool struct {
	// Name is the qualified name, mcp__<server>__<tool>.
	Name        string
	Server      string
	Tool        string // the server's own name for it
	Description string
	InputSchema map[string]any
	ReadOnly    bool
	Approval    ApprovalMode
}

// ServerStatus is one server's state for /mcp.
type ServerStatus struct {
	Name  string
	State State
	Error string
	// Tools are the qualified names offered to the model.
	Tools []string
}

// Manager starts the configured servers on first use and keeps them until
// Close. It is safe for concurrent use.
type Manager struct {
	configs map[string]ServerConfig
	opts    Options

	mu      sync.Mutex
	cancel  context.CancelFunc
	servers map[string]*server // nil until started
	tools   []Tool             // set when every server has started or failed
	done    chan struct{}      // closed then
}

type server struct {
	name  string
	cfg   ServerConfig
	calls chan struct{} // one slot unless the server takes parallel calls

	// Guarded by Manager.mu.
	state   State
	err     error
	session *sdk.ClientSession
	raw     []*sdk.Tool
}

// NewManager returns a manager for the servers, keyed by name. It starts
// nothing until Start, Tools, or Status.
func NewManager(servers map[string]ServerConfig, opts Options) (*Manager, error) {
	for _, name := range slices.Sorted(maps.Keys(servers)) {
		if err := servers[name].Validate(); err != nil {
			return nil, fmt.Errorf("mcp_servers.%s: %w", name, err)
		}
	}
	if opts.Getenv == nil {
		opts.Getenv = os.Getenv
	}

	return &Manager{configs: maps.Clone(servers), opts: opts}, nil
}

// Start begins starting the enabled servers, once.
func (m *Manager) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.servers != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel, m.servers, m.done = cancel, map[string]*server{}, make(chan struct{})
	var wg sync.WaitGroup
	for name, cfg := range m.configs {
		s := &server{name: name, cfg: cfg, state: StateStarting, calls: make(chan struct{}, 1)}
		if cfg.SupportsParallelToolCalls {
			s.calls = nil
		}
		m.servers[name] = s
		if !cfg.IsEnabled() {
			s.state = StateDisabled

			continue
		}
		wg.Go(func() { m.connect(ctx, s) })
	}
	done, servers := m.done, m.servers
	go func() {
		wg.Wait()
		m.mu.Lock()
		if m.done == done { // not closed meanwhile
			m.tools = qualify(servers)
		}
		m.mu.Unlock()
		close(done)
	}()
}

// connect starts one server and lists its tools within the startup timeout.
func (m *Manager) connect(ctx context.Context, s *server) {
	timeout := s.cfg.StartupTimeout()
	startCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	session, tools, err := m.open(startCtx, s.cfg)
	if errors.Is(startCtx.Err(), context.DeadlineExceeded) {
		err = fmt.Errorf("did not start within %s", timeout)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		s.state, s.err = StateFailed, err

		return
	}
	if ctx.Err() != nil { // closed while starting
		_ = session.Close()

		return
	}
	s.state, s.session, s.raw = StateReady, session, tools
	go func() {
		werr := session.Wait()
		m.mu.Lock()
		defer m.mu.Unlock()
		if s.state == StateReady && ctx.Err() == nil {
			s.state, s.err = StateFailed, fmt.Errorf("the server stopped: %w", errOrEOF(werr))
		}
	}()
}

func (m *Manager) open(ctx context.Context, cfg ServerConfig) (*sdk.ClientSession, []*sdk.Tool, error) {
	t, err := m.transport(cfg)
	if err != nil {
		return nil, nil, err
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "uah", Version: "1"}, nil)
	session, err := client.Connect(ctx, t, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect: %w", err)
	}
	var tools []*sdk.Tool
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			_ = session.Close()

			return nil, nil, fmt.Errorf("failed to list tools: %w", err)
		}
		tools = append(tools, tool)
	}

	return session, tools, nil
}

func errOrEOF(err error) error {
	if err == nil {
		return io.EOF
	}

	return err
}

// qualify names the ready servers' allowed tools, in server and tool order
// so names are stable. The caller holds mu.
func qualify(servers map[string]*server) []Tool {
	var candidates []Tool
	for _, s := range servers {
		if s.state != StateReady {
			continue
		}
		for _, t := range s.raw {
			if !s.cfg.Allows(t.Name) {
				continue
			}
			candidates = append(candidates, Tool{
				Server: s.name, Tool: t.Name, Description: t.Description, InputSchema: schema(t.InputSchema),
				ReadOnly: t.Annotations != nil && t.Annotations.ReadOnlyHint, Approval: s.cfg.ApprovalFor(t.Name),
			})
		}
	}
	slices.SortFunc(candidates, func(a, b Tool) int {
		return cmp.Or(cmp.Compare(a.Server, b.Server), cmp.Compare(a.Tool, b.Tool))
	})
	n := newNamer()
	for i := range candidates {
		candidates[i].Name = n.name(candidates[i].Server, candidates[i].Tool)
	}

	return candidates
}

// schema returns the input schema as a JSON object, defaulting to an
// object with no properties.
func schema(v any) map[string]any {
	out := map[string]any{}
	if b, err := json.Marshal(v); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	if out["type"] == nil {
		out["type"] = "object"
	}
	if out["properties"] == nil {
		out["properties"] = map[string]any{}
	}

	return out
}

// Tools starts the servers, waits until each has started or failed, and
// returns the tools to offer. It fails when a required server failed.
func (m *Manager) Tools(ctx context.Context) ([]Tool, error) {
	m.Start() //nolint:contextcheck // servers outlive the caller's context
	m.mu.Lock()
	done := m.done
	m.mu.Unlock()
	select {
	case <-done:
	case <-ctx.Done():
		return nil, fmt.Errorf("failed to start MCP servers: %w", ctx.Err())
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, name := range slices.Sorted(maps.Keys(m.servers)) {
		if s := m.servers[name]; s.cfg.Required && s.state == StateFailed {
			return nil, fmt.Errorf("the required MCP server %s failed to start: %w", name, s.err)
		}
	}

	return slices.Clone(m.tools), nil
}

// Status starts the servers if needed and reports each one.
func (m *Manager) Status() []ServerStatus {
	m.Start()
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ServerStatus, 0, len(m.servers))
	for _, name := range slices.Sorted(maps.Keys(m.servers)) {
		s := m.servers[name]
		st := ServerStatus{Name: name, State: s.state}
		if s.err != nil {
			st.Error = s.err.Error()
		}
		for _, t := range m.tools {
			if t.Server == name {
				st.Tools = append(st.Tools, t.Name)
			}
		}
		out = append(out, st)
	}

	return out
}

// Call calls a server's tool with JSON arguments, within the server's tool
// timeout. An error means the call did not complete; a tool that ran and
// failed returns a Result with IsError.
func (m *Manager) Call(ctx context.Context, serverName, tool string, args json.RawMessage) (Result, error) {
	m.mu.Lock()
	s := m.servers[serverName]
	var session *sdk.ClientSession
	var err error
	switch {
	case s == nil:
		err = fmt.Errorf("the MCP server %s is not running", serverName)
	case s.state == StateFailed:
		err = fmt.Errorf("the MCP server %s failed: %w", serverName, s.err)
	case s.state != StateReady:
		err = fmt.Errorf("the MCP server %s is %s", serverName, s.state)
	default:
		session = s.session
	}
	m.mu.Unlock()
	if err != nil {
		return Result{}, err
	}
	timeout := s.cfg.ToolTimeout()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if s.calls != nil {
		select {
		case s.calls <- struct{}{}:
			defer func() { <-s.calls }()
		case <-ctx.Done():
			return Result{}, callError(ctx, timeout)
		}
	}
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	r, err := session.CallTool(ctx, &sdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		if ctx.Err() != nil {
			return Result{}, callError(ctx, timeout)
		}

		return Result{}, fmt.Errorf("failed to call %s on %s: %w", tool, serverName, err)
	}

	return convert(r), nil
}

func callError(ctx context.Context, timeout time.Duration) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("the tool did not finish within %s", timeout)
	}

	return fmt.Errorf("the call was canceled: %w", ctx.Err())
}

// Close stops the servers. A later Start starts them again.
func (m *Manager) Close() error {
	m.mu.Lock()
	servers, cancel := m.servers, m.cancel
	m.servers, m.tools, m.cancel, m.done = nil, nil, nil, nil
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	var errs []error
	for _, s := range servers {
		m.mu.Lock()
		session := s.session
		m.mu.Unlock()
		if session != nil {
			if err := session.Close(); err != nil {
				errs = append(errs, fmt.Errorf("failed to close the MCP server %s: %w", s.name, err))
			}
		}
	}

	return errors.Join(errs...)
}
