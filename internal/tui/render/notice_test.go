package render_test

import (
	"fmt"
	"image/color"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/tui/render"
)

// TestNoticeColor: an information notice, such as an auto-approved
// command, is drawn in the theme's neutral gray, while tool lines stay in
// the warm dim and a warning in its own color, in both themes. The golden
// marks each line: "▒" in the notice gray, "░" in the dim, "█" otherwise.
func TestNoticeColor(t *testing.T) {
	s := apply(base(),
		core.RunStarted{At: t0, RunID: "20260924-120000-3f2a1b2c"},
		core.ToolCalled{At: t0, CallID: "c1", Name: "Bash", Label: "ls"},
		core.ToolFinished{At: t0, CallID: "c1", OK: true, Detail: "exit 0", Duration: 100 * time.Millisecond},
		engine.AutoReviewed{At: t0, Command: "python3 fetch.py", Outcome: "allow", Risk: "medium", Reason: "read-only requests for the data the user asked for"},
		engine.AutoReviewed{At: t0, Command: "rm -rf build", Outcome: "deny", Risk: "high", Reason: "deletes files outside the task"},
	)
	for _, tc := range []struct {
		name  string
		theme render.Theme
	}{{"notice", render.Amber}, {"notice-light", render.AmberLight}} {
		t.Run(tc.name, func(t *testing.T) {
			out := rawScreen(s, render.NewCache(tc.theme))
			golden(t, tc.name, colorMarks(out, tc.theme.Notice, tc.theme.Dim))
			for l := range strings.SplitSeq(out, "\n") {
				if strings.Contains(ansi.Strip(l), "auto-approved (medium risk): python3 fetch.py — read-only") {
					assert.Contains(t, l, fg(tc.theme.Notice), "the notice is gray")
					assert.NotContains(t, l, fg(tc.theme.Dim), "and not in the dim")
				}
			}
		})
	}
	assert.NotEqual(t, render.Amber.Notice, render.Amber.Dim)
	assert.NotEqual(t, render.AmberLight.Notice, render.AmberLight.Dim)
}

func fg(c color.Color) string {
	r, g, b, _ := c.RGBA()

	return fmt.Sprintf("38;2;%d;%d;%d", r>>8, g>>8, b>>8)
}

// colorMarks is a screen's text with a mark before each line that has
// text: "▒" when all of it is in the notice color, "░" in the dim, "█"
// otherwise.
func colorMarks(out string, notice, dim color.Color) string {
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		text := strings.TrimRight(ansi.Strip(l), " ")
		switch {
		case strings.TrimSpace(text) == "":
			lines[i] = ""
		case allDim(l, fg(notice)):
			lines[i] = "▒ " + text
		case allDim(l, fg(dim)):
			lines[i] = "░ " + text
		default:
			lines[i] = "█ " + text
		}
	}

	return strings.Join(lines, "\n") + "\n"
}
