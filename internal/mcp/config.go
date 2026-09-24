// Package mcp runs Model Context Protocol servers for the embedded engine:
// it starts the configured servers, lists their tools under Codex's names,
// and calls them. The configuration is Codex's [mcp_servers] format, so a
// Codex configuration copies over.
package mcp

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

// Codex's defaults (codex-rs/codex-mcp/src/rmcp_client.rs).
const (
	DefaultStartupTimeout = 30 * time.Second
	DefaultToolTimeout    = 300 * time.Second
)

// ApprovalMode is Codex's per-tool approval_mode.
type ApprovalMode string

const (
	// ApprovalAuto asks by the tool's annotations (Codex's default).
	ApprovalAuto ApprovalMode = "auto"
	// ApprovalPrompt always asks.
	ApprovalPrompt ApprovalMode = "prompt"
	// ApprovalWrites asks unless the tool is annotated read-only.
	ApprovalWrites ApprovalMode = "writes"
	// ApprovalApprove never asks.
	ApprovalApprove ApprovalMode = "approve"
)

var approvalModes = []ApprovalMode{ApprovalAuto, ApprovalPrompt, ApprovalWrites, ApprovalApprove}

// ServerConfig is one [mcp_servers.<name>] table, with Codex's keys. A
// server has either Command (stdio) or URL (streamable HTTP).
type ServerConfig struct {
	// Stdio.
	Command string            `toml:"command"`
	Args    []string          `toml:"args"`
	Env     map[string]string `toml:"env"`
	// EnvVars are variables passed through from uah's environment by name.
	EnvVars []string `toml:"env_vars"`
	// Cwd is the server's directory (default: the workspace).
	Cwd string `toml:"cwd"`

	// Streamable HTTP.
	URL               string            `toml:"url"`
	BearerTokenEnvVar string            `toml:"bearer_token_env_var"`
	HTTPHeaders       map[string]string `toml:"http_headers"`
	// EnvHTTPHeaders maps a header to the environment variable holding its value.
	EnvHTTPHeaders map[string]string `toml:"env_http_headers"`

	// Enabled defaults to true.
	Enabled *bool `toml:"enabled"`
	// Required makes a run fail when the server does not start.
	Required          bool     `toml:"required"`
	StartupTimeoutSec *float64 `toml:"startup_timeout_sec"`
	StartupTimeoutMs  *int64   `toml:"startup_timeout_ms"`
	ToolTimeoutSec    *float64 `toml:"tool_timeout_sec"`
	// EnabledTools, when set, is the only tools offered; DisabledTools are
	// then removed.
	EnabledTools  []string `toml:"enabled_tools"`
	DisabledTools []string `toml:"disabled_tools"`
	// SupportsParallelToolCalls lets calls to the server overlap; otherwise
	// they run one at a time, as in Codex.
	SupportsParallelToolCalls bool                  `toml:"supports_parallel_tool_calls"`
	DefaultToolsApprovalMode  ApprovalMode          `toml:"default_tools_approval_mode"`
	Tools                     map[string]ToolConfig `toml:"tools"`
}

// ToolConfig is one [mcp_servers.<name>.tools.<tool>] table.
type ToolConfig struct {
	ApprovalMode ApprovalMode `toml:"approval_mode"`
}

// IsEnabled reports whether the server should start.
func (c ServerConfig) IsEnabled() bool { return c.Enabled == nil || *c.Enabled }

// Validate checks the transport and values, as Codex's TryFrom does.
func (c ServerConfig) Validate() error {
	stdioOnly := map[string]bool{"args": len(c.Args) > 0, "env": len(c.Env) > 0, "env_vars": len(c.EnvVars) > 0, "cwd": c.Cwd != ""}
	httpOnly := map[string]bool{
		"bearer_token_env_var": c.BearerTokenEnvVar != "", "http_headers": len(c.HTTPHeaders) > 0, "env_http_headers": len(c.EnvHTTPHeaders) > 0,
	}
	switch {
	case c.Command != "" && c.URL != "":
		return errors.New("set command (stdio) or url (streamable HTTP), not both")
	case c.Command != "":
		if key := firstSet(httpOnly); key != "" {
			return fmt.Errorf("%s is not supported for stdio", key)
		}
	case c.URL != "":
		if key := firstSet(stdioOnly); key != "" {
			return fmt.Errorf("%s is not supported for streamable_http", key)
		}
	default:
		return errors.New("set command (stdio) or url (streamable HTTP)")
	}
	for _, v := range []*float64{c.StartupTimeoutSec, c.ToolTimeoutSec} {
		if v != nil && *v <= 0 {
			return errors.New("timeouts must be positive")
		}
	}
	if c.StartupTimeoutMs != nil && *c.StartupTimeoutMs <= 0 {
		return errors.New("timeouts must be positive")
	}
	modes := []ApprovalMode{c.DefaultToolsApprovalMode}
	for _, t := range c.Tools {
		modes = append(modes, t.ApprovalMode)
	}
	for _, m := range modes {
		if m != "" && !slices.Contains(approvalModes, m) {
			return fmt.Errorf("unknown approval mode %q (want one of %v)", m, approvalModes)
		}
	}

	return nil
}

func firstSet(keys map[string]bool) string {
	var set []string
	for k, v := range keys {
		if v {
			set = append(set, k)
		}
	}
	slices.Sort(set)
	if len(set) == 0 {
		return ""
	}

	return set[0]
}

// StartupTimeout is startup_timeout_sec, else startup_timeout_ms, else 30 s.
func (c ServerConfig) StartupTimeout() time.Duration {
	switch {
	case c.StartupTimeoutSec != nil:
		return seconds(*c.StartupTimeoutSec)
	case c.StartupTimeoutMs != nil:
		return time.Duration(*c.StartupTimeoutMs) * time.Millisecond
	}

	return DefaultStartupTimeout
}

// ToolTimeout is tool_timeout_sec, else 300 s.
func (c ServerConfig) ToolTimeout() time.Duration {
	if c.ToolTimeoutSec != nil {
		return seconds(*c.ToolTimeoutSec)
	}

	return DefaultToolTimeout
}

func seconds(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }

// Allows applies enabled_tools and disabled_tools to a raw tool name.
func (c ServerConfig) Allows(tool string) bool {
	if c.EnabledTools != nil && !slices.Contains(c.EnabledTools, tool) {
		return false
	}

	return !slices.Contains(c.DisabledTools, tool)
}

// ApprovalFor is the tool's approval mode: its own, else the server's
// default, else auto.
func (c ServerConfig) ApprovalFor(tool string) ApprovalMode {
	if m := c.Tools[tool].ApprovalMode; m != "" {
		return m
	}
	if c.DefaultToolsApprovalMode != "" {
		return c.DefaultToolsApprovalMode
	}

	return ApprovalAuto
}
