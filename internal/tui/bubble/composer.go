package bubble

import (
	"math"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/lipgloss/v2"

	"github.com/viktordanov/uah/internal/tui/render"
)

// minComposerRows is the composer's height limit on a short terminal.
const minComposerRows = 8

// composerRows is how tall the composer grows on a terminal h rows high:
// half the screen, and at least 8 rows, so a long prompt shows more of
// itself and the transcript keeps the other half. Codex caps it only at
// the screen's height, but its transcript lives in the terminal's
// scrollback, where uah's shares the screen.
func composerRows(h int) int { return max(minComposerRows, h/2) }

// resizeComposer fits the composer to the window: its width, and its
// height limit, which SetWidth applies at once.
func (m *Model) resizeComposer() {
	m.composer.MaxHeight = composerRows(m.h)
	m.composer.SetWidth(m.w)
}

func newComposer(theme *render.Styles) textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "Ask uah to do anything · / for commands"
	ta.ShowLineNumbers = false
	// The λ marks the composer's first row only; the rows below it line up
	// under the text, as Codex's composer does.
	ta.SetPromptFunc(2, firstRowPrompt("λ "))
	// The composer grows to composerRows and then scrolls to keep the
	// cursor in view. MaxHeight alone would also refuse new lines once the
	// draft filled it, so after a long paste shift+enter did nothing; the
	// content's only limit is the textarea's own 10,000 lines.
	ta.DynamicHeight = true
	ta.MinHeight = 1
	ta.MaxHeight = minComposerRows
	ta.MaxContentHeight = math.MaxInt
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "ctrl+j"))
	ta.SetStyles(composerStyles(theme))
	ta.SetVirtualCursor(false)
	ta.Focus()

	return ta
}

// firstRowPrompt marks the composer's first row only: λ, or ! in shell
// mode (render.ShellPrompt).
func firstRowPrompt(mark string) func(textarea.PromptInfo) string {
	return func(p textarea.PromptInfo) string {
		if p.LineNumber == 0 {
			return mark
		}

		return "  "
	}
}

// syncShell draws the composer for shell mode after it changed: its mark
// and placeholder come from the state, through render.
func (m *Model) syncShell(was bool) {
	if m.st.Shell == was {
		return
	}
	m.composer.SetPromptFunc(2, firstRowPrompt(render.ShellPrompt(m.st)))
	m.composer.Placeholder = render.ShellPlaceholder(m.st)
}

// composerStyles draw the composer in the theme: the λ in the accent, and
// no backgrounds of its own, since the screen puts it on the band.
func composerStyles(theme *render.Styles) textarea.Styles {
	styles := textarea.DefaultStyles(true)
	for _, st := range []*textarea.StyleState{&styles.Focused, &styles.Blurred} {
		st.Base = lipgloss.NewStyle()
		st.Text = lipgloss.NewStyle()
		st.CursorLine = lipgloss.NewStyle()
		st.Prompt = theme.Accent()
		st.Placeholder = theme.Dim()
	}

	return styles
}

// atEdge reports whether the composer's cursor is at the draft's start or
// end, where ↑ and ↓ recall a prompt left as it was (state.Recalls).
func (m Model) atEdge() bool {
	line, col := m.composer.Line(), m.composer.Column()
	if line == 0 && col == 0 {
		return true
	}
	lines := strings.Split(m.composer.Value(), "\n")

	return line == len(lines)-1 && col == utf8.RuneCountInString(lines[line])
}
