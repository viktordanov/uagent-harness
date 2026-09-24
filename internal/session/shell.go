package session

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/usershell"
)

// ErrNoShell means the session was opened without a runner for the user's
// commands (Options.Shell).
var ErrNoShell = errors.New("shell commands are not available in this session")

// ShellStarted means a command the user typed (RunShell) started.
type ShellStarted struct {
	At      time.Time
	ID      string
	Command string
}

// ShellOutput is output of a running command, as it arrives.
type ShellOutput struct {
	At   time.Time
	ID   string
	Text string
}

// ShellFinished means the command ended, was stopped, or was refused.
// Result.Record is what the agent gets with the next message, as a user
// message with the same ID.
type ShellFinished struct {
	At     time.Time
	ID     string
	Result usershell.Result
}

func (e ShellStarted) OccurredAt() time.Time  { return e.At }
func (e ShellOutput) OccurredAt() time.Time   { return e.At }
func (e ShellFinished) OccurredAt() time.Time { return e.At }

type cmdShell struct {
	command string
	cancel  context.CancelFunc
}

// shellStart is a started command: its ID and the permission mode it runs
// in.
type shellStart struct {
	id   string
	mode approval.Mode
}

// RunShell runs a command the user typed, as Codex's user shell commands:
// in the workspace, at once, also while the agent works, and until it ends
// or ctx ends, an interrupt stops it, or the session closes. Its record
// (the command, exit code, duration, and bounded output) goes to the agent
// the way Inject does: with the next message, without a turn of its own.
// Events report it: ShellStarted, ShellOutput, and ShellFinished.
func (s *Session) RunShell(ctx context.Context, command string) (usershell.Result, error) {
	if s.shell == nil {
		return usershell.Result{}, ErrNoShell
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return usershell.Result{}, errors.New("the command is empty")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	start, err := call[shellStart](s, cmdShell{command: command, cancel: cancel})
	if err != nil {
		return usershell.Result{}, err
	}
	res := s.shell.Run(ctx, usershell.Request{
		Command: command, Mode: start.mode,
		Stream: func(chunk string) {
			s.post(evNotify{event: ShellOutput{At: time.Now(), ID: start.id, Text: chunk}})
		},
	})
	s.post(evDo(func() { s.onShellDone(start.id, res) }))

	return res, nil
}

// onShell starts a command: the session's interrupt stops it, and so does
// closing the session.
func (s *Session) onShell(c cmdShell) shellStart {
	id := uuid.NewString()
	s.shells[id] = c.cancel
	s.emit(ShellStarted{At: time.Now(), ID: id, Command: c.command})

	return shellStart{id: id, mode: s.settings.Mode}
}

// onShellDone reports the command and holds its record for the next run,
// with the command's ID, so the runner's echo matches ShellFinished.
func (s *Session) onShellDone(id string, res usershell.Result) {
	delete(s.shells, id)
	s.emit(ShellFinished{At: time.Now(), ID: id, Result: res})
	if s.state != StateClosed {
		s.held = append(s.held, core.UserInput{ID: id, Text: res.Text()})
	}
}

// stopShells stops the user's running commands (an interrupt).
func (s *Session) stopShells() {
	for _, cancel := range s.shells {
		cancel()
	}
}
