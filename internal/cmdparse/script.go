// Adapted from openai/codex rust-v0.159.1 (Apache License 2.0, Copyright
// 2025 OpenAI): codex-rs/shell-command/src/parse_command.rs
// (parse_shell_script) and codex-rs/shell-command/src/bash.rs
// (try_parse_word_only_commands_sequence). Codex parses the script with
// tree-sitter-bash; uah parses it with mvdan.cc/sh, accepting the same
// shapes.

package cmdparse

import (
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// command is one plain command of a script; piped means its input is the
// previous command's output.
type command struct {
	words []string
	piped bool
}

// parseShellScript parses a script of plain commands joined by &&, ||,
// ;, and |. Anything else (redirections, substitutions, variables, globs,
// control flow) makes the script one Unknown.
func parseShellScript(script string) []Parsed {
	all, ok := wordOnlyCommands(script)
	if !ok || len(all) == 0 {
		return []Parsed{{Kind: Unknown, Cmd: script}}
	}
	scriptTokens, splitOK := split(script)
	if !splitOK {
		scriptTokens = []string{script}
	}
	hadMultiple := len(all) > 1
	commands := parseCommands(all)
	if len(commands) == 0 {
		return []Parsed{{Kind: Unknown, Cmd: script}}
	}
	if len(commands) > 1 {
		commands = slices.DeleteFunc(commands, func(p Parsed) bool { return p.Kind == Unknown && p.Cmd == "true" })
		for {
			next, ok := simplifyOnce(commands)
			if !ok {
				break
			}
			commands = next
		}
	}
	if len(commands) == 1 {
		commands[0] = attribute(commands[0], script, scriptTokens, hadMultiple || containsConnectors(scriptTokens))
	}

	return commands
}

// parseCommands summarizes the commands that are not formatting helpers,
// following cd. A read piped into head -n N or sed -n a,bp takes that
// range (uah).
func parseCommands(all []command) []Parsed {
	var out []Parsed
	cwd, inDir := "", false
	for i, c := range all {
		if isSmallFormattingCommand(c.words) {
			continue
		}
		if c.words[0] == "cd" {
			if dir, ok := cdTarget(c.words[1:]); ok {
				if inDir {
					dir = joinPaths(cwd, dir)
				}
				cwd, inDir = dir, true
			}

			continue
		}
		for _, p := range summarizeMainTokens(c.words) {
			if p.Kind == Read && inDir {
				p.Path = joinPaths(cwd, p.Path)
			}
			if p.Kind == Read && p.Lines == "" && i+1 < len(all) && all[i+1].piped {
				p.Lines = pipedLines(all[i+1].words)
			}
			out = append(out, p)
		}
	}

	return out
}

// pipedLines is the range a head or sed that reads a pipe shows.
func pipedLines(words []string) string {
	switch words[0] {
	case cmdHead:
		return headLines(words[1:])
	case cmdSed:
		if contains(words, "-n") {
			return sedLines(words[1:])
		}
	}

	return ""
}

// attribute gives a script reduced to one command the whole script as its
// command, for reads and listings Codex shows as the user wrote them; a
// pipeline keeps only its main command, unless it is a read through sed -n.
func attribute(p Parsed, script string, tokens []string, hadConnectors bool) Parsed {
	switch p.Kind {
	case Read:
		if !hadConnectors {
			p.Cmd = join(tokens)
		} else if contains(tokens, "|") && hasSedN(tokens) {
			p.Cmd = script
		}
	case ListFiles, Search:
		if !hadConnectors {
			p.Cmd = join(tokens)
		}
	case Unknown:
	}

	return p
}

func hasSedN(tokens []string) bool {
	for i := 0; i+1 < len(tokens); i++ {
		if tokens[i] == cmdSed && tokens[i+1] == "-n" {
			return true
		}
	}

	return false
}

// wordOnlyCommands reads a script made only of plain commands of literal
// words joined by &&, ||, ;, and |; ok is false for anything else.
func wordOnlyCommands(script string) ([]command, bool) {
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash), syntax.KeepComments(true)).Parse(strings.NewReader(script), "")
	if err != nil || len(f.Last) > 0 {
		return nil, false
	}
	var out []command
	for _, st := range f.Stmts {
		if !appendStmt(&out, st, false) {
			return nil, false
		}
	}

	return out, true
}

// appendStmt appends a statement's commands; piped marks its first
// command as reading a pipe.
func appendStmt(out *[]command, st *syntax.Stmt, piped bool) bool {
	if st.Negated || st.Background || st.Coprocess || len(st.Redirs) > 0 || len(st.Comments) > 0 {
		return false
	}
	switch c := st.Cmd.(type) {
	case *syntax.CallExpr:
		words, ok := literalWords(c)
		if ok {
			*out = append(*out, command{words: words, piped: piped})
		}

		return ok
	case *syntax.BinaryCmd:
		switch c.Op {
		case syntax.AndStmt, syntax.OrStmt:
			return appendStmt(out, c.X, piped) && appendStmt(out, c.Y, false)
		case syntax.Pipe:
			return appendStmt(out, c.X, piped) && appendStmt(out, c.Y, true)
		case syntax.PipeAll:
		}
	}

	return false
}

// literalWords are a call's words when each is literal: no expansion,
// substitution, glob, or escape could change it at run time.
func literalWords(c *syntax.CallExpr) ([]string, bool) {
	if len(c.Assigns) > 0 || len(c.Args) == 0 {
		return nil, false
	}
	words := make([]string, 0, len(c.Args))
	for _, w := range c.Args {
		var b strings.Builder
		for _, part := range w.Parts {
			text, ok := literalPart(part)
			if !ok {
				return nil, false
			}
			b.WriteString(text)
		}
		if b.Len() == 0 && len(w.Parts) > 1 {
			return nil, false
		}
		words = append(words, b.String())
	}

	return words, true
}

// literalPart is a word part's text when it is literal: a bare word with
// no expansion characters, a single-quoted string, or a double-quoted one
// with no escapes or expansions.
func literalPart(part syntax.WordPart) (string, bool) {
	switch p := part.(type) {
	case *syntax.Lit:
		return p.Value, !strings.HasPrefix(p.Value, "=") && !strings.ContainsAny(p.Value, "{}*?[]\\~^#$`")
	case *syntax.SglQuoted:
		return p.Value, !p.Dollar
	case *syntax.DblQuoted:
		if p.Dollar {
			return "", false
		}
		var b strings.Builder
		for _, in := range p.Parts {
			lit, ok := in.(*syntax.Lit)
			if !ok || hasDoubleQuoteEscape(lit.Value) {
				return "", false
			}
			b.WriteString(lit.Value)
		}

		return b.String(), true
	}

	return "", false
}

// hasDoubleQuoteEscape reports an escape double quotes would remove.
func hasDoubleQuoteEscape(s string) bool {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '\\' && strings.IndexByte("$`\"\\\n", s[i+1]) >= 0 {
			return true
		}
	}

	return false
}
