package bubble

import (
	tea "charm.land/bubbletea/v2"

	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// Keys named in more than one mode.
const (
	keyEnter = "enter"
	keyEsc   = "esc"
	keyCtrlN = "ctrl+n"
)

// onKey maps keys to intents. The keys never change meaning: Enter sends
// (queueing while the agent works), Ctrl+Enter sends now, Shift+Enter adds a
// line.
func (m Model) onKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.st.Mode == state.ModePicker {
		return m.onPickerKey(msg)
	}
	draft := m.composer.Value()
	if m.st.MenuOpen(draft) {
		switch msg.String() {
		case "tab":
			return m.dispatch(state.MenuAccept{Draft: draft})
		case keyEnter:
			return m.dispatch(state.MenuEnter{Draft: draft})
		case "up", "ctrl+p":
			return m.dispatch(state.MenuMove{Draft: draft, Delta: -1})
		case "down", keyCtrlN:
			return m.dispatch(state.MenuMove{Draft: draft, Delta: 1})
		case keyEsc:
			return m.dispatch(state.MenuClose{Draft: draft})
		}
	}
	switch msg.String() {
	case keyEnter:
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
	case keyEsc:
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
		return m.scroll(max(m.h/2, 1))
	case "pgdown":
		return m.scroll(-max(m.h/2, 1))
	case "shift+up":
		return m.scroll(1)
	case "shift+down":
		return m.scroll(-1)
	case "end":
		if draft == "" {
			return m.dispatch(state.ScrollToBottom{})
		}
	case "alt+,":
		return m.dispatch(state.StepEffort{Delta: -1})
	case "alt+.":
		return m.dispatch(state.StepEffort{Delta: 1})
	case "ctrl+s":
		return m.dispatch(state.OpenPicker{})
	case keyCtrlN:
		return m.dispatch(state.Submit{Text: "/new"})
	case "ctrl+r":
		return m.dispatch(state.ToggleReasoning{})
	case "ctrl+t":
		return m.dispatch(state.ToggleDetails{})
	}
	var cmd tea.Cmd
	m.composer, cmd = m.composer.Update(msg)
	if next := m.composer.Value(); next != draft {
		model, effects := m.dispatch(state.DraftChanged{Draft: next})

		return model, tea.Batch(cmd, effects)
	}

	return m, cmd
}

func (m Model) onPickerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "ctrl+p":
		return m.dispatch(state.PickerMove{Delta: -1})
	case "down", keyCtrlN:
		return m.dispatch(state.PickerMove{Delta: 1})
	case keyEnter:
		return m.dispatch(state.PickerChoose{})
	case "ctrl+c":
		if m.st.SessionID == "" {
			return m.dispatch(state.Quit{}) // the startup picker: quit, as Codex does
		}

		return m.dispatch(state.PickerCancel{})
	case keyEsc:
		return m.dispatch(state.PickerCancel{})
	case "backspace":
		return m.dispatch(state.PickerType{Text: "\b"})
	case "tab":
		return m.dispatch(state.PickerToggleAll{})
	}
	if msg.Text != "" {
		return m.dispatch(state.PickerType{Text: msg.Text})
	}

	return m, nil
}

// wheelLines is how far one mouse wheel step scrolls.
const wheelLines = 3

func (m Model) onWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if m.st.Mode == state.ModePicker {
		return m, nil
	}
	switch msg.Button {
	case tea.MouseWheelUp:
		return m.scroll(wheelLines)
	case tea.MouseWheelDown:
		return m.scroll(-wheelLines)
	default:
		return m, nil
	}
}

// scroll moves the transcript, stopping at its first line.
func (m Model) scroll(lines int) (tea.Model, tea.Cmd) {
	if lines > 0 {
		m.View() // refresh the limit: frames can lag behind a burst of wheel events
		if limit := m.cache.MaxScroll(); limit >= 0 {
			lines = max(min(lines, limit-m.st.Scroll), 0)
		}
	}
	if lines == 0 {
		return m, nil
	}

	return m.dispatch(state.ScrollBy{Lines: lines})
}
