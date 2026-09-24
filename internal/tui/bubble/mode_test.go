package bubble_test

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestTUI_ShiftTabCyclesTheMode drives shift+tab through the session: the
// footer follows each change, and the process engine applies it from the
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
