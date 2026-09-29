package gitdiff

import (
	"strconv"
	"strings"

	"github.com/viktordanov/uagent-harness/internal/patch"
)

// parse reads git's unified diff into display diffs, one per file. Binary
// files keep a note instead of lines, and a file past maxLines keeps the
// count of what it left out.
func parse(text string) []File {
	var files []File
	var cur *File
	var hunk *patch.DiffHunk
	oldN, newN := 0, 0
	flush := func() {
		if cur == nil {
			return
		}
		if hunk != nil && len(hunk.Lines) > 0 {
			cur.Hunks = append(cur.Hunks, *hunk)
		}
		files = append(files, *cur)
		cur, hunk = nil, nil
	}
	for line := range strings.SplitSeq(text, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flush()
			cur = &File{Op: opUpdate, Path: gitPaths(line)}
		case cur == nil:
		case hunk == nil || !isBody(line):
			if strings.HasPrefix(line, "@@ ") {
				if hunk != nil && len(hunk.Lines) > 0 {
					cur.Hunks = append(cur.Hunks, *hunk)
				}
				hunk = &patch.DiffHunk{}
				oldN, newN = hunkStart(line)
			} else {
				header(cur, line)
			}
		default:
			oldN, newN = body(cur, hunk, line, oldN, newN)
		}
	}
	flush()

	return files
}

// isBody reports whether a line inside a hunk is one of its lines.
func isBody(line string) bool {
	return line != "" && strings.ContainsRune(" +-\\", rune(line[0]))
}

// header applies one extended header line of a file's diff.
func header(f *File, line string) {
	switch {
	case strings.HasPrefix(line, "new file mode"):
		f.Op = opAdd
	case strings.HasPrefix(line, "deleted file mode"):
		f.Op = opDelete
	case strings.HasPrefix(line, "rename from "):
		f.Path = unquote(strings.TrimPrefix(line, "rename from "))
	case strings.HasPrefix(line, "rename to "):
		f.MovePath = unquote(strings.TrimPrefix(line, "rename to "))
	case strings.HasPrefix(line, "--- "):
		if p := diffPath(line[4:]); p != "" {
			f.Path = p
		}
	case strings.HasPrefix(line, "+++ "):
		p := diffPath(line[4:])
		switch {
		case p == "" || p == f.Path:
		case f.Op == opAdd:
			f.Path = p
		default:
			f.MovePath = p
		}
	case strings.HasPrefix(line, "Binary files "):
		f.Note = NoteBinary
	case strings.HasPrefix(line, "Submodule "):
		f.Note = strings.TrimSpace(line)
	}
}

// body adds one line of a hunk and returns the next line numbers.
func body(f *File, h *patch.DiffHunk, line string, oldN, newN int) (int, int) {
	kind := line[:1]
	if kind == `\` { // "\ No newline at end of file"
		return oldN, newN
	}
	l := patch.DiffLine{Kind: kind, Text: line[1:]}
	switch kind {
	case "+":
		l.New, newN = newN, newN+1
		f.Added++
	case "-":
		l.Old, oldN = oldN, oldN+1
		f.Removed++
	default:
		l.Old, l.New, oldN, newN = oldN, newN, oldN+1, newN+1
	}
	if f.shown() >= maxLines {
		f.Omitted++
	} else {
		h.Lines = append(h.Lines, l)
	}

	return oldN, newN
}

// hunkStart reads "@@ -12,3 +14,4 @@" into the first old and new line.
func hunkStart(line string) (int, int) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return 1, 1
	}

	return rangeStart(fields[1]), rangeStart(fields[2])
}

func rangeStart(r string) int {
	start, _, _ := strings.Cut(r[1:], ",")
	n, err := strconv.Atoi(start)
	if err != nil || n == 0 {
		return 1
	}

	return n
}

// gitPaths reads the new path from "diff --git a/x b/y"; the --- and +++
// lines, or the rename lines, correct it when they follow.
func gitPaths(line string) string {
	rest := strings.TrimPrefix(line, "diff --git ")
	if i := strings.LastIndex(rest, ` "b/`); strings.HasSuffix(rest, `"`) && i >= 0 {
		return diffPath(rest[i+1:])
	}
	if _, after, ok := strings.CutLast(rest, " b/"); ok {
		return after
	}

	return rest
}

// diffPath reads a --- or +++ path: without its a/ or b/, "" for
// /dev/null.
func diffPath(p string) string {
	p = unquote(strings.TrimSuffix(p, "\t")) // git ends a path with a space so
	if p == "/dev/null" {
		return ""
	}
	if strings.HasPrefix(p, "a/") || strings.HasPrefix(p, "b/") {
		return p[2:]
	}

	return p
}

// unquote reads a path git quoted for its special characters.
func unquote(p string) string {
	if strings.HasPrefix(p, `"`) {
		if s, err := strconv.Unquote(p); err == nil {
			return s
		}
	}

	return p
}
