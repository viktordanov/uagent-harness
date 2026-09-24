// Adapted from openai/codex rust-v0.156.1 (Apache License 2.0, Copyright
// 2025 OpenAI): codex-rs/apply-patch/src/parser.rs and
// codex-rs/apply-patch/src/streaming_parser.rs.

// Package patch parses and applies Codex's apply_patch format, and computes
// the diff of what a patch changed. It is a port of Codex's apply-patch
// crate: the same grammar, the same lenient context matching, and the same
// messages, so a model trained on Codex's tool edits files the same way.
package patch

import (
	"fmt"
	"strings"
)

// The patch markers.
const (
	beginPatch   = "*** Begin Patch"
	endPatch     = "*** End Patch"
	addFile      = "*** Add File: "
	deleteFile   = "*** Delete File: "
	updateFile   = "*** Update File: "
	moveTo       = "*** Move to: "
	endOfFile    = "*** End of File"
	contextMark  = "@@ "
	emptyContext = "@@"
)

// The boundary messages.
const (
	errFirstLine = "The first line of the patch must be '*** Begin Patch'"
	errLastLine  = "The last line of the patch must be '*** End Patch'"
)

// Op is what a hunk does to its file.
type Op int

const (
	// Add creates the file (or replaces it) with Contents.
	Add Op = iota
	// Delete removes the file.
	Delete
	// Update changes the file by Chunks, and moves it when MovePath is set.
	Update
)

// Hunk is one file operation of a patch.
type Hunk struct {
	Op   Op
	Path string
	// Contents is an added file's text, each line ending in "\n".
	Contents string
	MovePath string
	Chunks   []Chunk
}

// Target is the path the hunk leaves: the move destination, else Path.
func (h Hunk) Target() string {
	if h.MovePath != "" {
		return h.MovePath
	}

	return h.Path
}

// Chunk is one @@ section of an update: Old lines replaced by New lines,
// after the Context line when there is one.
type Chunk struct {
	// Context is the text after "@@ ", such as a function's signature, that
	// narrows where the chunk applies (HasContext says it was given).
	Context    string
	HasContext bool
	Old, New   []string
	// EndOfFile means Old must be at the end of the file.
	EndOfFile bool
}

func (c Chunk) empty() bool { return len(c.Old) == 0 && len(c.New) == 0 }

// ParseError is a patch that does not follow the grammar. Line is the
// 1-based line of a bad hunk, or 0 when the patch as a whole is invalid.
type ParseError struct {
	Line    int
	Message string
}

func (e *ParseError) Error() string {
	if e.Line == 0 {
		return "invalid patch: " + e.Message
	}

	return fmt.Sprintf("invalid hunk at line %d, %s", e.Line, e.Message)
}

// Parse reads a patch. It is lenient as Codex is: markers may have
// surrounding whitespace, and a patch wrapped in a <<'EOF' heredoc is
// unwrapped.
func Parse(text string) ([]Hunk, error) {
	lines := splitLines(strings.TrimSpace(text))
	lines, err := boundaries(lines)
	if err != nil {
		return nil, err
	}
	p := &parser{}
	for i, line := range lines {
		p.line++
		if i == len(lines)-1 && strings.TrimSpace(line) == endPatch {
			if err := p.ensureUpdateNotEmpty(endPatch); err != nil {
				return nil, err
			}
			p.mode = modeEnded

			continue
		}
		if err := p.process(line); err != nil {
			return nil, err
		}
	}
	if p.mode != modeEnded {
		return nil, &ParseError{Message: errLastLine}
	}

	return p.hunks, nil
}

// splitLines splits on "\n" and drops a "\r" before it, as Rust's lines().
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}

	return lines
}

// boundaries checks the first and last lines, unwrapping a heredoc.
func boundaries(lines []string) ([]string, error) {
	err := strictBoundaries(lines)
	if err == nil {
		return lines, nil
	}
	if n := len(lines); n >= 4 {
		first := lines[0]
		if (first == "<<EOF" || first == "<<'EOF'" || first == `<<"EOF"`) && strings.HasSuffix(lines[n-1], "EOF") {
			inner := lines[1 : n-1]

			return inner, strictBoundaries(inner)
		}
	}

	return nil, err
}

func strictBoundaries(lines []string) error {
	if len(lines) > 0 && strings.TrimSpace(lines[0]) != beginPatch {
		return &ParseError{Message: errFirstLine}
	}
	if len(lines) == 0 || strings.TrimSpace(lines[len(lines)-1]) != endPatch {
		return &ParseError{Message: errLastLine}
	}

	return nil
}

type mode int

const (
	modeNotStarted mode = iota
	modeStarted
	modeAdd
	modeDelete
	modeUpdate
	modeEnded
)

// parser is Codex's streaming parser, fed one line at a time.
type parser struct {
	mode  mode
	hunks []Hunk
	line  int
	// hunkLine is the line of the current update hunk's header.
	hunkLine int
}

func (p *parser) process(line string) error {
	trimmed := strings.TrimSpace(line)
	switch p.mode {
	case modeNotStarted:
		if trimmed == beginPatch {
			p.mode = modeStarted

			return nil
		}

		return &ParseError{Message: errFirstLine}
	case modeStarted, modeDelete:
		if ok, err := p.header(trimmed); ok || err != nil {
			return err
		}

		return p.badHeader(trimmed)
	case modeAdd:
		if ok, err := p.header(trimmed); ok || err != nil {
			return err
		}
		if rest, ok := strings.CutPrefix(line, "+"); ok {
			p.hunks[len(p.hunks)-1].Contents += rest + "\n"

			return nil
		}

		return p.badHeader(trimmed)
	case modeUpdate:
		return p.updateLine(line)
	case modeEnded:
		if trimmed == "" {
			return nil
		}

		return &ParseError{Message: errLastLine}
	}

	return nil
}

func (p *parser) badHeader(trimmed string) error {
	return &ParseError{Line: p.line, Message: fmt.Sprintf(
		"'%s' is not a valid hunk header. Valid hunk headers: '*** Add File: {path}', '*** Delete File: {path}', '*** Update File: {path}'", trimmed)}
}

func (p *parser) unexpected(line string) error {
	return &ParseError{Line: p.line, Message: fmt.Sprintf(
		"Unexpected line found in update hunk: '%s'. Every line should start with ' ' (context line), '+' (added line), or '-' (removed line)", line)}
}

// header starts a hunk or ends the patch; ok is false for any other line.
func (p *parser) header(trimmed string) (ok bool, err error) {
	var h Hunk
	var next mode
	switch {
	case trimmed == endPatch:
		if err := p.ensureUpdateNotEmpty(trimmed); err != nil {
			return false, err
		}
		p.mode = modeEnded

		return true, nil
	case strings.HasPrefix(trimmed, addFile):
		h, next = Hunk{Op: Add, Path: trimmed[len(addFile):]}, modeAdd
	case strings.HasPrefix(trimmed, deleteFile):
		h, next = Hunk{Op: Delete, Path: trimmed[len(deleteFile):]}, modeDelete
	case strings.HasPrefix(trimmed, updateFile):
		h, next = Hunk{Op: Update, Path: trimmed[len(updateFile):]}, modeUpdate
	default:
		return false, nil
	}
	if err := p.ensureUpdateNotEmpty(trimmed); err != nil {
		return false, err
	}
	p.hunks = append(p.hunks, h)
	p.mode = next
	if next == modeUpdate {
		p.hunkLine = p.line
	}

	return true, nil
}

// ensureUpdateNotEmpty rejects a new header or the end while the current
// update hunk has no chunk, or its last chunk no lines.
func (p *parser) ensureUpdateNotEmpty(line string) error {
	if len(p.hunks) == 0 || p.hunks[len(p.hunks)-1].Op != Update {
		return nil
	}
	h := p.hunks[len(p.hunks)-1]
	if len(h.Chunks) == 0 && p.mode == modeUpdate {
		return &ParseError{Line: p.hunkLine, Message: fmt.Sprintf("Update file hunk for path '%s' is empty", h.Path)}
	}
	if len(h.Chunks) > 0 && h.Chunks[len(h.Chunks)-1].empty() {
		if line == endPatch {
			return &ParseError{Line: p.line, Message: "Update hunk does not contain any lines"}
		}

		return p.unexpected(line)
	}

	return nil
}
