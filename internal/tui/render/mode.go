package render

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/viktordanov/uah/internal/approval"
	"github.com/viktordanov/uah/internal/sandbox"
	"github.com/viktordanov/uah/internal/tui/state"
)

// yoloText is how the footer and the header name yolo mode.
var yoloText = approval.ModeYolo.Label() + " mode"

// modeText is the permission mode, which shift+tab changes, as the footer
// and the detailed header show it ("" without one).
func modeText(s state.State) string {
	m := s.Settings.Mode
	if m == "" && s.Settings.Sandbox != "" {
		m = approval.ModeFor(sandbox.Mode(s.Settings.Sandbox))
	}
	if m == "" {
		return ""
	}

	return m.Label() + " mode"
}

// markYolo draws line in base with its "yolo mode" in mark, so the one
// mode that runs everything unasked stands out.
func markYolo(line string, base, mark lipgloss.Style) string {
	before, after, found := strings.Cut(line, yoloText)
	if !found {
		return base.Render(line)
	}

	return base.Render(before) + mark.Render(yoloText) + base.Render(after)
}
