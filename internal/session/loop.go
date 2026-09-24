package session

import (
	"fmt"
	"time"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/hooks"
)

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
	evHook struct {
		result hooks.Result
		at     time.Time
	}
	evStartChecked  struct{ decision hooks.Decision }
	evPromptChecked struct {
		id       string
		decision hooks.Decision
	}
	evStopChecked struct {
		gen      int
		decision hooks.Decision
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
	if s.hooks.jobs != nil {
		defer close(s.hooks.jobs)
	}
	for msg := range s.in {
		switch m := msg.(type) {
		case request:
			value, err := s.handle(m.cmd)
			m.reply <- reply{value: value, err: err}
		case cmdClose:
			if s.run == nil && s.state != StateStarting {
				s.sessionEnd(s.ctx)
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
		case evHook:
			r := m.result
			s.emit(HookRan{
				At: m.at, Event: string(r.Hook.Event), Command: r.Hook.Command, Source: string(r.Hook.Source),
				Outcome: string(r.Outcome), Reason: r.Reason, Duration: r.Duration,
			})
		case evStartChecked:
			s.showMessages(m.decision)
			s.hooks.startContext = m.decision.Context
		case evPromptChecked:
			s.onPromptChecked(m)
		case evStopChecked:
			s.onStopChecked(m)
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
		return s.onWithdraw(c.id), nil
	case cmdSettings:
		return s.onSettings(c.settings), nil
	case cmdCompact:
		return struct{}{}, s.onCompact()
	}

	return nil, fmt.Errorf("unknown session command %T", cmd)
}
