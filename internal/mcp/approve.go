package mcp

import (
	"fmt"
	"maps"
	"slices"

	"github.com/viktordanov/uagent-harness/internal/config/tomledit"
)

// ParseApprovalMode checks an approval_mode value.
func ParseApprovalMode(s string) (ApprovalMode, error) {
	m := ApprovalMode(s)
	if !slices.Contains(approvalModes, m) {
		return "", fmt.Errorf("invalid approval mode %q (want approve, prompt, writes, or auto)", s)
	}

	return m, nil
}

// SetApproval writes a server's approval mode to a configuration file:
// default_tools_approval_mode when tool is empty, else
// [mcp_servers.<server>.tools.<tool>] approval_mode. Comments and the
// other keys stay as they were; the server must still validate, or the
// file is left alone.
func SetApproval(path, server, tool string, mode ApprovalMode) error {
	if _, err := ParseApprovalMode(string(mode)); err != nil {
		return err
	}
	data, perm, err := tomledit.Read(path)
	if err != nil {
		return err //nolint:wrapcheck // Read names the file
	}
	key := []string{"mcp_servers", server, "default_tools_approval_mode"}
	if tool != "" {
		key = []string{"mcp_servers", server, "tools", tool, "approval_mode"}
	}
	if data, err = tomledit.Set(data, key, string(mode)); err != nil {
		return fmt.Errorf("failed to edit %s: %w", path, err)
	}
	if err := checkServer(data, server, true); err != nil {
		return fmt.Errorf("mcp_servers.%s in %s: %w", server, path, err)
	}

	return tomledit.Write(path, data, perm) //nolint:wrapcheck // Write names the file
}

// ToolApproval is the current approval mode of the tool with the
// qualified name, which AlwaysAllow may have changed since Tools returned
// it. ok is false for a tool the manager does not offer.
func (m *Manager) ToolApproval(name string) (mode ApprovalMode, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := slices.IndexFunc(m.tools, func(t Tool) bool { return t.Name == name })
	if i < 0 {
		return "", false
	}

	return m.tools[i].Approval, true
}

// AlwaysAllow sets the tool with the qualified name to approval_mode
// approve, as Codex's "Allow and don't ask me again" does: at once for
// this session, and in the file Options.ServerFile names for its server.
// The session keeps it when the file cannot be written.
func (m *Manager) AlwaysAllow(name string) error {
	m.mu.Lock()
	i := slices.IndexFunc(m.tools, func(t Tool) bool { return t.Name == name })
	if i < 0 {
		m.mu.Unlock()

		return fmt.Errorf("no MCP tool %s", name)
	}
	t := &m.tools[i]
	t.Approval = ApprovalApprove
	server, tool := t.Server, t.Tool
	cfg := m.configs[server]
	cfg.Tools = maps.Clone(cfg.Tools)
	if cfg.Tools == nil {
		cfg.Tools = map[string]ToolConfig{}
	}
	cfg.Tools[tool] = ToolConfig{ApprovalMode: ApprovalApprove}
	m.configs[server] = cfg // a restart keeps it
	m.mu.Unlock()
	if m.opts.ServerFile == nil {
		return nil
	}
	path := m.opts.ServerFile(server)
	if path == "" {
		return nil
	}
	if err := SetApproval(path, server, tool, ApprovalApprove); err != nil {
		return fmt.Errorf("allowed %s for this session, but failed to save it: %w", name, err)
	}

	return nil
}
