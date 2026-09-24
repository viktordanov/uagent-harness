// Adapted from openai/codex rust-v0.156.1 (Apache License 2.0, Copyright
// 2025 OpenAI): codex-rs/apply-patch/src/streaming_parser.rs (the update
// hunk lines), codex-rs/apply-patch/src/seek_sequence.rs, and
// codex-rs/apply-patch/src/file_update.rs.

package patch

import (
	"fmt"
	"slices"
	"strings"
)

// updateLine reads one line of an update hunk.
func (p *parser) updateLine(line string) error {
	u := strings.TrimRight(line, " \t\r\n\v\f")
	if ok, err := p.header(u); ok || err != nil {
		return err
	}
	h := &p.hunks[len(p.hunks)-1]
	var last *Chunk
	if n := len(h.Chunks); n > 0 {
		last = &h.Chunks[n-1]
	}
	isContext := u == emptyContext || strings.HasPrefix(u, contextMark)
	switch {
	case last != nil && last.EndOfFile && u == "":
		return nil
	case last != nil && last.EndOfFile && !isContext:
		return p.expectedContext(line)
	case last == nil && h.MovePath == "" && strings.HasPrefix(u, moveTo):
		h.MovePath = u[len(moveTo):]

		return nil
	case isContext && last != nil && last.empty():
		return p.unexpected(line)
	case u == emptyContext:
		h.Chunks = append(h.Chunks, Chunk{})

		return nil
	case strings.HasPrefix(u, contextMark):
		h.Chunks = append(h.Chunks, Chunk{Context: u[len(contextMark):], HasContext: true})

		return nil
	case u == endOfFile:
		if last == nil {
			return nil // Codex ignores it
		}
		if last.empty() {
			return &ParseError{Line: p.line, Message: "Update hunk does not contain any lines"}
		}
		last.EndOfFile = true

		return nil
	}

	return p.changeLine(h, line)
}

// changeLine adds a context, added, or removed line to the last chunk.
func (p *parser) changeLine(h *Hunk, line string) error {
	var kind byte
	switch {
	case line == "":
		kind = ' '
	case line[0] == ' ' || line[0] == '+' || line[0] == '-':
		kind, line = line[0], line[1:]
	default:
		if n := len(h.Chunks); n > 0 && !h.Chunks[n-1].empty() {
			return p.expectedContext(line)
		}

		return p.unexpected(line)
	}
	if len(h.Chunks) == 0 {
		h.Chunks = append(h.Chunks, Chunk{})
	}
	c := &h.Chunks[len(h.Chunks)-1]
	switch kind {
	case ' ':
		c.Old, c.New = append(c.Old, line), append(c.New, line)
	case '+':
		c.New = append(c.New, line)
	case '-':
		c.Old = append(c.Old, line)
	}

	return nil
}

func (p *parser) expectedContext(line string) error {
	return &ParseError{Line: p.line, Message: fmt.Sprintf("Expected update hunk to start with a @@ context marker, got: '%s'", line)}
}

// replacement replaces n lines at start with lines.
type replacement struct {
	start, n int
	lines    []string
}

// updated applies an update hunk's chunks to a file's text, normalizing
// line endings to LF as Codex's default mode does.
func updated(original, path string, chunks []Chunk) (string, error) {
	lines := strings.Split(original, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1] // the final newline, as diff counts lines
	}
	reps, err := replacements(lines, path, chunks)
	if err != nil {
		return "", err
	}
	for _, r := range slices.Backward(reps) {
		end := min(r.start+r.n, len(lines))
		lines = slices.Concat(lines[:r.start], r.lines, lines[end:])
	}
	if len(lines) == 0 || lines[len(lines)-1] != "" {
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n"), nil
}

// replacements finds where each chunk applies, in order.
func replacements(lines []string, path string, chunks []Chunk) ([]replacement, error) {
	var out []replacement
	at := 0
	for _, c := range chunks {
		if c.HasContext {
			i, ok := seek(lines, []string{c.Context}, at, false)
			if !ok {
				return nil, fmt.Errorf("Failed to find context '%s' in %s", c.Context, path) //nolint:staticcheck // Codex's message
			}
			at = i + 1
		}
		if len(c.Old) == 0 {
			insert := len(lines)
			if insert > 0 && lines[insert-1] == "" {
				insert--
			}
			out = append(out, replacement{start: insert, lines: c.New})

			continue
		}
		pattern, repl := c.Old, c.New
		i, ok := seek(lines, pattern, at, c.EndOfFile)
		if !ok && pattern[len(pattern)-1] == "" {
			// The last empty line stands for the file's final newline.
			pattern = pattern[:len(pattern)-1]
			if len(repl) > 0 && repl[len(repl)-1] == "" {
				repl = repl[:len(repl)-1]
			}
			i, ok = seek(lines, pattern, at, c.EndOfFile)
		}
		if !ok {
			return nil, fmt.Errorf("Failed to find expected lines in %s:\n%s", path, strings.Join(c.Old, "\n")) //nolint:staticcheck // Codex's message
		}
		out = append(out, replacement{start: i, n: len(pattern), lines: repl})
		at = i + len(pattern)
	}
	slices.SortStableFunc(out, func(a, b replacement) int { return a.start - b.start })

	return out, nil
}

// seek finds pattern in lines at or after start, trying exact matches,
// then ignoring trailing whitespace, then surrounding whitespace, then
// typographic punctuation. With eof it tries the end of the file first.
func seek(lines, pattern []string, start int, eof bool) (int, bool) {
	if len(pattern) == 0 {
		return start, true
	}
	if len(pattern) > len(lines) {
		return 0, false
	}
	from := start
	if eof {
		from = len(lines) - len(pattern)
	}
	for _, same := range matchers {
		for i := from; i <= len(lines)-len(pattern); i++ {
			if matchAt(lines[i:i+len(pattern)], pattern, same) {
				return i, true
			}
		}
	}

	return 0, false
}

// matchers compare a file line with a pattern line, strictest first.
var matchers = []func(a, b string) bool{
	func(a, b string) bool { return a == b },
	func(a, b string) bool { return strings.TrimRight(a, whitespace) == strings.TrimRight(b, whitespace) },
	func(a, b string) bool { return strings.TrimSpace(a) == strings.TrimSpace(b) },
	func(a, b string) bool { return normalize(a) == normalize(b) },
}

const whitespace = " \t\r\n\v\f"

func matchAt(lines, pattern []string, same func(a, b string) bool) bool {
	for i := range pattern {
		if !same(lines[i], pattern[i]) {
			return false
		}
	}

	return true
}

// normalize maps typographic dashes, quotes, and spaces to ASCII.
func normalize(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\u2010', '\u2011', '\u2012', '\u2013', '\u2014', '\u2015', '\u2212':
			return '-'
		case '\u2018', '\u2019', '\u201A', '\u201B':
			return '\''
		case '\u201C', '\u201D', '\u201E', '\u201F':
			return '"'
		case '\u00A0', '\u2002', '\u2003', '\u2004', '\u2005', '\u2006', '\u2007', '\u2008', '\u2009', '\u200A', '\u202F', '\u205F', '\u3000':
			return ' '
		}

		return r
	}, strings.TrimSpace(s))
}
