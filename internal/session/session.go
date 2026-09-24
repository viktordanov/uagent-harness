// Package session is the long-lived object the TUI and `uah run` talk to. A
// session owns its settings, a queue of messages, and at most one live run,
// and it merges run events and its own events into one ordered stream.
package session

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/hooks"
)

// ErrClosed means the session has been closed.
var ErrClosed = errors.New("session is closed")

const eventBuffer = 4096

// State is where the session is in its run lifecycle.
type State string

const (
	StateIdle     State = "idle"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateClosed   State = "closed"
)

// Options configure a new session.
type Options struct {
	// ID resumes that runner session; empty starts a new one.
	ID string
	// Resumed marks an ID that already has runs, for SessionOpened.
	Resumed  bool
	Settings Settings
	// Instructions, when set, is emitted after SessionOpened.
	Instructions *InstructionsLoaded
	// Hooks run at session events (nil runs none).
	Hooks *hooks.Runner
	// SessionsDir holds the runner session files; hooks get the file's path.
	SessionsDir string
	// Notices are shown after SessionOpened, such as configuration warnings.
	Notices []string
	// Source (SourceTUI or SourceRun) is recorded in a new session's sidecar
	// in SessionsDir when set.
	Source string
}

// Session is safe to use from any goroutine. All state lives on one internal
// goroutine, which is also the only writer of Events.
type Session struct {
	id   string
	eng  engine.Engine
	caps engine.Capabilities
	in   chan any
	out  chan core.Event
	ctx  context.Context
	stop context.CancelFunc
	done chan struct{}

	// Owned by the loop goroutine.
	settings Settings
	state    State
	queue    []core.UserInput
	sent     map[string]bool
	// live are messages sent into a running run, in order; the ones it had
	// not read when it ended go out again with the next run.
	live []core.UserInput
	// startSteers are steers made while a live-input run was starting.
	startSteers          []core.UserInput
	run                  engine.Run
	restartAfterStop     bool
	interruptWhenStarted bool
	closeReply           chan error
	// compactPending is a /compact the engine has not started yet.
	compactPending bool
	hooks          hookState
}

// Open starts a session. Its first event is SessionOpened.
func Open(ctx context.Context, eng engine.Engine, opts Options) (*Session, error) {
	if err := opts.Settings.Validate(); err != nil {
		return nil, fmt.Errorf("failed to open session: %w", err)
	}
	id := opts.ID
	if id == "" {
		id = uuid.NewString()
	}
	runCtx, stop := context.WithCancel(ctx)
	s := &Session{
		id: id, eng: eng, caps: eng.Capabilities(),
		in: make(chan any, eventBuffer), out: make(chan core.Event, eventBuffer),
		ctx: runCtx, stop: stop, done: make(chan struct{}),
		settings: opts.Settings, state: StateIdle, sent: map[string]bool{},
		hooks: hookState{runner: opts.Hooks, resumed: opts.Resumed, tools: map[string]core.ToolCalled{}},
	}
	s.out <- SessionOpened{At: time.Now(), ID: id, Resumed: opts.Resumed, Engine: eng.Name(), Settings: opts.Settings}
	if opts.Instructions != nil {
		loaded := *opts.Instructions
		loaded.At = time.Now()
		s.out <- loaded
	}
	if opts.SessionsDir != "" {
		s.hooks.transcriptPath = filepath.Join(opts.SessionsDir, id+".session.jsonl")
		if opts.Source != "" && !opts.Resumed {
			if err := writeSidecar(opts.SessionsDir, id, Sidecar{Source: opts.Source, Created: time.Now().UTC()}); err != nil {
				s.out <- Notice{At: time.Now(), Level: LevelWarning, Message: err.Error()}
			}
		}
	}
	for _, n := range opts.Notices {
		s.out <- Notice{At: time.Now(), Level: LevelWarning, Message: n}
	}
	s.startHooks(opts.Resumed)
	go s.loop()

	return s, nil
}

// ID is the runner session ID; resume the session with it.
func (s *Session) ID() string { return s.id }

// Capabilities are the engine's live capabilities.
func (s *Session) Capabilities() engine.Capabilities { return s.caps }

// Events is the ordered stream of run and session events. It is closed after Close.
func (s *Session) Events() <-chan core.Event { return s.out }

// Submit sends a message: it starts a run when idle and queues it while a run
// is live. Queued messages go out together when the run ends.
func (s *Session) Submit(text string) (core.UserInput, error) {
	return s.submit(text, false)
}

// SteerNow sends a message to the agent now. With live input it reaches the
// running agent; otherwise the run is interrupted and a new run starts with
// the queue and this message.
func (s *Session) SteerNow(text string) (core.UserInput, error) {
	return s.submit(text, true)
}

// Interrupt stops the live run gracefully. Queued messages stay queued.
func (s *Session) Interrupt() error {
	_, err := call[struct{}](s, cmdInterrupt{})

	return err
}

// Withdraw takes a queued message back before it is sent. It reports whether
// the message was still queued.
func (s *Session) Withdraw(id string) (bool, error) {
	return call[bool](s, cmdWithdraw{id: id})
}

// SetSettings changes the settings and reports when they apply.
func (s *Session) SetSettings(settings Settings) (Applied, error) {
	if err := settings.Validate(); err != nil {
		return "", err
	}

	return call[Applied](s, cmdSettings{settings: settings})
}

// Close interrupts a live run, waits for it to end, and closes Events.
func (s *Session) Close() error {
	reply := make(chan error, 1)
	select {
	case s.in <- cmdClose{reply: reply}:
	case <-s.done:
		return nil
	}
	select {
	case err := <-reply:
		<-s.done

		return err
	case <-s.done:
		return nil
	}
}

func (s *Session) submit(text string, steer bool) (core.UserInput, error) {
	if strings.TrimSpace(text) == "" {
		return core.UserInput{}, errors.New("the message is empty")
	}

	return call[core.UserInput](s, cmdSubmit{text: text, steer: steer})
}
