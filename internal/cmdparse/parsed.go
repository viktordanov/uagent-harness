// Adapted from openai/codex rust-v0.159.1 (Apache License 2.0, Copyright
// 2025 OpenAI): codex-rs/protocol/src/parse_command.rs and
// codex-rs/shell-command/src/parse_command.rs.

// Package cmdparse says what a shell command does, for the TUI's tool
// lines: it reads files, lists files, searches, or something else. Parse
// is a port of Codex's parse_command, the same lossy classification Codex's
// TUI groups commands by; the rest of the package is uah's own: stripping
// the wrappers a command arrives in (wrap.go) and the line the TUI draws
// (summary.go).
package cmdparse

// Kind is what a parsed command does.
type Kind int

const (
	// Unknown is any other command.
	Unknown Kind = iota
	// Read reads a file.
	Read
	// ListFiles lists files.
	ListFiles
	// Search searches file contents or names.
	Search
)

// Parsed is one command of a script, as Codex's ParsedCommand. The fields
// after Query are uah's additions, which Codex's summary drops.
type Parsed struct {
	Kind Kind
	// Cmd is the command, quoted as a shell would need it.
	Cmd string
	// Name is a read file's short name: its last path element.
	Name string
	// Path is a read file's path, joined to any cd before it; for a
	// listing or a search, the short name of its directory ("" for none).
	Path string
	// Query is a search's pattern ("" for none).
	Query string

	// Paths are a listing's or a search's path operands as written.
	Paths []string
	// Globs are a listing's glob filters (rg -g).
	Globs []string
	// Lines is the range a read shows, such as "1-160" ("" for the whole
	// file).
	Lines string
}

// Words the parser matches in several places.
const (
	cmdHead        = "head"
	cmdSed         = "sed"
	shBash         = "bash"
	shZsh          = "zsh"
	flagLC         = "-lc"
	flagFile       = "--file"
	flagExpression = "--expression"
	flagExclude    = "--exclude"
	flagTimeStyle  = "--time-style"
)
