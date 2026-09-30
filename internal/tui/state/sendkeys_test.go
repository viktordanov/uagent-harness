package state_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// press maps a send key to its intent and reduces it, as the shell does;
// nil effects and a false ok mean the key sent nothing.
func press(s state.State, key, draft string) (state.State, []state.Effect, bool) {
	intent := s.SendIntent(key, draft)
	if intent == nil {
		return s, nil, false
	}
	s, effects := apply(s, intent)

	return s, effects, true
}

// working is a session whose agent works on "start", with one message
// queued behind it.
func working(t *testing.T, keys state.Keys) state.State {
	t.Helper()
	s := opened()
	s.Keys = keys
	s, _ = apply(s,
		session.InputQueued{Input: core.UserInput{ID: "a", Text: "start"}},
		session.InputSent{IDs: []string{"a"}},
		core.RunStarted{RunID: "r"},
		session.InputQueued{Input: core.UserInput{ID: "b", Text: "queued"}},
	)
	require.True(t, s.Working())

	return s
}

func idle(keys state.Keys) state.State {
	s := opened()
	s.Keys = keys

	return s
}

var (
	enhancedKeys = state.Keys{Disambiguated: true}
	plainKeys    = state.Keys{}
)

func TestSendKeys_TheTerminalPicksTheBindings(t *testing.T) {
	s := state.New(t0)
	assert.True(t, s.Keys.EnterSteers(), "until the terminal answers the query, plain keys: they work everywhere")
	s, _ = apply(s, state.KeyboardReported{Disambiguates: true})
	assert.False(t, s.Keys.EnterSteers(), "the terminal tells ctrl+enter from enter")
	s, _ = apply(s, state.KeyboardReported{Disambiguates: false})
	assert.True(t, s.Keys.EnterSteers())

	for _, tc := range []struct {
		steer         state.SteerKey
		disambiguated bool
		enterSteers   bool
	}{
		{state.SteerKeyAuto, false, true},
		{state.SteerKeyAuto, true, false},
		{state.SteerKeyEnter, true, true},
		{state.SteerKeyCtrlEnter, false, false},
	} {
		assert.Equal(t, tc.enterSteers, state.Keys{Steer: tc.steer, Disambiguated: tc.disambiguated}.EnterSteers(), "%+v", tc)
	}
}

func TestSendKeys_ParseSteerKey(t *testing.T) {
	for in, want := range map[string]state.SteerKey{"": state.SteerKeyAuto, "auto": state.SteerKeyAuto, "ctrl+enter": state.SteerKeyCtrlEnter, "enter": state.SteerKeyEnter} {
		got, err := state.ParseSteerKey(in)
		require.NoError(t, err)
		assert.Equal(t, want, got, in)
	}
	_, err := state.ParseSteerKey("tab")
	assert.ErrorContains(t, err, `steer_key is "tab"`)
}

func TestSendKeys_Enhanced(t *testing.T) {
	busy := working(t, enhancedKeys)

	_, effects, _ := press(busy, state.KeyEnter, "later")
	assert.Equal(t, []state.Effect{state.EffSubmit{Text: "later"}}, effects, "enter queues")
	for _, key := range []string{state.KeyCtrlEnter, state.KeyAltEnter} {
		_, effects, _ = press(busy, key, "now")
		assert.Equal(t, []state.Effect{state.EffSteer{Text: "now"}}, effects, "%s sends now", key)
		_, effects, _ = press(busy, key, "")
		assert.Equal(t, []state.Effect{state.EffSteerQueued{}}, effects, "%s on an empty composer sends the queue", key)
	}
	_, _, sent := press(busy, state.KeyEnter, "  ")
	assert.False(t, sent, "an empty enter sends nothing")
	_, _, sent = press(busy, state.KeyTab, "x")
	assert.False(t, sent, "tab is the composer's")

	_, effects, _ = press(idle(enhancedKeys), state.KeyEnter, "hi")
	assert.Equal(t, []state.Effect{state.EffSubmit{Text: "hi"}}, effects)
}

func TestSendKeys_Plain(t *testing.T) {
	busy := working(t, plainKeys)

	_, effects, _ := press(busy, state.KeyEnter, "now")
	assert.Equal(t, []state.Effect{state.EffSteer{Text: "now"}}, effects, "enter sends now while the agent works")
	_, effects, _ = press(busy, state.KeyTab, "later")
	assert.Equal(t, []state.Effect{state.EffSubmit{Text: "later"}}, effects, "tab queues")
	_, effects, _ = press(busy, state.KeyEnter, "")
	assert.Equal(t, []state.Effect{state.EffSteerQueued{}}, effects, "enter on an empty composer sends the queue now")
	for _, key := range []string{state.KeyCtrlEnter, state.KeyAltEnter} {
		_, effects, _ = press(busy, key, "now")
		assert.Equal(t, []state.Effect{state.EffSteer{Text: "now"}}, effects, "%s still sends now", key)
	}
	_, _, sent := press(busy, state.KeyTab, " ")
	assert.False(t, sent, "tab on an empty composer is the composer's")

	still := idle(plainKeys)
	_, effects, _ = press(still, state.KeyEnter, "hi")
	assert.Equal(t, []state.Effect{state.EffSubmit{Text: "hi"}}, effects, "idle, enter sends")
	_, _, sent = press(still, state.KeyTab, "hi")
	assert.False(t, sent, "idle, tab is the composer's")
	_, effects, _ = press(still, state.KeyEnter, "")
	assert.Empty(t, effects, "nothing queued, nothing sent")

	kept, _ := apply(busy, session.Idle{})
	_, effects, _ = press(kept, state.KeyEnter, "")
	assert.Equal(t, []state.Effect{state.EffSteerQueued{}}, effects, "a queue an interrupt kept goes too")
}

func TestSendKeys_PlainShellMode(t *testing.T) {
	busy, _ := apply(working(t, plainKeys), state.EnterShell{})
	_, _, sent := press(busy, state.KeyTab, "ls")
	assert.False(t, sent, "a command runs at once; there is nothing to queue")
	_, effects, _ := press(busy, state.KeyEnter, "ls")
	require.Len(t, effects, 1)
	assert.IsType(t, state.EffShell{}, effects[0])
}

func TestSendKeys_HintsAndHelp(t *testing.T) {
	assert.Equal(t, "enter send now · tab queue", plainKeys.SendHint(true))
	assert.Equal(t, "enter send · ctrl+j new line", plainKeys.SendHint(false))
	assert.Equal(t, "enter queue · ctrl+enter send now", enhancedKeys.SendHint(true))
	assert.Equal(t, "enter send · ctrl+enter now", enhancedKeys.SendHint(false))
	assert.Equal(t, "enter", plainKeys.SendNowKey())
	assert.Equal(t, "ctrl+enter", enhancedKeys.SendNowKey())

	s, _ := apply(idle(plainKeys), state.Submit{Text: "/help"})
	assert.Contains(t, s.Items[len(s.Items)-1].Text, "tab queue while the agent works")
	s, _ = apply(idle(enhancedKeys), state.Submit{Text: "/help"})
	assert.Contains(t, s.Items[len(s.Items)-1].Text, "ctrl+enter or alt+enter send now")
}

// TestSendKeys_AgentView: the view's composer talks to the viewed agent, so
// whether enter sends now follows that agent's run, and the view keeps the
// terminal's bindings, also an answer that arrives while it is open.
func TestSendKeys_AgentView(t *testing.T) {
	s := idle(plainKeys)
	s, _ = apply(s, state.AgentViewOpened{ID: "subagent-1", Nickname: "Ada"})
	assert.False(t, s.Working(), "the viewed agent is idle")
	_, effects, _ := press(s, state.KeyEnter, "hi")
	assert.Equal(t, []state.Effect{state.EffAgentSend{ID: "subagent-1", Text: "hi"}}, effects)

	s, _ = apply(s, state.AgentEvents{ID: "subagent-1", Events: []core.Event{core.RunStarted{RunID: "r"}}})
	require.True(t, s.Working())
	_, effects, _ = press(s, state.KeyEnter, "faster")
	assert.Equal(t, []state.Effect{state.EffAgentSend{ID: "subagent-1", Text: "faster", Now: true}}, effects, "enter steers the working agent")
	_, effects, _ = press(s, state.KeyTab, "later")
	assert.Equal(t, []state.Effect{state.EffAgentSend{ID: "subagent-1", Text: "later"}}, effects, "tab gives it the message for after")

	s, _ = apply(s, state.KeyboardReported{Disambiguates: true})
	assert.Equal(t, s.Keys, s.View.St.Keys)
	_, effects, _ = press(s, state.KeyEnter, "later")
	assert.Equal(t, []state.Effect{state.EffAgentSend{ID: "subagent-1", Text: "later"}}, effects)
}
