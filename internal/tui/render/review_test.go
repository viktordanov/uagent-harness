package render_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/codereview"
	"github.com/viktordanov/uah/internal/gitdiff"
	"github.com/viktordanov/uah/internal/patch"
	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/internal/tui/state"
)

// TestScreens_Diff draws /diff: an edited file, an untracked one, and a
// binary one with its note.
func TestScreens_GitDiff(t *testing.T) {
	d := gitdiff.Diff{Root: "/workspace/proj", Files: []gitdiff.File{
		{FileDiff: patch.FileDiff{Op: "update", Path: "cmd/main.go", Added: 1, Removed: 1, Hunks: []patch.DiffHunk{{Lines: []patch.DiffLine{
			{Kind: " ", Old: 7, New: 7, Text: "func main() {"},
			{Kind: "-", Old: 8, Text: `	fmt.Println("hi")`},
			{Kind: "+", New: 8, Text: `	fmt.Println("hello")`},
			{Kind: " ", Old: 9, New: 9, Text: "}"},
		}}}}},
		{FileDiff: patch.FileDiff{Op: "add", Path: "NOTES.md", Added: 2, Hunks: []patch.DiffHunk{{Lines: []patch.DiffLine{
			{Kind: "+", New: 1, Text: "# Notes"},
			{Kind: "+", New: 2, Text: "Remember the tests."},
		}}}}, Untracked: true},
		{FileDiff: patch.FileDiff{Op: "add", Path: "logo.png"}, Untracked: true, Note: gitdiff.NoteBinary},
	}, MoreUntracked: 3}
	golden(t, "gitdiff", screen(apply(base(), state.DiffShown{Diff: d}), ""))
}

// TestScreens_Review draws a review while it runs and when it is done:
// the verdict's explanation, then each finding's priority, title, place
// relative to the workspace, and body.
func TestScreens_Review(t *testing.T) {
	running := apply(base(),
		session.ReviewStarted{At: t0, ID: "r1", Hint: "changes against 'main'", Model: "gpt-6-astra", Effort: "high"},
		session.ReviewActivity{At: t0, ID: "r1", Event: core.ToolCalled{At: t0, CallID: "c1", Name: "Bash", Label: `{"command":"git diff 1a2b3c4"}`}},
		state.Tick{Now: t0.Add(42 * time.Second)},
	)
	golden(t, "review-running", screen(running, ""))
	started := apply(base(), session.ReviewStarted{At: t0, ID: "r1", Hint: "changes against 'main'"})
	assert.Contains(t, screen(started, ""), "└ ⠋ thinking", "before its first tool")

	out := codereview.Parse(`{"findings":[
{"title":"[P1] Return the parse error","body":"` + "`parse`" + ` drops the error, so a bad file loads as empty.","confidence_score":0.8,"priority":1,
 "code_location":{"absolute_file_path":"/workspace/proj/internal/config/load.go","line_range":{"start":41,"end":44}}},
{"title":"Name the timeout","body":"A named constant would say what 30 is.","confidence_score":0.4,"priority":3,
 "code_location":{"absolute_file_path":"/elsewhere/x.go","line_range":{"start":7,"end":7}}}],
"overall_correctness":"patch is incorrect","overall_explanation":"The loader hides a failure.","overall_confidence_score":0.7}`)
	done := apply(running, session.ReviewFinished{At: t0.Add(72 * time.Second), ID: "r1", Output: out, Tokens: core.Tokens{InputTokens: 51_000, OutputTokens: 1_200}})
	golden(t, "review", screen(done, ""))

	stopped := apply(running, session.ReviewFinished{At: t0.Add(time.Minute), ID: "r1", Interrupted: true, Tokens: core.Tokens{InputTokens: 20_000, OutputTokens: 300}})
	golden(t, "review-interrupted", screen(stopped, ""))
}
