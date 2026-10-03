package instructions

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Include limits: how deep includes may nest, and the bytes all the files
// included into one system prompt may add.
const (
	MaxIncludeDepth = 5
	MaxIncludeBytes = 16 << 10
)

// includer expands the @ lines of instruction files, as Claude Code expands
// a CLAUDE.md's imports: a line that is only "@path" becomes the named
// file's text, in place. A relative path is resolved against the including
// file's directory, and "~/" against the home directory. Each file is
// expanded once, so a loop ends; a file that cannot be read, nests too
// deep, or does not fit is noted in place of its text.
type includer struct {
	// own are the instruction files themselves, which are never included.
	own map[string]bool
	// seen are the files already expanded.
	seen map[string]bool
	// room is what is left of MaxIncludeBytes.
	room int
}

func newIncluder(files []File) *includer {
	x := &includer{own: map[string]bool{}, seen: map[string]bool{}, room: MaxIncludeBytes}
	for _, f := range files {
		x.own[canonical(f.Path)] = true
	}

	return x
}

// expand returns text with its @ lines expanded, and the files it
// included, in order. path is the file text came from; depth is how deep
// text is nested, 0 for an instruction file.
func (x *includer) expand(text, path string, depth int) (string, []string) {
	lines := strings.Split(text, "\n")
	var included []string
	fence := ""
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if f := fenceOf(trimmed); f != "" {
			switch fence {
			case "":
				fence = f
			case f:
				fence = ""
			}

			continue
		}
		ref, ok := includeRef(trimmed)
		if !ok || fence != "" {
			continue
		}
		var files []string
		lines[i], files = x.include(ref, resolveInclude(filepath.Dir(path), ref), depth+1)
		included = append(included, files...)
	}

	return strings.Join(lines, "\n"), included
}

// include is the text that replaces the @ line ref, which names target.
func (x *includer) include(ref, target string, depth int) (string, []string) {
	key := canonical(target)
	switch {
	case x.own[key]:
		return fmt.Sprintf("@%s (%s is an instruction file of its own)", ref, target), nil
	case x.seen[key]:
		return fmt.Sprintf("@%s (%s is included above)", ref, target), nil
	case depth > MaxIncludeDepth:
		return fmt.Sprintf("@%s (not included: includes nest deeper than %d; read %s if you need it)", ref, MaxIncludeDepth, target), nil
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return fmt.Sprintf("@%s (uah could not include %s: %s)", ref, target, reason(err)), nil
	}
	body := strings.TrimSpace(string(data))
	if x.room <= 0 {
		return fmt.Sprintf("@%s (not included: the included files passed %d bytes; read %s when you need it)", ref, MaxIncludeBytes, target), nil
	}
	x.seen[key] = true
	if len(body) > x.room {
		body = cutAt(body, x.room) + fmt.Sprintf("\n(cut: the included files passed %d bytes; read %s for the rest)", MaxIncludeBytes, target)
	}
	x.room -= len(body)
	body, nested := x.expand(body, target, depth)

	return fmt.Sprintf("<include path=%q>\n%s\n</include>", target, body), append([]string{target}, nested...)
}

// includeRef is the path of an @ line: a line that is "@" and a path with
// no spaces.
func includeRef(line string) (string, bool) {
	ref, ok := strings.CutPrefix(line, "@")
	if !ok || ref == "" || strings.ContainsAny(ref, " \t") {
		return "", false
	}

	return ref, true
}

// fenceOf is the fence a line opens or closes (``` or ~~~), or "".
func fenceOf(line string) string {
	for _, f := range []string{"```", "~~~"} {
		if strings.HasPrefix(line, f) {
			return f
		}
	}

	return ""
}

// resolveInclude makes an include's path absolute: ~/ from the home
// directory, a relative one from dir.
func resolveInclude(dir, p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}

	return filepath.Join(dir, p)
}

// canonical names a file the same way through any symbolic link.
func canonical(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}

	return filepath.Clean(path)
}

// reason is why a file could not be read, without the path os puts in its
// errors.
func reason(err error) string {
	if pe := (*fs.PathError)(nil); errors.As(err, &pe) {
		return pe.Err.Error()
	}

	return err.Error()
}

// cutAt shortens text to at most limit bytes, at a line's end when one is
// in the second half.
func cutAt(text string, limit int) string {
	head := text[:limit]
	for !utf8.ValidString(head) {
		head = head[:len(head)-1]
	}
	if i := strings.LastIndexByte(head, '\n'); i > limit/2 {
		head = head[:i]
	}

	return head
}
