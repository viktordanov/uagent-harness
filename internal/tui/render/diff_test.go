package render_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/patch"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/tui/render"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// patchedRun is a run whose model applied a patch to two files: a small
// edit and a long new file.
func patchedRun(t *testing.T) state.State {
	t.Helper()
	text := "*** Begin Patch\n*** Update File: pkg/foo/foo.go\n@@ func Answer() int {\n-\treturn 41\n+\treturn 42\n*** Add File: pkg/foo/doc.md\n+x\n*** End Patch"
	args, err := json.Marshal(patch.Args{Input: text})
	assert.NoError(t, err)
	var added []patch.DiffLine
	for i := range 10 {
		added = append(added, patch.DiffLine{Kind: "+", New: i + 1, Text: fmt.Sprintf("line %d of the notes", i+1)})
	}
	files := []patch.FileDiff{
		{Op: "update", Path: "pkg/foo/foo.go", Added: 1, Removed: 1, Hunks: []patch.DiffHunk{{Lines: []patch.DiffLine{
			{Kind: " ", Old: 11, New: 11, Text: "func Answer() int {"},
			{Kind: "-", Old: 12, Text: "\treturn 41 // the answer"},
			{Kind: "+", New: 12, Text: "\treturn 42 // the answer"},
			{Kind: " ", Old: 13, New: 13, Text: "}"},
		}}}},
		{Op: "add", Path: "pkg/foo/doc.md", Added: 10, Hunks: []patch.DiffHunk{{Lines: added}}},
	}

	return apply(base(),
		core.RunStarted{At: t0, RunID: "20260924-120000-3f2a1b2c"},
		core.ToolCalled{At: t0, CallID: "p1", Name: "apply_patch", Label: string(args), Arguments: string(args)},
		core.ToolStarted{At: t0, CallID: "p1", OpID: "o1"},
		engine.PatchApplied{At: t0, CallID: "p1", Files: files},
		core.ToolFinished{At: t0.Add(100 * time.Millisecond), CallID: "p1", OpID: "o1", OK: true, Detail: "completed", Duration: 100 * time.Millisecond},
		core.RunFinished{At: t0.Add(time.Second), Result: core.Result{Request: core.Request{RunID: "20260924-120000-3f2a1b2c"}, Status: core.StatusOK, Wall: time.Second}},
		session.Idle{At: t0.Add(time.Second)},
	)
}

func TestScreens_Diff(t *testing.T) {
	golden(t, "patch", screen(patchedRun(t), ""))
	golden(t, "patch-details", screen(apply(patchedRun(t), state.ToggleDetails{}), ""))
}

func TestDiff_TintsWholeLinesAndChangedWords(t *testing.T) {
	render.SetTheme(render.Amber)
	out, _ := render.Screen(patchedRun(t), render.NewCache(), render.Frame{Width: 100, Height: 40, Composer: "λ ", ComposerHeight: 1})
	var removed, added string
	for line := range strings.SplitSeq(out, "\n") {
		switch plain := ansi.Strip(line); {
		case strings.Contains(plain, "return 41"):
			removed = line
		case strings.Contains(plain, "return 42"):
			added = line
		}
	}
	assert.Contains(t, removed, "48;2;60;23;15", "a removed line is tinted with Codex's red")
	assert.Contains(t, removed, "48;2;110;42;24", "its changed word is marked")
	assert.Contains(t, added, "48;2;33;41;34", "an added line is tinted with Codex's green")
	assert.Contains(t, added, "48;2;47;90;50", "its changed word is marked")
	assert.Regexp(t, `48;2;33;41;34m +\x1b\[m$`, added, "the tint runs to the edge")
}
