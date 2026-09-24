package state

import (
	"slices"
	"strings"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/usershell"
)

// Shell mode: "!" at the start of an empty composer makes enter run the
// line as a command in the workspace instead of sending it to the agent,
// as Codex's and Claude Code's "!" do. The command and its output join
// the conversation with the next message (session.RunShell).
type (
	// EnterShell is "!" typed into an empty composer.
	EnterShell struct{}
	// LeaveShell is backspace on an empty composer; esc leaves too.
	LeaveShell struct{}
	// NewSession is ctrl+n: /new, also from shell mode.
	NewSession struct{}
)

// EffShell runs a command the user typed.
type EffShell struct{ Command string }

func (EffShell) effect() {}

// shellOutputKeep is how much of a running command's output the item
// keeps; the finished item gets the bounded output the agent sees.
const shellOutputKeep = 16 << 10

// onShell handles shell mode and the user's commands; ok is false for any
// other event.
func (s *State) onShell(ev any) (effects []Effect, ok bool) {
	switch e := ev.(type) {
	case EnterShell:
		if s.Shell {
			return []Effect{EffSetDraft{Text: "!"}}, true // in shell mode "!" is text
		}
		s.Shell = true
	case LeaveShell:
		s.Shell = false
	case NewSession:
		s.Shell = false
		next, effects := s.command("/new")
		*s = next

		return effects, true
	case Esc:
		if !s.Shell {
			return nil, false
		}
		s.Shell = false
	case Submit:
		return s.runShell(e.Text)
	case Steer:
		return s.runShell(e.Text) // a command runs at once anyway
	case session.ShellStarted:
		s.put(Item{Kind: KindShell, Key: "msg:" + e.ID, Text: e.Command, Tool: ToolRunning, Started: e.At})
	case session.ShellOutput:
		s.update("msg:"+e.ID, func(it *Item) {
			it.Detail += e.Text
			if over := len(it.Detail) - shellOutputKeep; over > 0 {
				it.Detail = strings.ToValidUTF8(it.Detail[over:], "")
			}
		})
	case session.ShellFinished:
		s.finishShell(e)
	default:
		return nil, false
	}

	return nil, true
}

// runShell runs the draft as a command in shell mode; ok is false outside
// it, so the draft goes to the agent.
func (s *State) runShell(text string) ([]Effect, bool) {
	if !s.Shell {
		return nil, false
	}
	command := strings.TrimSpace(text)
	if command == "" {
		return nil, true
	}
	s.Shell, s.Scroll = false, 0
	s.Attached = nil // a command carries no images

	return []Effect{EffShell{Command: command}}, true
}

func (s *State) finishShell(e session.ShellFinished) {
	r := e.Result
	it := Item{
		Kind: KindShell, Key: "msg:" + e.ID, Text: r.Command, Detail: r.Output, Exit: r.ExitCode,
		Started: e.At.Add(-r.Duration), Duration: r.Duration, Input: InputQueued, Label: r.Refused,
	}
	if old, ok := s.Item(it.Key); ok {
		it.Started = old.Started
	}
	switch {
	case r.Refused != "":
		it.Tool, it.Detail = ToolFailed, ""
	case r.Canceled || r.TimedOut:
		it.Tool = ToolStopped
	case r.ExitCode == 0:
		it.Tool = ToolOK
	default:
		it.Tool = ToolFailed
	}
	s.put(it)
}

// shellMessage shows the runner's echo of a command's record: it marks
// the command's item delivered, or, in a resumed transcript, makes it.
func (s *State) shellMessage(e core.UserMessage) bool {
	r, ok := usershell.Parse(e.Text)
	if !ok {
		return false
	}
	if s.update("msg:"+e.ID, func(it *Item) { it.Input = InputDelivered }) {
		return true
	}
	state := ToolOK
	if r.ExitCode != 0 {
		state = ToolFailed
	}
	s.put(Item{
		Kind: KindShell, Key: "msg:" + e.ID, Text: r.Command, Detail: r.Output, Exit: r.ExitCode,
		Tool: state, Started: e.At.Add(-r.Duration), Duration: r.Duration, Input: InputDelivered,
	})

	return true
}

// ShellRunning reports whether a command the user typed is running, so
// esc esc can stop it and the clock ticks.
func (s State) ShellRunning() bool {
	for _, it := range slices.Backward(s.Items) {
		if it.Kind == KindShell && it.Tool == ToolRunning {
			return true
		}
	}

	return false
}
