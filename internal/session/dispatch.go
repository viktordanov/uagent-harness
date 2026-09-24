package session

import (
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/hooks"
)

// onSubmit accepts a message and sends it through its UserPromptSubmit hooks,
// or dispatches it directly when there are none to wait for.
func (s *Session) onSubmit(c cmdSubmit) core.UserInput {
	input := core.UserInput{ID: uuid.NewString(), Text: c.text}
	s.emit(InputQueued{At: time.Now(), Input: input})
	s.hooks.stopStreak = 0
	s.hooks.stopGen++ // a pending Stop hook no longer decides anything
	if s.hooks.jobs != nil && (s.hooks.runner.Has(hooks.UserPromptSubmit, "") || s.hooks.runner.Has(hooks.SessionStart, "")) {
		// Through the worker even without UserPromptSubmit hooks, so the
		// SessionStart context is ready and the order is kept.
		s.hooks.checking = append(s.hooks.checking, pendingInput{input: input, steer: c.steer})
		in := s.hookInput(hooks.UserPromptSubmit)
		in.Prompt = input.Text
		s.hooks.jobs <- func() {
			var d hooks.Decision
			if s.hooks.runner.Has(hooks.UserPromptSubmit, "") {
				d = s.hooks.runner.Run(s.ctx, in)
			}
			s.post(evPromptChecked{id: input.ID, decision: d})
		}

		return input
	}
	s.dispatch(input, c.steer)

	return input
}

// onWithdraw takes back a message that is waiting for its hooks or queued,
// and reports whether it found one.
func (s *Session) onWithdraw(id string) bool {
	if i := slices.IndexFunc(s.hooks.checking, func(p pendingInput) bool { return p.input.ID == id }); i >= 0 {
		s.hooks.checking = slices.Delete(s.hooks.checking, i, i+1)
		s.emit(InputWithdrawn{At: time.Now(), ID: id})

		return true
	}
	i := slices.IndexFunc(s.queue, func(in core.UserInput) bool { return in.ID == id })
	if i < 0 {
		return false
	}
	s.queue = slices.Delete(s.queue, i, i+1)
	s.emit(InputWithdrawn{At: time.Now(), ID: id})

	return true
}

// dispatch starts a run with the message, sends it live, or queues it.
func (s *Session) dispatch(input core.UserInput, steer bool) {
	switch s.state {
	case StateIdle:
		// Messages left queued by an interrupt go out first, in order.
		inputs := slices.Concat(s.queue, []core.UserInput{input})
		s.queue = nil
		s.startRun(inputs)
	case StateRunning:
		if steer && s.caps.LiveInput {
			if err := s.run.Send(input); err == nil {
				s.markSent([]core.UserInput{input})
				s.live = append(s.live, input)

				return
			}
		}
		s.queue = append(s.queue, input)
		if steer {
			s.restartAfterStop = true
			s.interruptLive()
		}
	case StateStarting:
		if steer && s.caps.LiveInput {
			s.startSteers = append(s.startSteers, input) // sent live once the run starts
			return
		}
		s.queue = append(s.queue, input)
		if steer {
			s.restartAfterStop = true
			s.interruptLive()
		}
	case StateStopping:
		s.queue = append(s.queue, input)
		if steer {
			s.restartAfterStop = true
		}
	case StateClosed:
	}
}

// onSettings applies new settings to the live run when it can, and otherwise
// from the next run.
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
	s.declinePending(true) // a run waiting for the user cannot stop
	switch s.state {
	case StateRunning:
		s.state = StateStopping
		s.run.Interrupt()
	case StateStarting:
		s.interruptWhenStarted = true
	case StateIdle, StateStopping, StateClosed:
	}
}
