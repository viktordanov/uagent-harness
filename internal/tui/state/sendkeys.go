package state

import "strings"

// Keys is what the terminal tells about its keys (the keys design,
// docs/design/keys.md).
type Keys struct {
	// Disambiguated is the terminal's answer to the keyboard enhancement
	// query: it tells shift+enter and ctrl+enter from enter. A terminal that
	// does not answer (tmux, Terminal.app) leaves it false.
	Disambiguated bool
}

// NewlineKey names the key that adds a line in this terminal, as Codex's
// footer does: shift+enter where the terminal tells it from enter, and
// ctrl+j, which works everywhere, elsewhere.
func (k Keys) NewlineKey() string {
	if k.Disambiguated {
		return "shift+enter"
	}

	return "ctrl+j"
}

// KeyboardReported is the terminal's answer to the keyboard enhancement
// query (Bubble Tea's KeyboardEnhancementsMsg).
type KeyboardReported struct{ Disambiguates bool }

// Send keys, as Bubble Tea names them.
const (
	KeyEnter     = "enter"
	KeyCtrlEnter = "ctrl+enter"
	KeyAltEnter  = "alt+enter"
	KeyTab       = "tab"
)

// SendIntent maps a send key to its intent, as Codex binds them: while the
// agent works, enter gives the message to it after its running tool calls,
// before its next model request (Steer), and tab queues the message for the
// end of the run (Submit); while idle both send. Enter on an empty composer
// sends the queued messages now. ctrl+enter and alt+enter do what enter
// does, also where the terminal cannot tell them from it. nil means the key
// sends nothing: tab on an empty composer or in shell mode, which then does
// what it does in the composer.
func (s State) SendIntent(key, draft string) any {
	empty := strings.TrimSpace(draft) == ""
	switch key {
	case KeyEnter, KeyCtrlEnter, KeyAltEnter:
		if s.Working() || empty {
			return Steer{Text: draft} // empty: sends the queue now, if any
		}

		return Submit{Text: draft}
	case KeyTab:
		if !empty && !s.Shell {
			return Submit{Text: draft} // queues while the agent works
		}
	}

	return nil
}

// Working reports whether the agent the composer talks to is working: the
// viewed subagent in the agent view, else the session.
func (s State) Working() bool {
	if v := s.View; v != nil {
		return v.St.Live != nil || v.St.Busy
	}

	return s.Busy || s.Live != nil
}

// SendHint is the footer's hint for the send keys: while the agent works,
// how to send now and how to queue; while idle, how to send and add a line.
func (k Keys) SendHint(working bool) string {
	if working {
		return "enter send now · tab queue"
	}

	return "enter send · " + k.NewlineKey() + " new line"
}

// sendHelp is /help's line for the send and newline keys.
func (k Keys) sendHelp() string {
	newline := "ctrl+j new line (shift+enter arrives as enter in this terminal, and sends)"
	if k.Disambiguated {
		newline = "shift+enter or ctrl+j new line"
	}

	return "enter send; while the agent works, it reads the message after its running tool calls (on an empty prompt: the queued messages now) · tab queue for the end of the run (idle: send) · ctrl+enter, alt+enter as enter · " + newline
}

// onKeys keeps the terminal's answer, also for an open agent view.
func (s *State) onKeys(ev any) ([]Effect, bool) {
	e, ok := ev.(KeyboardReported)
	if !ok {
		return nil, false
	}
	s.Keys.Disambiguated = e.Disambiguates
	if s.View != nil {
		s.View.St.Keys = s.Keys
	}

	return nil, true
}
