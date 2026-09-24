package session

import (
	"fmt"
	"slices"
	"time"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
)

// startRun starts a run with the messages. The engine starts it on another
// goroutine and the loop gets evStarted.
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

// markSent records messages that went to the runner until it echoes them.
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
		for _, in := range slices.Concat(m.inputs, s.startSteers) {
			delete(s.sent, in.ID)
			ids = append(ids, in.ID)
		}
		s.startSteers = nil
		s.emit(InputFailed{At: time.Now(), IDs: ids, Reason: m.err.Error()})
		s.emit(Notice{At: time.Now(), Level: LevelError, Message: m.err.Error()})
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
	steers := s.startSteers
	s.startSteers = nil
	for _, in := range steers {
		if err := s.run.Send(in); err != nil {
			s.queue = append(s.queue, in)

			continue
		}
		s.markSent([]core.UserInput{in})
		s.live = append(s.live, in)
	}
	if s.interruptWhenStarted || s.closeReply != nil {
		s.interruptWhenStarted = false
		s.interruptLive()
	}

	return false
}

// onRunEvent passes a run event on and notes what hooks and delivery need.
func (s *Session) onRunEvent(e core.Event) {
	s.emit(e)
	switch v := e.(type) {
	case core.RunStarted:
		s.hooks.runID = v.RunID
	case core.ToolCalled:
		s.hooks.tools[v.CallID] = v
	case core.ToolFinished:
		s.postToolUse(v)
	}
	if m, ok := e.(core.UserMessage); ok && s.sent[m.ID] {
		delete(s.sent, m.ID)
		s.emit(InputDelivered{At: time.Now(), ID: m.ID})
	}
}

// onEnded handles the end of a run and reports whether the session closed.
func (s *Session) onEnded(m evEnded) bool {
	s.run = nil
	if m.err != nil {
		s.emit(Notice{At: time.Now(), Level: LevelError, Message: m.err.Error()})
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
	clear(s.hooks.tools)
	if len(s.queue) > 0 && !userStopped {
		inputs := s.queue
		s.queue = nil
		s.startRun(inputs)

		return false
	}
	if len(s.hooks.checking) > 0 {
		return false // a message is on its way through its hooks
	}
	if !userStopped && s.checkStop() {
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

// finishClose runs SessionEnd hooks and answers the pending Close.
func (s *Session) finishClose(err error) {
	s.sessionEnd(s.ctx)
	s.state = StateClosed
	s.closeReply <- err
}

// emit sends on the ordered output stream. Only the loop goroutine calls it.
func (s *Session) emit(e core.Event) {
	s.out <- e
}
