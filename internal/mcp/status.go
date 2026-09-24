package mcp

import (
	"maps"
	"slices"
	"strings"
)

// Transports, as Codex names them.
const (
	TransportStdio = "stdio"
	TransportHTTP  = "streamable_http"
)

// ServerStatus is one server's state for /mcp and `uah doctor`.
type ServerStatus struct {
	Name  string
	State State
	Error string
	// Transport is stdio or streamable_http; Target is the command line or
	// the URL.
	Transport string
	Target    string
	Auth      AuthStatus
	// Tools are the tools offered to the model, in name order.
	Tools []Tool
}

// Transport is the server's transport name.
func (c ServerConfig) Transport() string {
	if c.URL != "" {
		return TransportHTTP
	}

	return TransportStdio
}

// Target is the command line or the URL.
func (c ServerConfig) Target() string {
	if c.URL != "" {
		return c.URL
	}

	return strings.Join(append([]string{c.Command}, c.Args...), " ")
}

// Status starts the servers if needed and reports each one. While some are
// still starting, the ready ones' tools are named as if the others fail.
func (m *Manager) Status() []ServerStatus {
	m.Start()
	m.mu.Lock()
	defer m.mu.Unlock()
	tools := m.tools
	if tools == nil {
		tools = qualify(m.servers)
	}
	out := make([]ServerStatus, 0, len(m.servers))
	for _, name := range slices.Sorted(maps.Keys(m.servers)) {
		s := m.servers[name]
		st := ServerStatus{Name: name, State: s.state, Transport: s.cfg.Transport(), Target: s.cfg.Target(), Auth: s.authStatus()}
		if s.err != nil {
			st.Error = s.err.Error()
		}
		for _, t := range tools {
			if t.Server == name {
				st.Tools = append(st.Tools, t)
			}
		}
		out = append(out, st)
	}

	return out
}

// authStatus is the server's auth status as far as starting it showed.
// The caller holds Manager.mu.
func (s *server) authStatus() AuthStatus {
	switch {
	case s.cfg.URL == "":
		return AuthUnsupported
	case s.cfg.usesBearer():
		return AuthBearerToken
	case s.state == StateNeedsLogin:
		return AuthNotLoggedIn
	case s.auth != nil && s.auth.loggedIn():
		return AuthOAuth
	}

	return AuthUnsupported
}

func (a *storedAuth) loggedIn() bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.hasLogin
}
