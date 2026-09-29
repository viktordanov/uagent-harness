package render

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/viktordanov/uagent-harness/internal/codereview"
	"github.com/viktordanov/uagent-harness/internal/gitdiff"
	"github.com/viktordanov/uagent-harness/internal/patch"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// /diff draws as the edit tool's diffs do, under a DIFF line with the
// totals; /review as a REVIEW line (live while the reviewer works, with
// its latest tool call), then the verdict's explanation and each finding:
// its priority, title, place, and body.

// gitDiffLines draws /diff's changes: every file with its counts and its
// lines, or its note, and the untracked files past the limit.
func (st *Styles) gitDiffLines(d *gitdiff.Diff, w int) []string {
	files := fmt.Sprintf("%d files ", len(d.Files))
	if len(d.Files) == 1 {
		files = "1 file "
	}
	out := []string{"", st.accent.Render("  DIFF   ") + st.dim.Render(files) + st.counts(d.Added(), d.Removed())}
	for _, f := range d.Files {
		head := st.dim.Render("  └ ") + diffPath(f.FileDiff) + " "
		switch {
		case f.Note != "":
			out = append(out, ansi.Truncate(head+st.dim.Render(f.Note+", not shown"), w, "…"))

			continue
		case f.Untracked:
			head += st.dim.Render("untracked ")
		case f.Op == "add":
			head += st.dim.Render("added ")
		case f.Op == "delete":
			head += st.dim.Render("deleted ")
		}
		out = append(out, ansi.Truncate(head+st.counts(f.Added, f.Removed), w, "…"))
		out = append(out, st.diffBlock([]patch.FileDiff{f.FileDiff}, w, 0)...)
	}
	if d.MoreUntracked > 0 {
		out = append(out, st.dim.Render(fmt.Sprintf("  └ %d more untracked files, not shown", d.MoreUntracked)))
	}

	return out
}

// reviewLines draws a /review.
func (st *Styles) reviewLines(it state.Item, w int, now time.Time) []string {
	r := it.Review
	if r == nil {
		return nil
	}
	if r.Running {
		doing := r.Doing
		if doing == "" {
			doing = "thinking"
		}

		return []string{
			"",
			ansi.Truncate(st.accent.Render("  REVIEW ")+r.Hint+st.dim.Render("  "+elapsed(now.Sub(r.Started))), w, "…"),
			ansi.Truncate(st.dim.Render("    └ ")+st.tool.Render(spin(now))+st.dim.Render(" "+oneLine(doing)), w, "…"),
		}
	}
	head := st.dim.Render("  REVIEW ") + r.Hint
	switch {
	case r.Err != "":
		return []string{"", ansi.Truncate(head+st.bad.Render("  failed: "+oneLine(r.Err)), w, "…")}
	case r.Interrupted:
		return []string{"", ansi.Truncate(head+st.warn.Render("  interrupted"), w, "…")}
	}
	out := []string{"", ansi.Truncate(head+st.dim.Render("  "+reviewSummary(r)), w, "…")}
	if e := strings.TrimSpace(r.Output.OverallExplanation); e != "" {
		out = append(out, st.markdownLines(e, w, "    ", "    ")...)
	} else if len(r.Output.Findings) == 0 {
		out = append(out, st.dim.Render("    "+codereview.FallbackMessage))
	}
	for _, f := range r.Output.Findings {
		out = append(out, st.findingLines(f, r.Workspace, w)...)
	}

	return out
}

// reviewSummary is the finished review's line: its findings, its verdict,
// and how long it took.
func reviewSummary(r *state.Review) string {
	parts := []string{fmt.Sprintf("%d findings", len(r.Output.Findings))}
	if len(r.Output.Findings) == 1 {
		parts[0] = "1 finding"
	}
	if v := strings.TrimSpace(r.Output.OverallCorrectness); v != "" {
		parts = append(parts, v)
	}
	if !r.Ended.IsZero() {
		parts = append(parts, elapsed(r.Ended.Sub(r.Started)))
	}

	return strings.Join(parts, " · ")
}

// findingIndent is where a finding's text starts, after its priority in
// the tool column.
var findingIndent = strings.Repeat(" ", 4+labelWidth)

// priorityTag is the "[P1] " the rubric starts a title with.
var priorityTag = regexp.MustCompile(`^\s*\[P([0-3])\]\s*`)

// findingLines draws one finding: "P1  title" (the priority in the bad
// color for P0 and P1), its place relative to the workspace, and its body.
func (st *Styles) findingLines(f codereview.Finding, workspace string, w int) []string {
	title, prio := f.Title, ""
	if m := priorityTag.FindStringSubmatch(title); m != nil {
		title, prio = title[len(m[0]):], "P"+m[1]
	} else if f.Priority != nil {
		prio = fmt.Sprintf("P%d", *f.Priority)
	}
	tag := st.dim.Render(pad(prio))
	if prio == "P0" || prio == "P1" {
		tag = st.bad.Render(pad(prio))
	}
	out := append([]string{""}, wrapPrefixed(st.bold.Render(title), w, "    "+tag, findingIndent)...)
	out = append(out, ansi.Truncate(st.dim.Render(findingIndent+findingPlace(f, workspace)), w, "…"))
	if body := strings.TrimSpace(f.Body); body != "" {
		out = append(out, st.markdownLines(body, w, findingIndent, findingIndent)...)
	}

	return out
}

// findingPlace is "path:12-14" (one number for one line), relative to the
// workspace when the file is in it.
func findingPlace(f codereview.Finding, workspace string) string {
	loc := f.CodeLocation
	path := loc.AbsoluteFilePath
	if workspace != "" {
		if rel, err := filepath.Rel(workspace, path); err == nil && !strings.HasPrefix(rel, "..") {
			path = rel
		}
	}
	if path == "" {
		return "(no location)"
	}
	lines := fmt.Sprintf("%d-%d", loc.LineRange.Start, loc.LineRange.End)
	if loc.LineRange.End <= loc.LineRange.Start {
		lines = strconv.Itoa(loc.LineRange.Start)
	}

	return path + ":" + lines
}
