// Package contextprep prepares the context a session starts with: short
// blocks about the environment, the sandbox, the agent files, and the
// harness, which uah sends once, before the first user message.
package contextprep

import "context"

// Facts is what the adapters know about a session.
type Facts struct {
	// Workspace is the session's working directory.
	Workspace string
	// SystemPrompt is the session's system prompt, which holds the loaded
	// AGENTS.md files.
	SystemPrompt string
	// Shell is the shell commands run in, a path such as /bin/zsh.
	Shell string
	// GOOS is the operating system, as runtime.GOOS names it.
	GOOS string
	// Sandbox describes where commands run.
	Sandbox Sandbox
	// Subagent is true in a subagent's session.
	Subagent bool
}

// Sandbox describes the sandbox commands run in.
type Sandbox struct {
	// Mode is "read-only" or "workspace-write", or "" when commands run
	// without a sandbox.
	Mode string
	// Network is true when sandboxed commands may use the network.
	Network bool
	// TempDir is the session's private writable temporary directory, or "".
	TempDir string
}

// Adapter contributes one block of the prepared context.
type Adapter interface {
	// Name names the block, such as "environment".
	Name() string
	// Prepare returns the block's text, or "" when it has nothing to say.
	Prepare(ctx context.Context, f Facts) string
}
