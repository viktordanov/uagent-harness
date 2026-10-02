package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Call calls a server's tool with JSON arguments. A server that does not
// take parallel calls runs one at a time; the tool timeout covers the wait
// for its turn too. An error
// means the call did not complete; a tool that ran and failed returns a
// Result with IsError.
func (m *Manager) Call(ctx context.Context, serverName, tool string, args json.RawMessage) (Result, error) {
	s, err := m.ready(serverName)
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
			return Result{}, fmt.Errorf("the call was canceled while waiting for the server: %w", ctx.Err())
		}
	}
	m.mu.Lock()
	session, state, serr := s.session, s.state, s.err
	m.mu.Unlock()
	if state != StateReady { // it stopped while this call waited
		return Result{}, fmt.Errorf("the MCP server %s %s: %w", serverName, state, serr)
	}
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	params := &sdk.CallToolParams{Name: tool, Arguments: args}
	r, err := session.CallTool(ctx, params)
	if errors.Is(err, sdk.ErrSessionMissing) && ctx.Err() == nil {
		// The server forgot the session, so it never ran the call; start a
		// new session and call once more, as Codex does.
		var next *sdk.ClientSession
		if next, err = m.reconnect(ctx, s, session); err == nil {
			session = next
			r, err = session.CallTool(ctx, params)
		}
	}
	if err != nil {
		if login, ok := errors.AsType[*LoginError](err); ok {
			m.needsLogin(ctx, s, session, login)

			return Result{}, login
		}
		if ctx.Err() != nil {
			return Result{}, callError(ctx, timeout)
		}

		return Result{}, fmt.Errorf("failed to call %s on %s: %w", tool, serverName, err)
	}

	return convert(r), nil
}

// needsLogin marks a running server needs_login when it answered a call
// with a 401, as a 401 at startup does, so /mcp and `uah doctor` say to log
// in and later calls fail at once.
func (m *Manager) needsLogin(ctx context.Context, s *server, session *sdk.ClientSession, login *LoginError) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.state != StateReady || s.session != session {
		return
	}
	s.state, s.err = StateNeedsLogin, login
	m.opts.Logger.LogAttrs(ctx, slog.LevelWarn, "MCP server needs a login",
		slog.String("server", s.name),
		slog.Any("err", login))
}

// ready returns the server if it can take calls.
func (m *Manager) ready(name string) (*server, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.servers[name]
	switch {
	case s == nil:
		return nil, fmt.Errorf("the MCP server %s is not running", name)
	case s.state == StateFailed:
		return nil, fmt.Errorf("the MCP server %s failed: %w", name, s.err)
	case s.state == StateNeedsLogin:
		return nil, s.err
	case s.state != StateReady:
		return nil, fmt.Errorf("the MCP server %s is %s", name, s.state)
	}

	return s, nil
}

func callError(ctx context.Context, timeout time.Duration) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("the tool did not finish within %s", timeout)
	}

	return fmt.Errorf("the call was canceled: %w", ctx.Err())
}
