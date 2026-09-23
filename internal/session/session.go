// Package session is the long-lived object the TUI and `uah run` talk to. A
// session owns its settings, a queue of messages, and at most one live run,
// and it merges run events and its own events into one ordered stream.
package session

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
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
	live                 []core.UserInput
	run                  engine.Run
	restartAfterStop     bool
	interruptWhenStarted bool
	closeReply           chan error
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
	}
	s.out <- SessionOpened{At: time.Now(), ID: id, Resumed: opts.Resumed, Engine: eng.Name(), Settings: opts.Settings}
	if opts.Instructions != nil {
		loaded := *opts.Instructions
		loaded.At = time.Now()
		s.out <- loaded
	}
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

// Commands and internal events handled by the loop.
type (
	cmdSubmit struct {
		text  string
		steer bool
	}
	cmdInterrupt struct{}
	cmdWithdraw  struct{ id string }
	cmdSettings  struct{ settings Settings }
	cmdClose     struct{ reply chan error }
	request      struct {
		cmd   any
		reply chan reply
	}
	reply struct {
		value any
		err   error
	}
	evStarted struct {
		run    engine.Run
		err    error
		inputs []core.UserInput
	}
	evRun   struct{ event core.Event }
	evEnded struct {
		result core.Result
		err    error
	}
)

func call[T any](s *Session, cmd any) (T, error) {
	var zero T
	r := request{cmd: cmd, reply: make(chan reply, 1)}
	select {
	case s.in <- r:
	case <-s.done:
		return zero, ErrClosed
	}
	select {
	case res := <-r.reply:
		if res.err != nil {
			return zero, res.err
		}
		v, _ := res.value.(T)

		return v, nil
	case <-s.done:
		return zero, ErrClosed
	}
}

func (s *Session) loop() {
	defer close(s.done)
	defer close(s.out)
	defer s.stop()
	for msg := range s.in {
		switch m := msg.(type) {
		case request:
			value, err := s.handle(m.cmd)
			m.reply <- reply{value: value, err: err}
		case cmdClose:
			if s.run == nil && s.state != StateStarting {
				s.state = StateClosed
				m.reply <- nil

				return
			}
			s.closeReply = m.reply
			s.interruptLive()
		case evStarted:
			if s.onStarted(m) {
				return
			}
		case evRun:
			s.onRunEvent(m.event)
		case evEnded:
			if s.onEnded(m) {
				return
			}
		}
	}
}

func (s *Session) handle(cmd any) (any, error) {
	if s.closeReply != nil {
		return nil, ErrClosed
	}
	switch c := cmd.(type) {
	case cmdSubmit:
		return s.onSubmit(c), nil
	case cmdInterrupt:
		s.restartAfterStop = false
		s.interruptLive()

		return struct{}{}, nil
	case cmdWithdraw:
		i := slices.IndexFunc(s.queue, func(in core.UserInput) bool { return in.ID == c.id })
		if i < 0 {
			return false, nil
		}
		s.queue = slices.Delete(s.queue, i, i+1)
		s.emit(InputWithdrawn{At: time.Now(), ID: c.id})

		return true, nil
	case cmdSettings:
		return s.onSettings(c.settings), nil
	}

	return nil, fmt.Errorf("unknown session command %T", cmd)
}

func (s *Session) onSubmit(c cmdSubmit) core.UserInput {
	input := core.UserInput{ID: uuid.NewString(), Text: c.text}
	s.emit(InputQueued{At: time.Now(), Input: input})
	switch s.state {
	case StateIdle:
		// Messages left queued by an interrupt go out first, in order.
		inputs := slices.Concat(s.queue, []core.UserInput{input})
		s.queue = nil
		s.startRun(inputs)
	case StateRunning:
		if c.steer && s.caps.LiveInput {
			if err := s.run.Send(input); err == nil {
				s.markSent([]core.UserInput{input})
				s.live = append(s.live, input)

				return input
			}
		}
		s.queue = append(s.queue, input)
		if c.steer {
			s.restartAfterStop = true
			s.interruptLive()
		}
	case StateStarting, StateStopping:
		s.queue = append(s.queue, input)
		if c.steer {
			s.restartAfterStop = true
			s.interruptLive()
		}
	case StateClosed:
	}

	return input
}

func (s *Session) onSettings(next Settings) Applied {
	prev := s.settings
	s.settings = next
	applied := AppliedNextRun
	if s.state == StateRunning && s.run != nil {
		live := true
		if next.Effort != prev.Effort {
			live = live && s.run.SetEffort(next.Effort) == nil
		}
		if next.Model != prev.Model {
			live = live && s.run.SetModel(next.Model) == nil
		}
		if next.ServiceTier != prev.ServiceTier {
			live = live && s.run.SetServiceTier(next.ServiceTier) == nil
		}
		onlyLiveFields := next.Provider == prev.Provider && next.Workspace == prev.Workspace && next.BaseURL == prev.BaseURL
		changed := next.Effort != prev.Effort || next.Model != prev.Model || next.ServiceTier != prev.ServiceTier
		if live && onlyLiveFields && changed {
			applied = AppliedLive
		}
	}
	s.emit(SettingsChanged{At: time.Now(), Settings: next, Applied: applied})

	return applied
}

// interruptLive stops the live run, or the run that is starting.
func (s *Session) interruptLive() {
	switch s.state {
	case StateRunning:
		s.state = StateStopping
		s.run.Interrupt()
	case StateStarting:
		s.interruptWhenStarted = true
	case StateIdle, StateStopping, StateClosed:
	}
}

func (s *Session) startRun(inputs []core.UserInput) {
	s.state = StateStarting
	s.markSent(inputs)
	req := s.settings.request(s.id, inputs)
	tier := s.settings.ServiceTier
	sink := func(e core.Event) { s.in <- evRun{event: e} }
	go func() {
		run, err := s.eng.Start(s.ctx, req, engine.Options{ServiceTier: tier}, sink)
		s.in <- evStarted{run: run, err: err, inputs: inputs}
	}()
}

func (s *Session) markSent(inputs []core.UserInput) {
	ids := make([]string, 0, len(inputs))
	for _, in := range inputs {
		s.sent[in.ID] = true
		ids = append(ids, in.ID)
	}
	s.emit(InputSent{At: time.Now(), IDs: ids})
}

// onStarted handles a run that started or failed to start, and reports
// whether the session closed.
func (s *Session) onStarted(m evStarted) bool {
	if m.err != nil {
		ids := make([]string, 0, len(m.inputs))
		for _, in := range m.inputs {
			delete(s.sent, in.ID)
			ids = append(ids, in.ID)
		}
		s.emit(InputFailed{At: time.Now(), IDs: ids, Reason: m.err.Error()})
		s.emit(Notice{At: time.Now(), Level: "error", Message: m.err.Error()})
		s.interruptWhenStarted, s.restartAfterStop = false, false
		s.state = StateIdle
		if s.closeReply != nil {
			s.finishClose(nil)

			return true
		}
		s.emit(Idle{At: time.Now()})

		return false
	}
	s.run = m.run
	s.state = StateRunning
	go func() {
		result, err := m.run.Wait()
		s.in <- evEnded{result: result, err: err}
	}()
	if s.interruptWhenStarted || s.closeReply != nil {
		s.interruptWhenStarted = false
		s.interruptLive()
	}

	return false
}

func (s *Session) onRunEvent(e core.Event) {
	s.emit(e)
	if m, ok := e.(core.UserMessage); ok && s.sent[m.ID] {
		delete(s.sent, m.ID)
		s.emit(InputDelivered{At: time.Now(), ID: m.ID})
	}
}

// onEnded handles the end of a run and reports whether the session closed.
func (s *Session) onEnded(m evEnded) bool {
	s.run = nil
	if m.err != nil {
		s.emit(Notice{At: time.Now(), Level: "error", Message: m.err.Error()})
	}
	userStopped := s.state == StateStopping && !s.restartAfterStop
	if s.closeReply == nil {
		s.requeueUnread(userStopped)
	}
	s.live = nil
	if len(s.sent) > 0 {
		ids := make([]string, 0, len(s.sent))
		for id := range s.sent {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		clear(s.sent)
		s.emit(InputFailed{At: time.Now(), IDs: ids, Reason: fmt.Sprintf("the run ended (%s) before the runner accepted them", m.result.Status)})
	}
	if s.closeReply != nil {
		s.finishClose(m.err)

		return true
	}
	s.state = StateIdle
	s.restartAfterStop = false
	if len(s.queue) > 0 && !userStopped {
		inputs := s.queue
		s.queue = nil
		s.startRun(inputs)

		return false
	}
	s.emit(Idle{At: time.Now()})

	return false
}

// requeueUnread puts messages sent into the run that it never read back at
// the front of the queue. After a user stop they show as queued again.
func (s *Session) requeueUnread(userStopped bool) {
	var unread []core.UserInput
	for _, in := range s.live {
		if s.sent[in.ID] {
			delete(s.sent, in.ID)
			unread = append(unread, in)
		}
	}
	s.queue = slices.Concat(unread, s.queue)
	if userStopped {
		for _, in := range unread {
			s.emit(InputQueued{At: time.Now(), Input: in})
		}
	}
}

func (s *Session) finishClose(err error) {
	s.state = StateClosed
	s.closeReply <- err
}

// emit sends on the ordered output stream. Only the loop goroutine calls it.
func (s *Session) emit(e core.Event) {
	s.out <- e
}
