package state

import (
	"time"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/session"
)

// onRunEvent folds one of the runner's events into the transcript.
func (s *State) onRunEvent(ev core.Event) {
	switch e := ev.(type) {
	case core.RunStarted:
		s.Live = &Live{RunID: e.RunID, Started: e.At}
		s.put(Item{Kind: KindRun, Key: "run:" + e.RunID, RunID: e.RunID, Status: core.StatusRunning, Started: e.At})
	case core.PreflightWarning:
		s.notice(session.LevelWarning, e.Message)
	case core.UserMessage:
		// The runner's echo delivers a message this session sent; any other
		// message comes from history or another client.
		if !s.update("msg:"+e.ID, func(it *Item) { it.Input = InputDelivered }) {
			s.put(Item{Kind: KindUser, Key: "msg:" + e.ID, Text: e.Text, Input: InputDelivered})
		}
	case core.TurnStarted:
		if s.Live != nil {
			s.Live.TurnSince = e.At
		}
		s.put(Item{Kind: KindTurn, Key: turnKey(s.Live, e.Turn), Turn: e.Turn, Pending: true, Started: e.At})
	case core.ModelResponded:
		if s.Live != nil {
			s.Live.TurnSince = time.Time{}
		}
		s.noteUsage(e)
		s.update(turnKey(s.Live, e.Turn), func(it *Item) {
			it.Pending, it.In, it.Out, it.Duration = false, e.Usage.InputTokens, e.Usage.OutputTokens, e.Duration
		})
		if e.Failure != "" {
			s.notice(session.LevelError, "model failure: "+e.Failure)
		}
	case core.ToolCalled:
		s.put(Item{Kind: KindTool, Key: "call:" + e.CallID, Name: e.Name, Label: e.Label, Tool: ToolCalled, Started: e.At})
	case core.ToolStarted:
		if s.Live != nil {
			s.Live.Tools++
		}
		s.update("call:"+e.CallID, func(it *Item) { it.Tool, it.Started = ToolRunning, e.At })
	case core.ToolFinished:
		if s.Live != nil && e.OpID != "" {
			s.Live.Tools = max(0, s.Live.Tools-1)
		}
		state := ToolOK
		if !e.OK {
			state = ToolFailed
		}
		if !s.update("call:"+e.CallID, func(it *Item) { it.Tool, it.Detail, it.Duration = state, e.Detail, e.Duration }) {
			s.put(Item{Kind: KindTool, Key: "call:" + e.CallID, Name: e.Name, Label: e.Label, Tool: state, Detail: e.Detail, Duration: e.Duration})
		}
	case core.AssistantMessage:
		s.put(Item{Kind: KindAssistant, Key: s.nextKey("text"), Text: e.Text, Final: e.Final})
	case core.ReasoningSummary:
		s.put(Item{Kind: KindReasoning, Key: s.nextKey("reason"), Text: e.Text})
	case core.RunnerError:
		s.notice(session.LevelError, e.Message)
	case core.RunFinished:
		s.finishRun(e.Result)
	}
}
