package bubble_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/engine/embedded"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/tui/bubble"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
	"github.com/viktordanov/uagent-harness/testing/harnesstest"
)

// liveDeps opens sessions on the embedded engine, which takes messages
// into a live run, with fakellm answering; they stream, as the TUI's do.
func liveDeps(t *testing.T, llm *fakellm.Server) bubble.Deps {
	t.Helper()
	env := harnesstest.NewEnv(t)
	getenv := func(key string) string {
		if key == "OPENAI_API_KEY" {
			return "test-key"
		}

		return env.Getenv(key)
	}
	eng := embedded.New(embedded.Config{StateDir: env.StateDir, Provider: "openai", Getenv: getenv})
	settings := session.Settings{Provider: "openai", Model: "gpt-test", Effort: "high", Workspace: env.Workspace, BaseURL: llm.URL}

	return bubble.Deps{
		Open: func(ctx context.Context, id string) (*session.Session, []session.LoadedRun, error) {
			s, err := session.Open(ctx, eng, session.Options{ID: id, Settings: settings, Interactive: true, Stream: true})

			return s, nil, err
		},
		Sessions: func() ([]session.Info, error) { return nil, nil },
	}
}

// enhanced is the terminal's answer to the keyboard enhancement query when
// it tells ctrl+enter from enter (kitty, Ghostty, WezTerm); tmux and
// Terminal.app never answer.
var enhanced = tea.KeyboardEnhancementsMsg{Flags: 1}

// TestTUI_SendTheQueueNow: while the model thinks, two messages queue; the
// send-now key on the empty composer gives both to the working agent, in
// order, before its next model request. Where the terminal tells
// ctrl+enter from enter, enter queues and ctrl+enter sends now; where it
// cannot (no answer to the query), tab queues and enter sends now.
func TestTUI_SendTheQueueNow(t *testing.T) {
	for _, tc := range []struct {
		name           string
		report         tea.Msg
		queue, sendNow tea.KeyPressMsg
		hint           string
	}{
		{"enhanced", enhanced, tea.KeyPressMsg{Code: tea.KeyEnter}, tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl}, "ctrl+enter sends now"},
		{"plain", nil, tea.KeyPressMsg{Code: tea.KeyTab}, tea.KeyPressMsg{Code: tea.KeyEnter}, "enter sends now"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gate := make(chan struct{})
			llm := fakellm.New(t, fakellm.Reply{Text: "never shown", Gate: gate})
			t.Cleanup(func() { close(gate) }) // before the server closes
			d := start(t, liveDeps(t, llm))
			if tc.report != nil {
				d.send(tc.report)
			}
			d.until("the session is open", func() bool { return d.m.(bubble.Model).Exit().SessionID != "" })

			d.typeText("start")
			d.key(tea.KeyEnter, 0)
			d.until("the model thinking", func() bool { return len(llm.Requests()) == 1 })
			d.typeText("first queued")
			d.send(tc.queue)
			d.typeText("second queued")
			d.send(tc.queue)
			d.waitFor("↳ queued: second queued")
			assert.Contains(t, d.view(), "↳ queued: first queued")
			assert.Contains(t, d.view(), tc.hint, "the queue's hint names the key that works")

			d.send(tc.sendNow)
			d.waitFor("• done")
			d.waitIdle()

			v := d.view()
			assert.NotContains(t, v, "↳ queued:", "the queue went out")
			first, second := strings.Index(v, "λ first queued"), strings.Index(v, "λ second queued")
			require.GreaterOrEqual(t, first, 0)
			assert.Greater(t, second, first, "in their order")
			reqs := llm.Requests()
			last := reqs[len(reqs)-1].UserTexts
			i := slices.Index(last, "first queued")
			require.GreaterOrEqual(t, i, 0, "the model got the queue: %q", last)
			assert.Equal(t, []string{"start", "first queued", "second queued"}, last[i-1:i+2])
			assert.NotContains(t, v, "never shown", "the held request was canceled for the new messages")
		})
	}
}

// TestTUI_EnterSteersWhereCtrlEnterCannotBeSeen: in a terminal that never
// answers the keyboard enhancement query (tmux without extended keys, where
// ctrl+enter arrives as enter), enter while the agent works gives the
// message to the live run, and tab queues one for after it.
func TestTUI_EnterSteersWhereCtrlEnterCannotBeSeen(t *testing.T) {
	gate := make(chan struct{})
	llm := fakellm.New(t, fakellm.Reply{Text: "never shown", Gate: gate})
	t.Cleanup(func() { close(gate) })
	d := start(t, liveDeps(t, llm))
	d.until("the session is open", func() bool { return d.m.(bubble.Model).Exit().SessionID != "" })

	d.typeText("start")
	d.key(tea.KeyEnter, 0)
	d.until("the model thinking", func() bool { return len(llm.Requests()) == 1 })
	d.waitFor("enter send now · tab queue") // the footer shows the keys that work
	d.typeText("after the run")
	d.key(tea.KeyTab, 0)
	d.waitFor("↳ queued: after the run")
	d.typeText("look here first")
	d.key(tea.KeyEnter, 0)
	d.until("the steered request", func() bool {
		reqs := llm.Requests()

		return len(reqs) > 1 && slices.Contains(reqs[1].UserTexts, "look here first")
	})
	assert.NotContains(t, llm.Requests()[1].UserTexts, "after the run", "the queued message waits for the run's end")
	d.waitFor("λ after the run")
	d.waitIdle()
	reqs := llm.Requests()
	assert.Contains(t, reqs[len(reqs)-1].UserTexts, "after the run", "then the queue goes out")
}
