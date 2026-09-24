package bubble

import (
	tea "charm.land/bubbletea/v2"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// Keys named in more than one mode.
const (
	keyEnter = "enter"
	keyEsc   = "esc"
	keyCtrlN = "ctrl+n"
	keyCtrlC = "ctrl+c"
	keyDown  = "down"
	keyUp    = "up"
	keyCtrlP = "ctrl+p"
)

// onKey maps keys to intents. The keys never change meaning: Enter sends
// (queueing while the agent works), Ctrl+Enter sends now, Shift+Enter adds a
// line.
func (m Model) onKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.st.Mode == state.ModePicker {
		return m.onPickerKey(msg)
	}
	if _, ok := m.st.PendingApproval(); ok {
		return m.onApprovalKey(msg)
	}
	if m.st.Config != nil {
		return m.onConfigKey(msg)
	}
	draft := m.composer.Value()
	if intent := menuIntent(m.st, msg.String(), draft); intent != nil {
		return m.dispatch(intent)
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
	case keyCtrlC:
		if draft != "" {
			m.composer.Reset()

			return m, nil
		}

		return m.dispatch(state.Quit{})
	case "up":
		if draft == "" && len(m.st.Queue) > 0 {
			return m.dispatch(state.EditLastQueued{})
		}
		// The terminal's wheel arrives as ↑ and ↓ when the mouse is not
		// reported: an empty composer scrolls the transcript.
		if draft == "" {
			return m.scroll(1)
		}
	case keyDown:
		if draft == "" {
			return m.scroll(-1)
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
	case "alt+left":
		return m.dispatch(state.SwitchAgent{Delta: -1})
	case "alt+right":
		return m.dispatch(state.SwitchAgent{Delta: 1})
	case "alt+b", "alt+f":
		// Many macOS terminals send alt+← and alt+→ as these word motions;
		// on an empty composer they switch agents, as in Codex.
		if draft == "" {
			delta := 1
			if msg.String() == "alt+b" {
				delta = -1
			}

			return m.dispatch(state.SwitchAgent{Delta: delta})
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

// onApprovalKey answers the approval overlay: y approves, s approves and
// allows the proposed prefix, n, esc, and ctrl+c decline. Other keys wait.
func (m Model) onApprovalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		return m.dispatch(state.Answer{Answer: approval.Approve})
	case "s", "p":
		return m.dispatch(state.Answer{Answer: approval.ApprovePrefix})
	case "n", keyEsc, keyCtrlC:
		return m.dispatch(state.Answer{Answer: approval.Decline})
	}

	return m, nil
}

func (m Model) onPickerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyUp, keyCtrlP:
		return m.dispatch(state.PickerMove{Delta: -1})
	case keyDown, keyCtrlN:
		return m.dispatch(state.PickerMove{Delta: 1})
	case keyEnter:
		return m.dispatch(state.PickerChoose{})
	case keyCtrlC:
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
		scrolled := m.st.Scroll
		if m.st.View != nil {
			scrolled = m.st.View.St.Scroll
		}
		if limit := m.cache.MaxScroll(); limit >= 0 {
			lines = max(min(lines, limit-scrolled), 0)
		}
	}
	if lines == 0 {
		return m, nil
	}

	return m.dispatch(state.ScrollBy{Lines: lines})
}

// menuIntent maps a key to a menu intent while the menu is open, or nil.
func menuIntent(st state.State, key, draft string) any {
	if !st.MenuOpen(draft) {
		return nil
	}
	switch key {
	case "tab":
		return state.MenuAccept{Draft: draft}
	case keyEnter:
		return state.MenuEnter{Draft: draft}
	case keyUp, keyCtrlP:
		return state.MenuMove{Draft: draft, Delta: -1}
	case keyDown, keyCtrlN:
		return state.MenuMove{Draft: draft, Delta: 1}
	case keyEsc:
		return state.MenuClose{Draft: draft}
	}

	return nil
}

// onConfigKey drives the /config panel: ↑↓ choose, enter or space change,
// ← → cycle back and forth, esc closes (or stops typing a value).
func (m Model) onConfigKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	editing := m.st.Config.Editing
	switch msg.String() {
	case keyUp, keyCtrlP:
		return m.dispatch(state.ConfigMove{Delta: -1})
	case keyDown, keyCtrlN:
		return m.dispatch(state.ConfigMove{Delta: 1})
	case keyEnter:
		return m.dispatch(state.ConfigEnter{})
	case keyEsc, keyCtrlC:
		return m.dispatch(state.ConfigEsc{})
	case "left":
		if !editing {
			return m.dispatch(state.ConfigChange{Delta: -1})
		}
	case "right":
		if !editing {
			return m.dispatch(state.ConfigChange{Delta: 1})
		}
	case "space":
		if !editing {
			return m.dispatch(state.ConfigChange{Delta: 1})
		}
	case "backspace":
		return m.dispatch(state.ConfigType{Text: "\b"})
	}
	if editing && msg.Text != "" {
		return m.dispatch(state.ConfigType{Text: msg.Text})
	}

	return m, nil
}
