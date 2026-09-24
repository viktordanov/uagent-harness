// Package config reads uah's configuration: a user file and, for workspaces
// the user trusts, a project file. Unknown keys are errors, so typos surface.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/viktordanov/uagent-harness/internal/hooks"
	"github.com/viktordanov/uagent-harness/internal/mcp"
)

// Config holds defaults below flags, the environment, and a resumed session.
type Config struct {
	Provider string `toml:"provider"`
	Model    string `toml:"model"`
	Effort   string `toml:"effort"`
	Timeout  string `toml:"timeout"`
	MaxDisk  string `toml:"max_disk"`
	// Engine is "embedded" (the default) or "process".
	Engine string `toml:"engine"`
	// Fast asks for priority processing on the embedded engine.
	Fast bool `toml:"fast"`
	// SandboxMode is read-only, workspace-write (the default), or
	// danger-full-access; the names match Codex's.
	SandboxMode           string                `toml:"sandbox_mode"`
	SandboxWorkspaceWrite SandboxWorkspaceWrite `toml:"sandbox_workspace_write"`
	// ShellEnvironmentPolicy is which environment variables commands get.
	ShellEnvironmentPolicy ShellEnvironmentPolicy `toml:"shell_environment_policy"`
	// ApprovalPolicy is on-request (the default) or never, as Codex's.
	ApprovalPolicy string `toml:"approval_policy"`
	// Approvals are command prefixes allowed or forbidden besides the
	// rules files.
	Approvals Approvals `toml:"approvals"`
	// ApprovalsReviewer is who approves an action that needs approval:
	// auto_review (the default) or user, as Codex's key.
	ApprovalsReviewer string `toml:"approvals_reviewer"`
	// Review configures the auto-reviewer's model call.
	Review Review `toml:"review"`

	// AutoCompactPercent compacts the context once a response used this
	// share of the model's window (default 90; 0 turns it off).
	AutoCompactPercent *int `toml:"auto_compact_percent"`
	// ModelContextWindow overrides the model's context window in tokens, as
	// Codex's key does.
	ModelContextWindow int64 `toml:"model_context_window"`

	Instructions Instructions `toml:"instructions"`
	// Codex's AGENTS.md keys: fallback file names after AGENTS.md (none by
	// default), project root markers (nil: .git; empty: no walking up), and
	// the size cap (the same as [instructions] max_bytes).
	ProjectDocFallbackFilenames []string  `toml:"project_doc_fallback_filenames"`
	ProjectRootMarkers          *[]string `toml:"project_root_markers"`
	ProjectDocMaxBytes          int       `toml:"project_doc_max_bytes"`
	TUI                         TUI       `toml:"tui"`
	// Hooks are keyed by event name: [[hooks.PreToolUse]].
	Hooks map[string][]Hook `toml:"hooks"`
	// MCPServers are keyed by server name, in Codex's format.
	MCPServers map[string]mcp.ServerConfig `toml:"mcp_servers"`
	// Codex's MCP OAuth keys: where logins are kept (auto, file, or
	// keyring) and the callback `uah mcp login` listens on.
	MCPOAuthCredentialsStore string `toml:"mcp_oauth_credentials_store"`
	MCPOAuthCallbackPort     int    `toml:"mcp_oauth_callback_port"`
	MCPOAuthCallbackURL      string `toml:"mcp_oauth_callback_url"`
	// Agents configures subagents, as Codex's [agents].
	Agents Agents `toml:"agents"`

	// Projects are keyed by absolute workspace path.
	Projects map[string]Project `toml:"projects"`
}

// Agents configures subagents with Codex's [agents] keys. Unset values
// take the defaults: enabled, 4 open agents per session, depth 1, and the
// parent's model and effort.
type Agents struct {
	Enabled                        *bool `toml:"enabled"`
	MaxConcurrentThreadsPerSession *int  `toml:"max_concurrent_threads_per_session"`
	// MaxThreads is Codex's older name for the same limit.
	MaxThreads                     *int   `toml:"max_threads"`
	MaxDepth                       *int   `toml:"max_depth"`
	DefaultSubagentModel           string `toml:"default_subagent_model"`
	DefaultSubagentReasoningEffort string `toml:"default_subagent_reasoning_effort"`
}

// MaxThreadsValue is the concurrency limit under either name, or nil.
func (a Agents) MaxThreadsValue() *int {
	if a.MaxConcurrentThreadsPerSession != nil {
		return a.MaxConcurrentThreadsPerSession
	}

	return a.MaxThreads
}

// AgentsDir holds the user's agent role files (*.toml), as Codex's.
func AgentsDir() string { return filepath.Join(Dir(), "agents") }

// ProjectAgentsDir holds a trusted workspace's agent role files.
func ProjectAgentsDir(workspace string) string {
	return filepath.Join(workspace, ".uagent", "agents")
}

// Instructions configure instruction files.
type Instructions struct {
	// Enabled defaults to true.
	Enabled  *bool `toml:"enabled"`
	MaxBytes int   `toml:"max_bytes"`
}

// Hook is one [[hooks.<Event>]] entry.
type Hook struct {
	Matcher string `toml:"matcher"`
	Command string `toml:"command"`
	Timeout string `toml:"timeout"`
	// Source is the file kind it came from, set by Load.
	Source hooks.Source `toml:"-"`
}

// HookList converts the configured hooks, checking events and timeouts.
func (c Config) HookList() ([]hooks.Hook, error) {
	var out []hooks.Hook
	for _, event := range hooks.Events {
		for _, h := range c.Hooks[string(event)] {
			var timeout time.Duration
			if h.Timeout != "" {
				d, err := time.ParseDuration(h.Timeout)
				if err != nil || d <= 0 {
					return nil, fmt.Errorf("invalid %s hook timeout %q", event, h.Timeout)
				}
				timeout = d
			}
			out = append(out, hooks.Hook{Event: event, Matcher: h.Matcher, Command: h.Command, Timeout: timeout, Source: h.Source})
		}
	}
	for name := range c.Hooks {
		if !slices.Contains(hooks.Events, hooks.Event(name)) {
			return nil, fmt.Errorf("unknown hook event %q (want one of %v)", name, hooks.Events)
		}
	}

	return out, nil
}

// SandboxWorkspaceWrite configures the workspace-write sandbox, as Codex's
// [sandbox_workspace_write] does.
type SandboxWorkspaceWrite struct {
	// NetworkAccess lets sandboxed commands use the network.
	NetworkAccess bool `toml:"network_access"`
	// WritableRoots are extra writable directories; ~ is the home directory,
	// and relative paths are relative to the workspace.
	WritableRoots []string `toml:"writable_roots"`
}

// ShellEnvironmentPolicy is Codex's [shell_environment_policy]. Empty, it
// passes the whole environment to commands, as Codex does.
type ShellEnvironmentPolicy struct {
	Inherit               string            `toml:"inherit"` // all, core, or none
	IgnoreDefaultExcludes *bool             `toml:"ignore_default_excludes"`
	Exclude               []string          `toml:"exclude"`
	IncludeOnly           []string          `toml:"include_only"`
	Set                   map[string]string `toml:"set"`
}

// Approvals are simple command rules: each entry is a command prefix, such
// as "git status". Allowed commands run outside the sandbox without asking;
// forbidden ones never run.
type Approvals struct {
	Allow  []string `toml:"allow"`
	Forbid []string `toml:"forbid"`
}

// Review configures the auto-reviewer. Empty fields take the defaults:
// codex-auto-review on openai-codex (else the session model), low effort,
// and a 90s timeout.
type Review struct {
	Model   string `toml:"model"`
	Effort  string `toml:"effort"`
	Timeout string `toml:"timeout"`
}

// TUI configures the terminal UI.
type TUI struct {
	// Details starts in the detailed view (ctrl+t toggles it).
	Details bool `toml:"details"`
}

// Project is per-workspace configuration from the user file.
type Project struct {
	// Trusted allows <workspace>/.uagent/config.toml to apply.
	Trusted bool `toml:"trusted"`
}

// InstructionOptions are the AGENTS.md discovery settings and the size cap
// (project_doc_max_bytes wins over [instructions] max_bytes).
func (c Config) InstructionOptions() (fallbacks []string, markers []string, maxBytes int) {
	if c.ProjectRootMarkers != nil {
		markers = *c.ProjectRootMarkers
		if markers == nil {
			markers = []string{}
		}
	}
	maxBytes = c.Instructions.MaxBytes
	if c.ProjectDocMaxBytes != 0 {
		maxBytes = c.ProjectDocMaxBytes
	}

	return c.ProjectDocFallbackFilenames, markers, maxBytes
}

// InstructionsEnabled reports whether instruction files should be loaded.
func (c Config) InstructionsEnabled() bool {
	return c.Instructions.Enabled == nil || *c.Instructions.Enabled
}

// TimeoutValue parses Timeout; ok is false when it is unset.
func (c Config) TimeoutValue() (d time.Duration, ok bool, err error) {
	if c.Timeout == "" {
		return 0, false, nil
	}
	d, err = time.ParseDuration(c.Timeout)
	if err != nil {
		return 0, false, fmt.Errorf("invalid timeout %q: %w", c.Timeout, err)
	}

	return d, true, nil
}

// Dir is $XDG_CONFIG_HOME/uagent or ~/.config/uagent.
func Dir() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "uagent")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".uagent-config")
	}

	return filepath.Join(home, ".config", "uagent")
}

// UserFile is the user configuration file in Dir.
func UserFile() string { return filepath.Join(Dir(), "config.toml") }

// RulesDir holds the user's command rules files (*.rules).
func RulesDir() string { return filepath.Join(Dir(), "rules") }

// ProjectRulesDir holds a trusted workspace's command rules files.
func ProjectRulesDir(workspace string) string {
	return filepath.Join(workspace, ".uagent", "rules")
}

// ProjectFile is a workspace's project configuration file.
func ProjectFile(workspace string) string {
	return filepath.Join(workspace, ".uagent", "config.toml")
}

// Load reads the user file at userPath and, when the user file trusts the
// workspace, its project file, which overrides the user file. It returns the
// files it read. Missing files are not errors.
func Load(userPath, workspace string) (Config, []string, error) {
	l, err := LoadLayers(userPath, workspace)
	if err != nil {
		return Config{}, nil, err
	}

	return l.Merged(), l.Files(), nil
}

// Layers are the configuration files for a workspace as read, before they
// are merged.
type Layers struct {
	User    Config
	Project Config
	// UserFile and ProjectFile are the paths read ("" when not read).
	UserFile    string
	ProjectFile string
	// Trusted reports whether the user file trusts the workspace.
	Trusted bool
}

// Merged is the user file with the project file over it.
func (l Layers) Merged() Config { return merge(l.User, l.Project) }

// Files are the files read, the user file first.
func (l Layers) Files() []string {
	var files []string
	for _, f := range []string{l.UserFile, l.ProjectFile} {
		if f != "" {
			files = append(files, f)
		}
	}

	return files
}

// LoadLayers reads the user file at userPath and, when it trusts the
// workspace, the workspace's project file. Missing files are not errors.
func LoadLayers(userPath, workspace string) (Layers, error) {
	var l Layers
	found, err := decode(userPath, &l.User)
	if err != nil {
		return Layers{}, err
	}
	if found {
		l.UserFile = userPath
	}
	tagHooks(l.User.Hooks, hooks.SourceUser)
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return Layers{}, fmt.Errorf("failed to resolve workspace: %w", err)
	}
	if l.Trusted = l.User.Projects[abs].Trusted; !l.Trusted {
		return l, nil
	}
	path := ProjectFile(abs)
	if found, err = decode(path, &l.Project); err != nil {
		return Layers{}, err
	}
	if !found {
		return l, nil
	}
	if len(l.Project.Projects) > 0 {
		return Layers{}, fmt.Errorf("%s: [projects] belongs in the user file only", path)
	}
	tagHooks(l.Project.Hooks, hooks.SourceProject)
	l.ProjectFile = path

	return l, nil
}

func decode(path string, into *Config) (bool, error) {
	meta, err := toml.DecodeFile(path, into)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to read %s: %w", path, err)
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		return false, fmt.Errorf("%s: unknown key %q", path, undecoded[0].String())
	}

	return true, nil
}

func tagHooks(byEvent map[string][]Hook, source hooks.Source) {
	for event, list := range byEvent {
		for i := range list {
			list[i].Source = source
		}
		byEvent[event] = list
	}
}
