package config

import "github.com/viktordanov/uagent-harness/internal/mcp"

// merge returns base with every value set in over (a project file)
// replacing it. Hooks, writable roots, approval lists, and environment
// patterns add up; an MCP server replaces the one of the same name whole;
// booleans that turn something on stay on.
func merge(base, over Config) Config {
	set(&base.Provider, over.Provider)
	set(&base.Model, over.Model)
	set(&base.Effort, over.Effort)
	set(&base.Timeout, over.Timeout)
	set(&base.MaxDisk, over.MaxDisk)
	set(&base.Engine, over.Engine)
	base.Fast = base.Fast || over.Fast
	base.TUI.Details = base.TUI.Details || over.TUI.Details
	if over.AutoCompactPercent != nil {
		base.AutoCompactPercent = over.AutoCompactPercent
	}
	if over.ModelContextWindow != 0 {
		base.ModelContextWindow = over.ModelContextWindow
	}
	mergeSandbox(&base, over)
	mergeEnv(&base.ShellEnvironmentPolicy, over.ShellEnvironmentPolicy)
	mergeInstructions(&base, over)
	base.Hooks = mergeMap(base.Hooks, over.Hooks, func(a, b []Hook) []Hook { return append(a, b...) })
	base.MCPServers = mergeMap(base.MCPServers, over.MCPServers, func(_, b mcp.ServerConfig) mcp.ServerConfig { return b })

	return base
}

func set(dst *string, v string) {
	if v != "" {
		*dst = v
	}
}

// mergeSandbox merges the sandbox, approval, and review keys.
func mergeSandbox(base *Config, over Config) {
	set(&base.SandboxMode, over.SandboxMode)
	set(&base.ApprovalPolicy, over.ApprovalPolicy)
	base.Approvals.Allow = append(base.Approvals.Allow, over.Approvals.Allow...)
	base.Approvals.Forbid = append(base.Approvals.Forbid, over.Approvals.Forbid...)
	set(&base.ApprovalsReviewer, over.ApprovalsReviewer)
	set(&base.Review.Model, over.Review.Model)
	set(&base.Review.Effort, over.Review.Effort)
	set(&base.Review.Timeout, over.Review.Timeout)
	w := &base.SandboxWorkspaceWrite
	w.NetworkAccess = w.NetworkAccess || over.SandboxWorkspaceWrite.NetworkAccess
	w.WritableRoots = append(w.WritableRoots, over.SandboxWorkspaceWrite.WritableRoots...)
}

func mergeEnv(env *ShellEnvironmentPolicy, over ShellEnvironmentPolicy) {
	set(&env.Inherit, over.Inherit)
	if over.IgnoreDefaultExcludes != nil {
		env.IgnoreDefaultExcludes = over.IgnoreDefaultExcludes
	}
	env.Exclude = append(env.Exclude, over.Exclude...)
	env.IncludeOnly = append(env.IncludeOnly, over.IncludeOnly...)
	env.Set = mergeMap(env.Set, over.Set, func(_, b string) string { return b })
}

func mergeInstructions(base *Config, over Config) {
	if over.Instructions.Enabled != nil {
		base.Instructions.Enabled = over.Instructions.Enabled
	}
	if over.Instructions.MaxBytes != 0 {
		base.Instructions.MaxBytes = over.Instructions.MaxBytes
	}
	if over.ProjectDocFallbackFilenames != nil {
		base.ProjectDocFallbackFilenames = over.ProjectDocFallbackFilenames
	}
	if over.ProjectRootMarkers != nil {
		base.ProjectRootMarkers = over.ProjectRootMarkers
	}
	if over.ProjectDocMaxBytes != 0 {
		base.ProjectDocMaxBytes = over.ProjectDocMaxBytes
	}
}

// mergeMap adds over's entries to base, combining a key both have.
func mergeMap[V any](base, over map[string]V, combine func(a, b V) V) map[string]V {
	for k, v := range over {
		if base == nil {
			base = map[string]V{}
		}
		base[k] = combine(base[k], v)
	}

	return base
}
