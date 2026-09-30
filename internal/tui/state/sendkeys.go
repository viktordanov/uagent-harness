package state

import (
	"fmt"
	"strings"
)

// SteerKey is the key that sends a message now while the agent works
// ([tui] steer_key; the keys design, docs/design/keys.md).
type SteerKey int

const (
	// SteerKeyAuto is ctrl+enter where the terminal tells it from enter,
	// and enter elsewhere.
	SteerKeyAuto SteerKey = iota
	// SteerKeyCtrlEnter keeps enter queueing and ctrl+enter sending now.
	SteerKeyCtrlEnter
	// SteerKeyEnter makes enter send now and tab queue, as Codex does.
	SteerKeyEnter
)

// ParseSteerKey reads [tui] steer_key: "auto" (or unset), "ctrl+enter",
// or "enter".
func ParseSteerKey(v string) (SteerKey, error) {
	switch strings.TrimSpace(v) {
	case "", "auto":
		return SteerKeyAuto, nil
	case "ctrl+enter":
		return SteerKeyCtrlEnter, nil
	case "enter":
		return SteerKeyEnter, nil
	}

	return SteerKeyAuto, fmt.Errorf(`[tui] steer_key is %q; want "auto", "ctrl+enter", or "enter"`, v)
}

// Keys is what the send keys mean in this terminal.
type Keys struct {
	Steer SteerKey
	// Disambiguated is the terminal's answer to the keyboard enhancement
	// query: it tells ctrl+enter and shift+enter from enter. A terminal that
	// does not answer (tmux, Terminal.app) leaves it false.
	Disambiguated bool
}

// EnterSteers reports the plain-key bindings: enter sends now while the
// agent works and tab queues. Otherwise enter queues and ctrl+enter sends
// now.
func (k Keys) EnterSteers() bool {
	switch k.Steer {
	case SteerKeyEnter:
		return true
	case SteerKeyCtrlEnter:
		return false
	}

	return !k.Disambiguated
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

// SendIntent maps a send key to its intent for this terminal's bindings,
// or nil when the key sends nothing: an empty enter, or tab while idle,
// which then does what it does in the composer. alt+enter and ctrl+enter
// send now in both bindings; ctrl+enter on a terminal that cannot tell it
// from enter arrives as enter, which then sends now as well.
func (s State) SendIntent(key, draft string) any {
	empty := strings.TrimSpace(draft) == ""
	switch key {
	case KeyCtrlEnter, KeyAltEnter:
		return Steer{Text: draft} // empty: sends the queue now, if any
	case KeyEnter:
		if !s.Keys.EnterSteers() {
			if empty {
				return nil
			}

			return Submit{Text: draft}
		}
		if s.Working() || empty {
			return Steer{Text: draft}
		}

		return Submit{Text: draft}
	case KeyTab:
		if s.Keys.EnterSteers() && s.Working() && !empty && !s.Shell {
			return Submit{Text: draft} // queues
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
// how to queue and how to send now; while idle, how to send and add a line.
func (k Keys) SendHint(working bool) string {
	switch {
	case working && k.EnterSteers():
		return "enter send now · tab queue"
	case working:
		return "enter queue · ctrl+enter send now"
	case k.EnterSteers():
		return "enter send · ctrl+j new line"
	}

	return "enter send · ctrl+enter now"
}

// SendNowKey names the key that sends the queue now, for the queue's hint.
func (k Keys) SendNowKey() string {
	if k.EnterSteers() {
		return KeyEnter
	}

	return KeyCtrlEnter
}

// sendHelp is /help's line for the send and newline keys.
func (k Keys) sendHelp() string {
	if k.EnterSteers() {
		return "enter send (while the agent works: now; on an empty prompt: the queued messages now) · tab queue while the agent works · alt+enter send now · ctrl+j new line (shift+enter where the terminal tells it from enter)"
	}

	return "enter send (queues while the agent works) · ctrl+enter or alt+enter send now (on an empty prompt: the queued messages) · shift+enter or ctrl+j new line"
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
