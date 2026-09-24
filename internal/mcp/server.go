package mcp

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// implementation is how uah introduces itself to servers.
var implementation = &sdk.Implementation{Name: "uah", Version: "1"}

// server is one configured server and its connection.
type server struct {
	name  string
	cfg   ServerConfig
	calls chan struct{} // one slot unless the server takes parallel calls

	// Guarded by Manager.mu.
	state   State
	err     error
	session *sdk.ClientSession
	raw     []*sdk.Tool
	auth    *storedAuth // HTTP servers with OAuth
}

func newServer(name string, cfg ServerConfig, opts Options) *server {
	s := &server{name: name, cfg: cfg, state: StateStarting}
	if cfg.URL != "" && !cfg.usesBearer() && opts.Credentials != nil {
		s.auth = &storedAuth{name: name, url: cfg.URL, store: opts.Credentials, client: opts.HTTPClient, logger: opts.Logger}
	}
	if !cfg.SupportsParallelToolCalls {
		s.calls = make(chan struct{}, 1)
	}
	if !cfg.IsEnabled() {
		s.state = StateDisabled
	}

	return s
}

// connect starts one server and lists its tools within the startup timeout.
func (m *Manager) connect(ctx context.Context, s *server) {
	timeout := s.cfg.StartupTimeout()
	startCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	session, tools, err := m.open(startCtx, s)
	if errors.Is(startCtx.Err(), context.DeadlineExceeded) {
		err = fmt.Errorf("did not start within %s; a slow server needs a larger startup_timeout_sec", timeout)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var login *LoginError
	switch {
	case errors.As(err, &login):
		s.state, s.err = StateNeedsLogin, login
	case err != nil:
		s.state, s.err = StateFailed, err
	case ctx.Err() != nil: // closed while starting
		_ = session.Close()
	default:
		s.state, s.err, s.session, s.raw = StateReady, nil, session, tools
		go m.watch(ctx, s, session)
	}
}

// watch marks the server failed when its connection ends: a stdio server
// exited, or an HTTP server went away. It is not restarted, as in Codex.
func (m *Manager) watch(ctx context.Context, s *server, session *sdk.ClientSession) {
	werr := session.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.session == session && s.state == StateReady && ctx.Err() == nil {
		s.state, s.err = StateFailed, fmt.Errorf("the server stopped: %w", errOrEOF(werr))
		m.opts.Logger.LogAttrs(ctx, slog.LevelWarn, "MCP server stopped",
			slog.String("server", s.name),
			slog.Any("err", s.err))
	}
}

// reconnect opens a new session for an HTTP server whose session expired
// (a 404 for its session ID), as Codex re-initializes it.
func (m *Manager) reconnect(ctx context.Context, s *server, old *sdk.ClientSession) (*sdk.ClientSession, error) {
	m.mu.Lock()
	serversCtx := m.ctx
	m.mu.Unlock()
	if serversCtx == nil {
		return nil, fmt.Errorf("the MCP server %s is not running", s.name)
	}
	startCtx, cancel := context.WithTimeout(ctx, s.cfg.StartupTimeout())
	defer cancel()
	session, _, err := m.open(startCtx, s)
	if err != nil {
		return nil, fmt.Errorf("failed to reconnect to %s after its session expired: %w", s.name, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case serversCtx.Err() != nil:
		_ = session.Close()

		return nil, fmt.Errorf("the MCP server %s is not running", s.name)
	case s.session != old: // another call reconnected first
		_ = session.Close()

		return s.session, nil
	}
	s.state, s.err, s.session = StateReady, nil, session
	go m.watch(serversCtx, s, session) //nolint:contextcheck // the servers' lifetime, not the call's
	_ = old.Close()

	return session, nil
}

func (m *Manager) open(ctx context.Context, s *server) (*sdk.ClientSession, []*sdk.Tool, error) {
	t, err := m.transport(s)
	if err != nil {
		return nil, nil, err
	}
	client := sdk.NewClient(implementation, &sdk.ClientOptions{
		// Codex logs a changed tool list and keeps the one it listed at
		// startup, so the tools offered stay stable for the session.
		ToolListChangedHandler: func(ctx context.Context, _ *sdk.ToolListChangedRequest) {
			m.opts.Logger.LogAttrs(ctx, slog.LevelInfo, "MCP server tool list changed; the tools listed at startup stay", slog.String("server", s.name))
		},
	})
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
				ReadOnly: t.Annotations != nil && t.Annotations.ReadOnlyHint, AutoAsks: autoAsks(t.Annotations), Approval: s.cfg.ApprovalFor(t.Name),
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

// autoAsks ports Codex's requires_mcp_tool_approval
// (codex-rs/core/src/mcp_tool_call.rs): unset hints default to asking.
func autoAsks(a *sdk.ToolAnnotations) bool {
	if a == nil {
		return true
	}
	if a.DestructiveHint != nil && *a.DestructiveHint {
		return true
	}
	if a.ReadOnlyHint {
		return false
	}
	destructive := a.DestructiveHint == nil || *a.DestructiveHint
	openWorld := a.OpenWorldHint == nil || *a.OpenWorldHint

	return destructive || openWorld
}
