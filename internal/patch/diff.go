package patch

import (
	"slices"
	"strings"
)

// DiffContext is how many unchanged lines surround each change in a diff,
// as in the unified diff Codex's TUI draws (unified_diff_from_chunks_with_mode
// in codex-rs/apply-patch/src/file_update.rs uses a context radius of 1).
const DiffContext = 1

// maxDiffLines caps the lines kept per file, so a large added file does not
// bloat the session file; the rest is counted in FileDiff.Omitted.
const maxDiffLines = 2000

// maxEdits caps the line diff's work; a larger rewrite shows as the old
// middle removed and the new one added.
const maxEdits = 2000

// FileDiff is what an applied patch did to one file, for display: the
// counts, and the changed lines with their line numbers and context.
type FileDiff struct {
	// Op is "add", "delete", or "update".
	Op       string `json:"op"`
	Path     string `json:"path"`
	MovePath string `json:"move_path,omitempty"`
	Added    int    `json:"added"`
	Removed  int    `json:"removed"`
	// Hunks are runs of changed lines with DiffContext lines around them.
	Hunks []DiffHunk `json:"hunks,omitempty"`
	// Omitted counts the diff lines left out past the cap.
	Omitted int `json:"omitted,omitempty"`
}

// DiffHunk is one run of lines; hunks are separated by unchanged lines.
type DiffHunk struct {
	Lines []DiffLine `json:"lines"`
}

// DiffLine is one line of a diff. Kind is " " (context), "+", or "-". Old
// and New are its 1-based line numbers in the old and new file (0 when it
// has none there).
type DiffLine struct {
	Kind string `json:"k"`
	Old  int    `json:"o,omitempty"`
	New  int    `json:"n,omitempty"`
	Text string `json:"t"`
}

// Line is the number the gutter shows: the new line's, else the old one's.
func (l DiffLine) Line() int {
	if l.New > 0 {
		return l.New
	}

	return l.Old
}

// String names the op as FileDiff.Op does.
func (o Op) String() string {
	switch o {
	case Add:
		return "add"
	case Delete:
		return "delete"
	case Update:
	}

	return "update"
}

// Diffs is the display diff of each change.
func Diffs(changes []Change) []FileDiff {
	out := make([]FileDiff, 0, len(changes))
	for _, c := range changes {
		out = append(out, Diff(c))
	}

	return out
}

// Diff is the display diff of one change.
func Diff(c Change) FileDiff {
	d := FileDiff{Op: c.Op.String(), Path: c.Path, MovePath: c.MovePath}
	switch c.Op {
	case Add:
		d.Added = len(textLines(c.New))
		d.Hunks, d.Omitted = whole("+", textLines(c.New))
	case Delete:
		d.Removed = len(textLines(c.Old))
		d.Hunks, d.Omitted = whole("-", textLines(c.Old))
	case Update:
		ops := lineDiff(textLines(c.Old), textLines(c.New))
		for _, o := range ops {
			switch o.kind {
			case '+':
				d.Added++
			case '-':
				d.Removed++
			}
		}
		d.Hunks, d.Omitted = hunks(ops, DiffContext)
	}

	return d
}

// textLines splits text into lines without the final newline's empty one.
func textLines(text string) []string {
	if text == "" {
		return nil
	}

	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// whole is one hunk of every line, all added or all removed.
func whole(kind string, lines []string) ([]DiffHunk, int) {
	if len(lines) == 0 {
		return nil, 0
	}
	kept := lines[:min(len(lines), maxDiffLines)]
	h := DiffHunk{Lines: make([]DiffLine, 0, len(kept))}
	for i, text := range kept {
		l := DiffLine{Kind: kind, Text: text}
		if kind == "+" {
			l.New = i + 1
		} else {
			l.Old = i + 1
		}
		h.Lines = append(h.Lines, l)
	}

	return []DiffHunk{h}, len(lines) - len(kept)
}

// edit is one step of a line diff: kind ' ', '+', or '-', with the line's
// index in the old and new file (-1 where it has none).
type edit struct {
	kind byte
	a, b int
	text string
}

// hunks groups a diff's edits into hunks with ctx lines of context, capped
// at maxDiffLines lines.
func hunks(ops []edit, ctx int) ([]DiffHunk, int) {
	var out []DiffHunk
	kept, omitted := 0, 0
	for i := 0; i < len(ops); {
		if ops[i].kind == ' ' {
			i++

			continue
		}
		start, end := max(i-ctx, 0), i
		for j := i; j < len(ops) && j <= end+2*ctx; j++ {
			if ops[j].kind != ' ' {
				end = j
			}
		}
		end = min(end+ctx, len(ops)-1)
		var h DiffHunk
		for _, o := range ops[start : end+1] {
			if kept >= maxDiffLines {
				omitted++

				continue
			}
			kept++
			h.Lines = append(h.Lines, o.line())
		}
		if len(h.Lines) > 0 {
			out = append(out, h)
		}
		i = end + 1
	}

	return out, omitted
}

func (e edit) line() DiffLine {
	l := DiffLine{Kind: string(e.kind), Text: e.text}
	if e.a >= 0 {
		l.Old = e.a + 1
	}
	if e.b >= 0 {
		l.New = e.b + 1
	}

	return l
}

// lineDiff is a shortest edit script from a to b: the common prefix and
// suffix, and Myers' algorithm between them.
func lineDiff(a, b []string) []edit {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	out := make([]edit, 0, len(a)+len(b))
	for i := range pre {
		out = append(out, edit{kind: ' ', a: i, b: i, text: a[i]})
	}
	ma, mb := a[pre:len(a)-suf], b[pre:len(b)-suf]
	for _, e := range myers(ma, mb) {
		if e.a >= 0 {
			e.a += pre
		}
		if e.b >= 0 {
			e.b += pre
		}
		out = append(out, e)
	}
	for i := range suf {
		ai, bi := len(a)-suf+i, len(b)-suf+i
		out = append(out, edit{kind: ' ', a: ai, b: bi, text: a[ai]})
	}

	return out
}

// myers is Myers' O(ND) diff. Past maxEdits it gives up and removes all of
// a, then adds all of b.
func myers(a, b []string) []edit {
	n, m := len(a), len(b)
	if n+m == 0 {
		return nil
	}
	limit := min(n+m, maxEdits)
	off := limit + 1
	v := make([]int, 2*limit+3)
	var trace [][]int // trace[d] is v for k in [-d, d] after step d
	for d := 0; d <= limit; d++ {
		for k := -d; k <= d; k += 2 {
			x := v[off+k-1] + 1
			if k == -d || (k != d && v[off+k-1] < v[off+k+1]) {
				x = v[off+k+1]
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x, y = x+1, y+1
			}
			v[off+k] = x
			if x >= n && y >= m {
				trace = append(trace, slices.Clone(v[off-d:off+d+1]))

				return backtrack(a, b, trace)
			}
		}
		trace = append(trace, slices.Clone(v[off-d:off+d+1]))
	}

	return replaceAll(a, b)
}

// backtrack walks the trace from the end to the start and returns the
// edits in order.
func backtrack(a, b []string, trace [][]int) []edit {
	at := func(d, k int) int { return trace[d][k+d] }
	x, y := len(a), len(b)
	var rev []edit
	for d := len(trace) - 1; d > 0; d-- {
		k := x - y
		prevK := k - 1
		if k == -d || (k != d && at(d-1, k-1) < at(d-1, k+1)) {
			prevK = k + 1
		}
		px := at(d-1, prevK)
		py := px - prevK
		sx, sy := px, py+1 // an insertion moves down
		if prevK == k-1 {
			sx, sy = px+1, py // a deletion moves right
		}
		for x > sx && y > sy {
			x, y = x-1, y-1
			rev = append(rev, edit{kind: ' ', a: x, b: y, text: a[x]})
		}
		if prevK == k-1 {
			rev = append(rev, edit{kind: '-', a: px, b: -1, text: a[px]})
		} else {
			rev = append(rev, edit{kind: '+', a: -1, b: py, text: b[py]})
		}
		x, y = px, py
	}
	for x > 0 && y > 0 {
		x, y = x-1, y-1
		rev = append(rev, edit{kind: ' ', a: x, b: y, text: a[x]})
	}
	slices.Reverse(rev)

	return rev
}

func replaceAll(a, b []string) []edit {
	out := make([]edit, 0, len(a)+len(b))
	for i, t := range a {
		out = append(out, edit{kind: '-', a: i, b: -1, text: t})
	}
	for i, t := range b {
		out = append(out, edit{kind: '+', a: -1, b: i, text: t})
	}

	return out
}
