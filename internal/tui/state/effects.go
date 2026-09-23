package state

import "github.com/viktordanov/uagent-harness/internal/session"

// Effect is work for the shell to do. The reducer never does I/O.
type Effect interface{ effect() }

type (
	// EffSubmit sends a message (it queues while the agent works).
	EffSubmit struct{ Text string }
	// EffSteer sends a message now.
	EffSteer struct{ Text string }
	// EffInterrupt stops the live run; queued messages stay.
	EffInterrupt struct{}
	// EffWithdraw takes a queued message back into the composer.
	EffWithdraw struct{ ID, Text string }
	// EffSetSettings changes the session settings.
	EffSetSettings struct{ Settings session.Settings }
	// EffLoadSessions lists sessions for the picker.
	EffLoadSessions struct{}
	// EffOpenSession closes the current session and opens another ("" for a new one).
	EffOpenSession struct{ ID string }
	// EffQuit closes the session and exits.
	EffQuit struct{}
)

func (EffSubmit) effect()       {}
func (EffSteer) effect()        {}
func (EffInterrupt) effect()    {}
func (EffWithdraw) effect()     {}
func (EffSetSettings) effect()  {}
func (EffLoadSessions) effect() {}
func (EffOpenSession) effect()  {}
func (EffQuit) effect()         {}
