package state_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"
	uaharness "github.com/viktordanov/uagent/harness"
	"github.com/viktordanov/uagent/testing/fixtures"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

var t0 = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

func settings() session.Settings {
	return session.Settings{Provider: "openai-codex", Model: "gpt-6-sol", Effort: "high", Workspace: "/workspace"}
}

// apply reduces events and intents in order and returns the effects.
func apply(s state.State, evs ...any) (state.State, []state.Effect) {
	var all []state.Effect
	for _, ev := range evs {
		var effects []state.Effect
		s, effects = state.Reduce(s, ev)
		all = append(all, effects...)
	}

	return s, all
}

func fixtureEvents(t *testing.T, name string) []any {
	t.Helper()
	var out []any
	require.NoError(t, uaharness.ReadEvents(bytes.NewReader(fixtures.RunnerOutput(name)), func(e core.Event) { out = append(out, e) }))

	return out
}

func kinds(s state.State) []state.Kind {
	out := make([]state.Kind, 0, len(s.Items))
	for _, it := range s.Items {
		out = append(out, it.Kind)
	}

	return out
}

func opened() state.State {
	s, _ := apply(state.New(t0), session.SessionOpened{At: t0, ID: "sess-1", Engine: "process", Settings: settings()})

	return s
}

func TestReduce_ARunFromTheFixture(t *testing.T) {
	s := opened()
	userID := "7cb42beb-329b-4c7b-8c2f-abced68ef095" // the fixture's message ID
	s, _ = apply(s,
		session.InputQueued{At: t0, Input: core.UserInput{ID: userID, Text: "Run `ls` and `cat a.txt`"}},
		session.InputSent{At: t0, IDs: []string{userID}},
		core.RunStarted{At: t0, RunID: "run-1", SessionID: "sess-1"},
	)
	assert.True(t, s.Busy)
	require.NotNil(t, s.Live)

	s, _ = apply(s, fixtureEvents(t, "simple.jsonl")...)
	s, _ = apply(s,
		core.RunFinished{At: t0, Result: core.Result{
			Request: core.Request{RunID: "run-1"}, Status: core.StatusOK, Wall: 5 * time.Second,
			Stats: core.Stats{Turns: 2, ToolCalls: 2, MaxParallelTools: 2, Tokens: core.Tokens{InputTokens: 1127, OutputTokens: 55}},
		}},
		session.Idle{At: t0},
	)

	assert.Equal(t, []state.Kind{
		state.KindUser, state.KindRun, state.KindTurn, state.KindTool, state.KindTool, state.KindTurn, state.KindAssistant, state.KindFinish,
	}, kinds(s))
	msg, _ := s.Item("msg:" + userID)
	assert.Equal(t, state.InputDelivered, msg.Input, "the runner's echo marks the message delivered")
	run, _ := s.Item("run:run-1")
	assert.Equal(t, core.StatusOK, run.Status)
	assert.Equal(t, int64(1182), run.Tokens)
	for _, it := range s.Items {
		if it.Kind == state.KindTool {
			assert.Equal(t, state.ToolOK, it.Tool, it.Label)
			assert.Equal(t, "exit 0", it.Detail)
		}
		if it.Kind == state.KindAssistant {
			assert.True(t, it.Final)
			assert.Equal(t, "hello", it.Text)
		}
	}
	assert.False(t, s.Busy)
	assert.Nil(t, s.Live)
	assert.Equal(t, 1, s.Totals.Runs)
	assert.Equal(t, 2, s.Totals.MaxParallel)
}

func TestReduce_ToolsKeepTheirPlace(t *testing.T) {
	s := opened()
	s, _ = apply(s,
		core.RunStarted{At: t0, RunID: "r"},
		core.TurnStarted{At: t0, Turn: 1},
		core.ModelResponded{At: t0.Add(time.Second), Turn: 1},
		core.ToolCalled{At: t0.Add(time.Second), CallID: "slow", Name: "Bash", Label: "go test ./..."},
		core.ToolStarted{At: t0.Add(time.Second), CallID: "slow", OpID: "op"},
		core.TurnStarted{At: t0.Add(2 * time.Second), Turn: 2},
		core.ModelResponded{At: t0.Add(3 * time.Second), Turn: 2},
		core.AssistantMessage{At: t0.Add(3 * time.Second), Text: "patching while the tests run"},
		core.ToolFinished{At: t0.Add(9 * time.Second), CallID: "slow", OpID: "op", OK: false, Detail: "exit 1", Duration: 8 * time.Second},
	)

	assert.Equal(t, []state.Kind{state.KindRun, state.KindTurn, state.KindTool, state.KindTurn, state.KindAssistant}, kinds(s),
		"a tool that finishes after later turns updates its original row")
	tool, _ := s.Item("call:slow")
	assert.Equal(t, state.ToolFailed, tool.Tool)
	assert.Equal(t, 8*time.Second, tool.Duration)

	t.Run("tools still running when the run ends are marked stopped", func(t *testing.T) {
		s, _ := apply(s,
			core.ToolCalled{CallID: "hung", Name: "Bash"},
			core.ToolStarted{CallID: "hung", OpID: "op2"},
			core.RunFinished{Result: core.Result{Request: core.Request{RunID: "r"}, Status: core.StatusInterrupted}},
		)
		hung, _ := s.Item("call:hung")
		assert.Equal(t, state.ToolStopped, hung.Tool)
	})
}

func TestReduce_Queue(t *testing.T) {
	s := opened()
	s, _ = apply(s,
		session.InputQueued{Input: core.UserInput{ID: "a", Text: "first"}},
		session.InputSent{IDs: []string{"a"}},
		core.RunStarted{RunID: "r"},
		session.InputQueued{Input: core.UserInput{ID: "b", Text: "second"}},
		session.InputQueued{Input: core.UserInput{ID: "c", Text: "third"}},
	)
	assert.Equal(t, []state.Queued{{ID: "b", Text: "second"}, {ID: "c", Text: "third"}}, s.Queue)
	_, inTranscript := s.Item("msg:b")
	assert.False(t, inTranscript, "queued messages wait in the queue, not the transcript")

	s, effects := apply(s, state.EditLastQueued{})
	assert.Equal(t, []state.Effect{state.EffWithdraw{ID: "c", Text: "third"}}, effects)
	s, _ = apply(s, session.InputWithdrawn{ID: "c"})
	assert.Equal(t, []state.Queued{{ID: "b", Text: "second"}}, s.Queue)

	s, _ = apply(s, session.InputSent{IDs: []string{"b"}}, session.InputFailed{IDs: []string{"b"}, Reason: "the run ended"})
	assert.Empty(t, s.Queue)
	b, _ := s.Item("msg:b")
	assert.Equal(t, state.InputFailed, b.Input)
}

func TestReduce_SteerTheQueue(t *testing.T) {
	queued, _ := apply(opened(),
		session.InputQueued{Input: core.UserInput{ID: "a", Text: "first"}},
		session.InputSent{IDs: []string{"a"}},
		core.RunStarted{RunID: "r"},
		session.InputQueued{Input: core.UserInput{ID: "b", Text: "second"}},
		session.InputQueued{Input: core.UserInput{ID: "c", Text: "third"}},
	)

	t.Run("ctrl+enter on an empty composer sends the queue now", func(t *testing.T) {
		s, effects := apply(queued, state.ScrollBy{Lines: 5}, state.Steer{Text: "  "})
		assert.Equal(t, []state.Effect{state.EffSteerQueued{}}, effects)
		assert.Zero(t, s.Scroll)

		s, _ = apply(s, session.InputSent{IDs: []string{"b", "c"}}, session.InputDelivered{ID: "b"})
		assert.Empty(t, s.Queue)
		b, _ := s.Item("msg:b")
		c, _ := s.Item("msg:c")
		assert.Equal(t, state.InputDelivered, b.Input)
		assert.Equal(t, state.InputSent, c.Input)
		assert.Equal(t, []string{"first", "second", "third"}, userTexts(s), "in their order, under their IDs")
	})

	t.Run("a queue an interrupt kept goes too", func(t *testing.T) {
		idle, _ := apply(queued, session.Idle{})
		_, effects := apply(idle, state.Steer{})
		assert.Equal(t, []state.Effect{state.EffSteerQueued{}}, effects)
	})

	t.Run("nothing queued, nothing sent", func(t *testing.T) {
		_, effects := apply(opened(), state.Steer{})
		assert.Empty(t, effects)
		busy, _ := apply(opened(), session.InputQueued{Input: core.UserInput{ID: "a", Text: "x"}}, session.InputSent{IDs: []string{"a"}})
		_, effects = apply(busy, state.Steer{})
		assert.Empty(t, effects)
	})

	t.Run("shell mode keeps its own ctrl+enter", func(t *testing.T) {
		_, effects := apply(queued, state.EnterShell{}, state.Steer{})
		assert.Empty(t, effects)
	})
}

func userTexts(s state.State) []string {
	var out []string
	for _, it := range s.Items {
		if it.Kind == state.KindUser {
			out = append(out, it.Text)
		}
	}

	return out
}

func TestReduce_Keys(t *testing.T) {
	busy, _ := apply(opened(), session.InputQueued{Input: core.UserInput{ID: "a", Text: "x"}})

	t.Run("enter sends; slash text runs a command", func(t *testing.T) {
		_, effects := apply(opened(), state.Submit{Text: "  fix it  "})
		assert.Equal(t, []state.Effect{state.EffSubmit{Text: "fix it"}}, effects)
		_, effects = apply(opened(), state.Steer{Text: "now"})
		assert.Equal(t, []state.Effect{state.EffSteer{Text: "now"}}, effects)
	})

	t.Run("esc twice interrupts, once only warns", func(t *testing.T) {
		s, effects := apply(busy, state.Esc{})
		assert.Empty(t, effects)
		assert.Contains(t, s.Status, "esc again")
		_, effects = apply(s, state.Esc{})
		assert.Equal(t, []state.Effect{state.EffInterrupt{}}, effects)

		expired, _ := apply(s, state.Tick{Now: t0.Add(3 * time.Second)})
		assert.Empty(t, expired.Status)
		_, effects = apply(expired, state.Esc{})
		assert.Empty(t, effects, "an expired first press does not count")
	})

	t.Run("ctrl+c quits at once when idle and asks twice when busy", func(t *testing.T) {
		_, effects := apply(opened(), state.Quit{})
		assert.Equal(t, []state.Effect{state.EffQuit{}}, effects)
		_, effects = apply(busy, state.Quit{})
		assert.Empty(t, effects)
		_, effects = apply(busy, state.Quit{}, state.Quit{})
		assert.Equal(t, []state.Effect{state.EffQuit{}}, effects)
	})

	t.Run("alt+, and alt+. step the effort within bounds", func(t *testing.T) {
		_, effects := apply(opened(), state.StepEffort{Delta: 1})
		require.Len(t, effects, 1)
		assert.Equal(t, "xhigh", effects[0].(state.EffSetSettings).Settings.Effort)
		max, _ := apply(opened(), session.SettingsChanged{Settings: session.Settings{Effort: "max"}})
		_, effects = apply(max, state.StepEffort{Delta: 1})
		assert.Empty(t, effects)
	})
}

func TestReduce_Commands(t *testing.T) {
	busy, _ := apply(opened(), session.InputQueued{Input: core.UserInput{ID: "a", Text: "x"}})
	tests := map[string]struct {
		from    state.State
		text    string
		effects []state.Effect
		notice  string
	}{
		"model":                  {from: opened(), text: "/model gpt-6-astra", effects: []state.Effect{state.EffSetSettings{Settings: withModel("gpt-6-astra")}}},
		"bad model":              {from: opened(), text: "/model -x", notice: "starts with a dash"},
		"effort":                 {from: busy, text: "/effort low", effects: []state.Effect{state.EffSetSettings{Settings: withEffort("low")}}},
		"bad effort":             {from: opened(), text: "/effort huge", notice: "effort: high"},
		"fast unavailable":       {from: opened(), text: "/fast", notice: "embedded engine"},
		"new":                    {from: opened(), text: "/new", effects: []state.Effect{state.EffOpenSession{}}},
		"clear needs compaction": {from: opened(), text: "/clear", notice: "/clear needs the embedded engine"},
		"new while busy":         {from: busy, text: "/new", notice: "waits until the agent is idle"},
		"resume opens list":      {from: opened(), text: "/resume", effects: []state.Effect{state.EffLoadSessions{}}},
		"stop":                   {from: busy, text: "/stop", effects: []state.Effect{state.EffInterrupt{}}},
		"unknown":                {from: opened(), text: "/nope", notice: "unknown command /nope"},
		"help":                   {from: opened(), text: "/help", notice: "/model <id>"},
		"status":                 {from: opened(), text: "/status", notice: "session sess-1 · process engine", effects: []state.Effect{state.EffLoadActivity{}, state.EffLoadUsage{Reason: state.UsageStatus}}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			s, effects := apply(tc.from, state.Submit{Text: tc.text})

			assert.Equal(t, tc.effects, effects)
			if tc.notice != "" {
				last := s.Items[len(s.Items)-1]
				if name == "status" {
					last = s.Items[len(s.Items)-4]
				}
				assert.Equal(t, state.KindNotice, last.Kind)
				assert.Contains(t, last.Text, tc.notice)
			}
		})
	}
}

// TestReduce_StatusNamesTheEngineGaps: /status lists what the engine does
// not run, from its capabilities, and says nothing for one that runs all.
func TestReduce_StatusNamesTheEngineGaps(t *testing.T) {
	s, _ := apply(opened(), state.Submit{Text: "/status"})
	last := s.Items[len(s.Items)-1]
	assert.Equal(t, "the process engine runs without: live input, live settings, fast mode, compaction, PreCompact hooks, command rules, "+
		"prompt rules, approvals, Auto mode, PermissionRequest hooks, PreToolUse hooks, MCP servers, subagents, apply_patch, Codex skills, /context, pasted images", last.Text)

	all := opened()
	all.Caps = engine.Capabilities{
		LiveInput: true, LiveEffort: true, LiveModel: true, ServiceTier: true, Compaction: true, LiveMode: true, Rules: true,
		Approvals: true, ToolHooks: true, MCP: true, Subagents: true, ApplyPatch: true, CodexSkills: true, ContextUsage: true, Images: true,
	}
	s, _ = apply(all, state.Submit{Text: "/status"})
	assert.Equal(t, "instructions: none", s.Items[len(s.Items)-1].Text)
}

func TestReduce_HistoryAndSessions(t *testing.T) {
	s := opened()
	s, _ = apply(s, state.Submit{Text: "/status"})
	history := []session.LoadedRun{{
		Record: uaharness.RunRecord{Complete: true, Result: core.Result{
			Request: core.Request{RunID: "old-run", SessionID: "sess-2"}, Status: core.StatusOK, StartedAt: t0, Wall: time.Second,
		}},
		Events: []core.Event{
			core.UserMessage{ID: "m1", Text: "earlier question"},
			core.AssistantMessage{Text: "earlier answer", Final: true},
		},
	}}

	s, _ = apply(s, state.HistoryLoaded{SessionID: "sess-2", Runs: history})
	s, _ = apply(s, session.SessionOpened{ID: "sess-2", Resumed: true, Settings: settings()})

	assert.Equal(t, []state.Kind{state.KindRun, state.KindUser, state.KindAssistant, state.KindFinish}, kinds(s), "resuming keeps the loaded history")
	run, _ := s.Item("run:old-run")
	assert.Equal(t, core.StatusOK, run.Status)
	assert.True(t, s.Resumed)

	s, _ = apply(s, session.SessionOpened{ID: "sess-3", Settings: settings()})
	assert.Empty(t, s.Items, "a different session starts an empty transcript")

	t.Run("picker filters and chooses", func(t *testing.T) {
		infos := []session.Info{{ID: "aaaa1111", FirstPrompt: "fix the build"}, {ID: "bbbb2222", FirstPrompt: "write docs"}}
		p, _ := apply(s, state.SessionsLoaded{Sessions: infos, Local: infos[:1]})
		assert.Equal(t, state.ModePicker, p.Mode)
		assert.Len(t, p.Picker.Filtered(), 1, "this directory's sessions first, as in Codex")
		p, _ = apply(p, state.PickerToggleAll{})
		assert.Len(t, p.Picker.Filtered(), 2, "tab shows every directory")
		p, _ = apply(p, state.PickerType{Text: "d"}, state.PickerType{Text: "o"}, state.PickerType{Text: "c"})
		assert.Len(t, p.Picker.Filtered(), 1)
		p, effects := apply(p, state.PickerChoose{})
		assert.Equal(t, []state.Effect{state.EffOpenSession{ID: "bbbb2222"}}, effects)
		assert.Equal(t, state.ModeChat, p.Mode)
	})
}

func withModel(m string) session.Settings {
	s := settings()
	s.Model = m

	return s
}

func withEffort(e string) session.Settings {
	s := settings()
	s.Effort = e

	return s
}

func TestReduce_StartupPicker(t *testing.T) {
	s, _ := apply(state.New(t0), state.SessionsLoaded{})

	s, effects := apply(s, state.PickerCancel{})

	assert.Equal(t, state.ModeChat, s.Mode)
	assert.Equal(t, []state.Effect{state.EffOpenSession{}}, effects, "leaving the startup picker starts a new session")
}
