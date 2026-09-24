package rules

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Split parses a shell command into the words of its simple commands, as
// Codex does before matching rules: plain words and quotes joined by `&&`,
// `||`, `;`, and `|`. ok is false for anything else, such as redirects,
// variables, substitutions, subshells, or control flow, and such a command
// matches no rule.
func Split(command string) ([][]string, bool) {
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(command), "")
	if err != nil || len(file.Stmts) == 0 {
		return nil, false
	}
	var out [][]string
	for _, st := range file.Stmts {
		if !appendStmt(&out, st) {
			return nil, false
		}
	}

	return out, true
}

// Words parses one simple command, such as a rule example or a configured
// prefix.
func Words(command string) ([]string, bool) {
	cmds, ok := Split(command)
	if !ok || len(cmds) != 1 {
		return nil, false
	}

	return cmds[0], true
}

func appendStmt(out *[][]string, st *syntax.Stmt) bool {
	if st.Negated || st.Background || st.Coprocess || st.Disown || len(st.Redirs) > 0 || st.Cmd == nil {
		return false
	}
	switch c := st.Cmd.(type) {
	case *syntax.CallExpr:
		if len(c.Assigns) > 0 || len(c.Args) == 0 {
			return false
		}
		words := make([]string, 0, len(c.Args))
		for _, w := range c.Args {
			s, ok := literal(w)
			if !ok {
				return false
			}
			words = append(words, s)
		}
		*out = append(*out, words)

		return true
	case *syntax.BinaryCmd:
		switch c.Op {
		case syntax.AndStmt, syntax.OrStmt, syntax.Pipe:
			return appendStmt(out, c.X) && appendStmt(out, c.Y)
		default:
			return false
		}
	}

	return false
}

// literal is a word made only of plain text and quotes without expansions.
func literal(w *syntax.Word) (string, bool) {
	var b strings.Builder
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			if strings.ContainsAny(p.Value, "*?[") {
				return "", false // a glob expands to other words
			}
			b.WriteString(unescape(p.Value))
		case *syntax.SglQuoted:
			if p.Dollar {
				return "", false
			}
			b.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, q := range p.Parts {
				lit, ok := q.(*syntax.Lit)
				if !ok {
					return "", false
				}
				b.WriteString(lit.Value)
			}
		default:
			return "", false
		}
	}

	return b.String(), true
}

// unescape drops the backslashes of an unquoted word.
func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	escaped := false
	for _, r := range s {
		if r == '\\' && !escaped {
			escaped = true

			continue
		}
		escaped = false
		b.WriteRune(r)
	}

	return b.String()
}
