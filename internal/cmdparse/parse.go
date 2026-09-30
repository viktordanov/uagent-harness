// Adapted from openai/codex rust-v0.159.1 (Apache License 2.0, Copyright
// 2025 OpenAI): codex-rs/shell-command/src/parse_command.rs.

package cmdparse

import (
	"path/filepath"
	"slices"
	"strings"
)

// Parse says what a command, as argv, does: one Parsed per command it
// runs, in order, with consecutive duplicates collapsed. When any of them
// is Unknown, the whole command is one Unknown, as Codex shows it. The
// command is model-driven and could be anything, so the parse is lossy by
// design. PowerShell commands, which Codex also reads, are not handled:
// uah runs on macOS and Linux.
func Parse(argv []string) []Parsed {
	var deduped []Parsed
	for _, p := range parseImpl(argv) {
		if len(deduped) > 0 && sameParsed(deduped[len(deduped)-1], p) {
			continue
		}
		deduped = append(deduped, p)
	}
	if slices.ContainsFunc(deduped, func(p Parsed) bool { return p.Kind == Unknown }) {
		if script, ok := extractShell(argv); ok {
			return []Parsed{{Kind: Unknown, Cmd: script}}
		}

		return []Parsed{{Kind: Unknown, Cmd: join(argv)}}
	}

	return deduped
}

// ParseScript parses a script a shell runs, as Parse parses
// ["bash", "-lc", script]: the form uah's Bash tool runs.
func ParseScript(script string) []Parsed {
	return Parse([]string{shBash, flagLC, script})
}

// sameParsed compares the fields Codex's ParsedCommand has.
func sameParsed(a, b Parsed) bool {
	return a.Kind == b.Kind && a.Cmd == b.Cmd && a.Name == b.Name && a.Path == b.Path && a.Query == b.Query
}

func parseImpl(argv []string) []Parsed {
	if script, ok := extractShell(argv); ok {
		return parseShellScript(script)
	}
	normalized := normalizeTokens(argv)
	parts := [][]string{normalized}
	if containsConnectors(normalized) {
		parts = splitOnConnectors(normalized)
	}
	commands := parseSequence(parts)
	for {
		next, ok := simplifyOnce(commands)
		if !ok {
			return commands
		}
		commands = next
	}
}

// parseSequence parses each command in order, following cd so a read's
// path is where the file is.
func parseSequence(parts [][]string) []Parsed {
	var commands []Parsed
	cwd, inDir := "", false
	for _, tokens := range parts {
		if len(tokens) > 0 && tokens[0] == "cd" {
			if dir, ok := cdTarget(tokens[1:]); ok {
				if inDir {
					dir = joinPaths(cwd, dir)
				}
				cwd, inDir = dir, true
			}

			continue
		}
		for _, p := range summarizeMainTokens(tokens) {
			if p.Kind == Read && inDir {
				p.Path = joinPaths(cwd, p.Path)
			}
			commands = append(commands, p)
		}
	}

	return commands
}

// extractShell reads [shell, -c or -lc, script] for bash, zsh, or sh.
func extractShell(argv []string) (script string, ok bool) {
	if len(argv) != 3 || (argv[1] != flagLC && argv[1] != "-c") {
		return "", false
	}
	switch name := strings.TrimSuffix(filepath.Base(argv[0]), ".exe"); name {
	case shBash, shZsh, "sh":
		return argv[2], true
	}

	return "", false
}

// simplifyOnce drops one command that adds nothing to the summary: a
// leading echo, a cd followed by more, a true, or an nl with only flags.
func simplifyOnce(commands []Parsed) ([]Parsed, bool) {
	if len(commands) <= 1 {
		return nil, false
	}
	if firstWord(commands[0]) == "echo" {
		return commands[1:], true
	}
	if i := slices.IndexFunc(commands, func(p Parsed) bool { return firstWord(p) == "cd" }); i >= 0 && len(commands) > i+1 {
		return slices.Delete(slices.Clone(commands), i, i+1), true
	}
	if i := slices.IndexFunc(commands, func(p Parsed) bool { return p.Kind == Unknown && p.Cmd == "true" }); i >= 0 {
		return slices.Delete(slices.Clone(commands), i, i+1), true
	}
	if i := slices.IndexFunc(commands, onlyFlagsNL); i >= 0 {
		return slices.Delete(slices.Clone(commands), i, i+1), true
	}

	return nil, false
}

// firstWord is an Unknown command's first word, else "".
func firstWord(p Parsed) string {
	if p.Kind != Unknown {
		return ""
	}
	if words, ok := split(p.Cmd); ok && len(words) > 0 {
		return words[0]
	}

	return ""
}

func onlyFlagsNL(p Parsed) bool {
	if p.Kind != Unknown {
		return false
	}
	words, ok := split(p.Cmd)
	if !ok || len(words) == 0 || words[0] != "nl" {
		return false
	}
	for _, w := range words[1:] {
		if !strings.HasPrefix(w, "-") {
			return false
		}
	}

	return true
}

// normalizeTokens drops a yes or no piped in front, and opens a
// bash -c or zsh -c script.
func normalizeTokens(cmd []string) []string {
	if len(cmd) >= 2 && cmd[1] == "|" {
		switch cmd[0] {
		case "yes", "y", "no", "n":
			return slices.Clone(cmd[2:])
		}
	}
	if len(cmd) == 3 && (cmd[0] == shBash || cmd[0] == shZsh) && (cmd[1] == "-c" || cmd[1] == flagLC) {
		if words, ok := split(cmd[2]); ok {
			return words
		}
	}

	return slices.Clone(cmd)
}

func isConnector(t string) bool { return t == "&&" || t == "||" || t == "|" || t == ";" }

func containsConnectors(tokens []string) bool { return slices.ContainsFunc(tokens, isConnector) }

func splitOnConnectors(tokens []string) [][]string {
	var out [][]string
	var cur []string
	for _, t := range tokens {
		if !isConnector(t) {
			cur = append(cur, t)

			continue
		}
		if len(cur) > 0 {
			out = append(out, cur)
			cur = nil
		}
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}

	return out
}

// trimAtConnector is tokens up to the first connector.
func trimAtConnector(tokens []string) []string {
	if i := slices.IndexFunc(tokens, isConnector); i >= 0 {
		return tokens[:i]
	}

	return tokens
}

// shortDisplayPath is a path's last element that says something: build,
// dist, node_modules, and src are skipped, so webview/src is webview.
func shortDisplayPath(p string) string {
	trimmed := strings.TrimRight(strings.ReplaceAll(p, `\`, "/"), "/")
	parts := strings.Split(trimmed, "/")
	for _, part := range slices.Backward(parts) {
		switch part {
		case "", "build", "dist", "node_modules", "src":
			continue
		}

		return part
	}

	return trimmed
}

// cdTarget is the directory a cd goes to: its last operand, or the one
// after --.
func cdTarget(args []string) (string, bool) {
	target, ok := "", false
	for i, a := range args {
		if a == "--" {
			if i+1 < len(args) {
				return args[i+1], true
			}

			return "", false
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		target, ok = a, true
	}

	return target, ok
}

// joinPaths joins rel to base unless rel is absolute.
func joinPaths(base, rel string) string {
	if isAbsLike(rel) || base == "" {
		return rel
	}

	return strings.TrimSuffix(base, "/") + "/" + rel
}

func isAbsLike(p string) bool {
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\\`) {
		return true
	}

	return len(p) >= 3 && p[1] == ':' && p[2] == '\\' && (p[0]|0x20 >= 'a' && p[0]|0x20 <= 'z')
}
