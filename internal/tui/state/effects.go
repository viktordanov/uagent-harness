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
	// EffLoadActivity counts recent runs per day for /status.
	EffLoadActivity struct{}
	// EffLoadFiles lists the workspace's files for "@".
	EffLoadFiles struct{}
	// EffSetDraft replaces the composer's text.
	EffSetDraft struct{ Text string }
	// EffOpenSession closes the current session and opens another ("" for a new one).
	EffOpenSession struct{ ID string }
	// EffCompact compacts the context; Focus is what the summary should
	// focus on ("" for none).
	EffCompact struct{ Focus string }
	// EffClear drops the context in the same session.
	EffClear struct{}
	// EffQuit closes the session and exits.
	EffQuit struct{}
)

func (EffSubmit) effect()       {}
func (EffSteer) effect()        {}
func (EffInterrupt) effect()    {}
func (EffWithdraw) effect()     {}
func (EffSetSettings) effect()  {}
func (EffLoadSessions) effect() {}
func (EffLoadActivity) effect() {}
func (EffLoadFiles) effect()    {}
func (EffSetDraft) effect()     {}
func (EffOpenSession) effect()  {}
func (EffCompact) effect()      {}
func (EffClear) effect()        {}
func (EffQuit) effect()         {}
