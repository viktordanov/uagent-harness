package bubble_test

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/viktordanov/uagent-harness/internal/approval"
)

// TestTUI_ShiftTabCyclesTheMode drives shift+tab through the session: the
// footer follows each change, and the fake runner applies it from the
// next run.
func TestTUI_ShiftTabCyclesTheMode(t *testing.T) {
	d := start(t, deps(t, "simple.jsonl"))
	d.waitFor("gpt-6-sol high ·")

	d.key(tea.KeyTab, tea.ModShift)
	d.waitFor("auto mode ·")
	d.waitFor("Applies from the next run.")
	d.key(tea.KeyTab, tea.ModShift)
	d.waitFor("read only mode ·")
	d.key(tea.KeyTab, tea.ModShift)
	d.waitFor("workspace mode ·")
}

// TestTUI_YoloIsPreselectedAndCycles: a session started with --yolo opens
// in yolo mode, and shift+tab goes read only, workspace, auto, and back to
// yolo; a session without it never offers yolo (TestTUI_ShiftTabCyclesTheMode).
func TestTUI_YoloIsPreselectedAndCycles(t *testing.T) {
	d := start(t, depsIn(t, "simple.jsonl", approval.ModeYolo))
	d.waitFor("yolo mode ·")

	for _, want := range []string{"read only mode ·", "workspace mode ·", "auto mode ·", "yolo mode ·"} {
		d.key(tea.KeyTab, tea.ModShift)
		d.waitFor(want)
	}
}
