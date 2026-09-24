package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Call calls a server's tool with JSON arguments. A server that does not
// take parallel calls runs one at a time; a call waiting for its turn is
// bounded only by ctx, and the tool timeout starts when it runs. An error
// means the call did not complete; a tool that ran and failed returns a
// Result with IsError.
func (m *Manager) Call(ctx context.Context, serverName, tool string, args json.RawMessage) (Result, error) {
	s, err := m.ready(serverName)
	if err != nil {
		return Result{}, err
	}
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
	timeout := s.cfg.ToolTimeout()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	params := &sdk.CallToolParams{Name: tool, Arguments: args}
	r, err := session.CallTool(ctx, params)
	if errors.Is(err, sdk.ErrSessionMissing) && ctx.Err() == nil {
		// The server forgot the session, so it never ran the call; start a
		// new session and call once more, as Codex does.
		if session, err = m.reconnect(ctx, s, session); err == nil {
			r, err = session.CallTool(ctx, params)
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return Result{}, callError(ctx, timeout)
		}

		return Result{}, fmt.Errorf("failed to call %s on %s: %w", tool, serverName, err)
	}

	return convert(r), nil
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
