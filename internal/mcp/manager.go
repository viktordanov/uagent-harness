package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"slices"
	"sync"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// State is where a server is in its lifecycle.
type State string

const (
	StateStarting State = "starting"
	StateReady    State = "ready"
	StateFailed   State = "failed"
	StateDisabled State = "disabled"
	// StateNeedsLogin is an HTTP server that asked for OAuth without a
	// usable login; `uah mcp login <name>` fixes it.
	StateNeedsLogin State = "needs_login"
)

var discard = slog.New(slog.DiscardHandler)

// Options configure a Manager.
type Options struct {
	// Workspace is a stdio server's directory unless it sets cwd.
	Workspace string
	// Getenv reads the variables servers get (default os.Getenv).
	Getenv func(string) string
	// Logger receives stdio servers' standard error, one record per line,
	// and problems that do not fail a call (default: discarded).
	Logger *slog.Logger
	// Credentials holds OAuth logins; nil turns OAuth off, so a server
	// that asks for it fails.
	Credentials CredentialStore
	// HTTPClient makes OAuth discovery and token requests (default
	// http.DefaultClient).
	HTTPClient *http.Client
	// ServerFile names the configuration file AlwaysAllow saves a server's
	// tool approval to; nil or "" saves nothing.
	ServerFile func(server string) string
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
	// AutoAsks is Codex's annotation rule for approval_mode auto: a
	// destructive tool asks, a read-only one does not, and otherwise it asks
	// unless marked both non-destructive and closed-world.
	AutoAsks bool
	Approval ApprovalMode
}

// NeedsApproval applies approval_mode as Codex does: prompt always asks,
// writes asks unless the tool is read-only, auto follows the tool's
// annotations, and approve never asks.
func (t Tool) NeedsApproval() bool {
	switch t.Approval {
	case ApprovalApprove:
		return false
	case ApprovalAuto:
		return t.AutoAsks
	case ApprovalWrites:
		return !t.ReadOnly
	case ApprovalPrompt:
	}

	return true
}

// Manager starts the configured servers on first use and keeps them until
// Close. It is safe for concurrent use.
type Manager struct {
	configs map[string]ServerConfig
	opts    Options

	mu      sync.Mutex
	ctx     context.Context // the servers' lifetime, until Close
	cancel  context.CancelFunc
	servers map[string]*server // nil until started
	tools   []Tool             // set when every server has started or failed
	done    chan struct{}      // closed then
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
	if opts.Logger == nil {
		opts.Logger = discard
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = http.DefaultClient
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
	m.ctx, m.cancel = ctx, cancel
	m.servers, m.done = map[string]*server{}, make(chan struct{})
	var wg sync.WaitGroup
	for name, cfg := range m.configs {
		s := newServer(name, cfg, m.opts)
		m.servers[name] = s
		if s.state == StateDisabled {
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

// Tools starts the servers, waits until each has started or failed, and
// returns the tools to offer. It fails when a required server did not
// start.
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
		s := m.servers[name]
		if s.cfg.Required && (s.state == StateFailed || s.state == StateNeedsLogin) {
			return nil, fmt.Errorf("the required MCP server %s failed to start: %w", name, s.err)
		}
	}

	return slices.Clone(m.tools), nil
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
		session, state := s.session, s.state
		m.mu.Unlock()
		if session == nil {
			continue
		}
		// A server that stopped on its own was reported then; its exit
		// status is no news.
		if err := session.Close(); err != nil && state == StateReady && !errors.Is(err, sdk.ErrConnectionClosed) {
			errs = append(errs, fmt.Errorf("failed to close the MCP server %s: %w", s.name, err))
		}
	}

	return errors.Join(errs...)
}
