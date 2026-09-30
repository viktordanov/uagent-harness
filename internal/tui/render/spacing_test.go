package render_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/engine"
	"github.com/viktordanov/uah/internal/patch"
	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/internal/tui/render"
	"github.com/viktordanov/uah/internal/tui/state"
)

// spacedRun is a message, two commands, an edit whose diff folds, two more
// commands, and a second message.
func spacedRun(t *testing.T) state.State {
	t.Helper()
	ran := func(id, cmd string) []any {
		return []any{
			core.ToolCalled{At: t0, CallID: id, Name: "Bash", Label: cmd},
			core.ToolStarted{At: t0, CallID: id, OpID: id},
			core.ToolFinished{At: t0, CallID: id, OpID: id, OK: true, Detail: "exit 0", Duration: 100 * time.Millisecond},
		}
	}
	args, err := json.Marshal(patch.Args{Input: "*** Begin Patch\n*** Add File: notes.md\n+x\n*** End Patch"})
	require.NoError(t, err)
	var added []patch.DiffLine
	for i := range 20 {
		added = append(added, patch.DiffLine{Kind: "+", New: i + 1, Text: fmt.Sprintf("line %d", i+1)})
	}
	evs := []any{
		session.InputQueued{At: t0, Input: core.UserInput{ID: "m1", Text: "write the notes"}},
		session.InputSent{At: t0, IDs: []string{"m1"}},
		core.RunStarted{At: t0, RunID: "20260924-120000-3f2a1b2c"},
	}
	evs = append(evs, ran("r1", "go build ./...")...)
	evs = append(evs, ran("r2", "go vet ./...")...)
	evs = append(evs,
		core.ToolCalled{At: t0, CallID: "p1", Name: "apply_patch", Label: string(args), Arguments: string(args)},
		core.ToolStarted{At: t0, CallID: "p1", OpID: "p1"},
		engine.PatchApplied{At: t0, CallID: "p1", Files: []patch.FileDiff{{Op: "add", Path: "notes.md", Added: 20, Hunks: []patch.DiffHunk{{Lines: added}}}}},
		core.ToolFinished{At: t0, CallID: "p1", OpID: "p1", OK: true, Detail: "completed", Duration: 100 * time.Millisecond},
	)
	evs = append(evs, ran("r3", "go test ./...")...)
	evs = append(evs, ran("r4", "git status")...)
	evs = append(evs,
		core.RunFinished{At: t0.Add(time.Second), Result: core.Result{Request: core.Request{RunID: "20260924-120000-3f2a1b2c"}, Status: core.StatusOK, Wall: time.Second}},
		session.Idle{At: t0.Add(time.Second)},
		session.InputQueued{At: t0, Input: core.UserInput{ID: "m2", Text: "thanks"}},
		session.InputSent{At: t0, IDs: []string{"m2"}},
		core.RunStarted{At: t0, RunID: "20260924-120001-3f2a1b2c"},
	)
	evs = append(evs, ran("r5", "ls")...)

	return apply(base(), evs...)
}

// transcriptOf is a frame's lines from the banner's end to the working
// line, ANSI stripped and right-trimmed; band rows are "~".
func transcriptOf(t *testing.T, s state.State) []string {
	t.Helper()
	out, _ := render.Screen(s, render.NewCache(render.Amber), render.Frame{Width: 80, Height: 80, Composer: "λ ", ComposerHeight: 1})
	var lines []string
	for l := range strings.SplitSeq(out, "\n") {
		plain := strings.TrimRight(ansi.Strip(l), " ")
		if plain == "" && strings.Contains(l, "48;2;") {
			plain = "~" // an empty band row
		}
		lines = append(lines, plain)
	}
	start := 0
	for i, l := range lines {
		if strings.HasPrefix(l, "╰") || strings.HasPrefix(l, " uah ·") {
			start = i + 1
		}
	}
	for start < len(lines) && lines[start] == "" {
		start++ // the rows above a short transcript
	}
	start = max(start-1, 0) // the first item's own blank line
	end := len(lines)
	for i, l := range lines {
		if strings.Contains(l, "Working") || strings.HasPrefix(l, "λ ") && i > start+2 && lines[i-1] == "~" && i > len(lines)-5 {
			end = i

			break
		}
	}

	return lines[start:end]
}

// index is the first line that contains text, from line from on.
func index(t *testing.T, lines []string, from int, text string) int {
	t.Helper()
	for i := from; i < len(lines); i++ {
		if strings.Contains(lines[i], text) {
			return i
		}
	}
	require.Failf(t, "missing line", "%q not in\n%s", text, strings.Join(lines, "\n"))

	return -1
}

// TestSpacing: commands stay one line after another, while an edit and its
// diff, and each message's band, have one blank line above and below, and
// the transcript never shows two blank lines in a row.
func TestSpacing(t *testing.T) {
	for _, details := range []bool{false, true} {
		t.Run(fmt.Sprintf("details=%v", details), func(t *testing.T) {
			s := spacedRun(t)
			if details {
				s = apply(s, state.ToggleDetails{})
			}
			lines := transcriptOf(t, s)
			text := strings.Join(lines, "\n")
			for i := 1; i < len(lines); i++ {
				assert.False(t, lines[i] == "" && lines[i-1] == "", "two blank lines at %d:\n%s", i, text)
			}

			// The first message: a blank line, its band, a blank line.
			msg := index(t, lines, 0, "write the notes")
			assert.Equal(t, []string{"", "~"}, lines[msg-2:msg], text)
			assert.Equal(t, []string{"~", ""}, lines[msg+1:msg+3], text)

			// Commands one after another.
			ls := index(t, lines, msg, "go build ./...")
			assert.Contains(t, lines[ls+1], "go vet ./...", text)

			// The edit: a blank line above and one after its diff.
			edit := index(t, lines, ls, "notes.md")
			assert.Equal(t, "", lines[edit-1], text)
			after := index(t, lines, edit, "go test ./...")
			assert.Equal(t, "", lines[after-1], text)
			assert.NotEqual(t, "", lines[after-2], "the diff's last line, or its fold, is right above the blank line")
			assert.Contains(t, lines[after+1], "git status", text)
			if !details {
				assert.Contains(t, lines[after-2], "… +8 lines (ctrl+t to view)", text)
			}

			// The second message.
			msg = index(t, lines, after, "thanks")
			assert.Equal(t, []string{"", "~"}, lines[msg-2:msg], text)
			assert.Equal(t, []string{"~", ""}, lines[msg+1:msg+3], text)
		})
	}
}

// TestSpacing_SelectionAndBacktrack: a blank line above an item is that
// item's, so the text under the mouse is the same with or without it, and
// going back fades the edit's blank lines without shifting rows.
func TestSpacing_SelectionAndBacktrack(t *testing.T) {
	s := spacedRun(t)
	c := render.NewCache(render.Amber)
	f := render.Frame{Width: 80, Height: 80, Composer: "λ ", ComposerHeight: 1}
	out, _ := render.Screen(s, c, f)
	rows := strings.Split(out, "\n")
	for y, l := range rows {
		if !strings.Contains(ansi.Strip(l), "notes.md") {
			continue
		}
		pos, text, ok := c.At(2, y, false)
		require.True(t, ok)
		assert.Contains(t, text, "EDIT")
		assert.Equal(t, 1, pos.Line, "line 0 is the blank line above it")
		above, _, ok := c.At(0, y-1, false)
		require.True(t, ok)
		assert.Equal(t, pos.Key, above.Key, "the blank line belongs to the edit")
	}

	s.Backtrack = &state.Backtrack{Key: s.Items[0].Key} // the first message
	require.Equal(t, state.KindUser, s.Items[0].Kind)
	back, _ := render.Screen(s, render.NewCache(render.Amber), f)
	assert.Equal(t, strings.Count(out, "\n"), strings.Count(back, "\n"))
	plain := func(s string) []string { return strings.Split(ansi.Strip(s), "\n") }
	for i, l := range plain(out) {
		if strings.Contains(l, "notes.md") {
			assert.Equal(t, l, plain(back)[i], "going back fades the rows in place")
			assert.Empty(t, plain(back)[i-1])
		}
	}
}
