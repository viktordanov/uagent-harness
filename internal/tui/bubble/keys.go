package bubble

import (
	tea "charm.land/bubbletea/v2"

	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// onKey maps keys to intents. The keys never change meaning: Enter sends
// (queueing while the agent works), Ctrl+Enter sends now, Shift+Enter adds a
// line.
func (m Model) onKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.st.Mode == state.ModePicker {
		return m.onPickerKey(msg)
	}
	draft := m.composer.Value()
	switch msg.String() {
	case "enter":
		if trimmed(draft) == "" {
			return m, nil
		}
		m.composer.Reset()

		return m.dispatch(state.Submit{Text: draft})
	case "ctrl+enter", "alt+enter":
		if trimmed(draft) == "" {
			return m, nil
		}
		m.composer.Reset()

		return m.dispatch(state.Steer{Text: draft})
	case "esc":
		return m.dispatch(state.Esc{})
	case "ctrl+c":
		if draft != "" {
			m.composer.Reset()

			return m, nil
		}

		return m.dispatch(state.Quit{})
	case "up":
		if draft == "" && len(m.st.Queue) > 0 {
			return m.dispatch(state.EditLastQueued{})
		}
	case "pgup":
		return m.dispatch(state.ScrollBy{Lines: max(m.h/2, 1)})
	case "pgdown":
		return m.dispatch(state.ScrollBy{Lines: -max(m.h/2, 1)})
	case "alt+,":
		return m.dispatch(state.StepEffort{Delta: -1})
	case "alt+.":
		return m.dispatch(state.StepEffort{Delta: 1})
	case "ctrl+s":
		return m.dispatch(state.OpenPicker{})
	case "ctrl+n":
		return m.dispatch(state.Submit{Text: "/new"})
	case "ctrl+r":
		return m.dispatch(state.ToggleReasoning{})
	}
	var cmd tea.Cmd
	m.composer, cmd = m.composer.Update(msg)

	return m, cmd
}

func (m Model) onPickerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "ctrl+p":
		return m.dispatch(state.PickerMove{Delta: -1})
	case "down", "ctrl+n":
		return m.dispatch(state.PickerMove{Delta: 1})
	case "enter":
		return m.dispatch(state.PickerChoose{})
	case "esc", "ctrl+c":
		return m.dispatch(state.PickerCancel{})
	case "backspace":
		return m.dispatch(state.PickerType{Text: "\b"})
	}
	if msg.Text != "" {
		return m.dispatch(state.PickerType{Text: msg.Text})
	}

	return m, nil
}
