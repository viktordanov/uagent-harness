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
// it tells shift+enter and ctrl+enter from enter (kitty, Ghostty, WezTerm);
// tmux and Terminal.app never answer. It changes no send key.
var enhanced = tea.KeyboardEnhancementsMsg{Flags: 1}

// TestTUI_SendTheQueueNow: while the model thinks, tab queues two messages;
// enter (or ctrl+enter) on the empty composer gives both to the working
// agent, in order, before its next model request.
func TestTUI_SendTheQueueNow(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sendNow tea.KeyPressMsg
	}{
		{"enter", tea.KeyPressMsg{Code: tea.KeyEnter}},
		{"ctrl+enter", tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gate := make(chan struct{})
			llm := fakellm.New(t, fakellm.Reply{Text: "never shown", Gate: gate})
			t.Cleanup(func() { close(gate) }) // before the server closes
			d := start(t, liveDeps(t, llm))
			d.until("the session is open", func() bool { return d.m.(bubble.Model).Exit().SessionID != "" })

			d.typeText("start")
			d.key(tea.KeyEnter, 0)
			d.until("the model thinking", func() bool { return len(llm.Requests()) == 1 })
			d.typeText("first queued")
			d.key(tea.KeyTab, 0)
			d.typeText("second queued")
			d.key(tea.KeyTab, 0)
			d.waitFor("↳ queued: second queued")
			assert.Contains(t, d.view(), "↳ queued: first queued")
			assert.Contains(t, d.view(), "enter sends now", "the queue's hint")

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

// TestTUI_EnterReachesTheNextRequestTabWaits: while the agent works, enter
// gives the message to the live run before its next model request, and a
// tab-queued message waits for the run's end, as in Codex. The terminal's
// keyboard answer changes neither.
func TestTUI_EnterReachesTheNextRequestTabWaits(t *testing.T) {
	for _, tc := range []struct {
		name    string
		report  tea.Msg
		newline string
	}{
		{"no answer, as tmux", nil, "ctrl+j new line"},
		{"enhanced", enhanced, "shift+enter new line"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gate := make(chan struct{})
			llm := fakellm.New(t, fakellm.Reply{Text: "never shown", Gate: gate})
			t.Cleanup(func() { close(gate) })
			deps := liveDeps(t, llm)
			deps.Details = true // the idle footer names the new-line key
			d := start(t, deps)
			if tc.report != nil {
				d.send(tc.report)
			}
			d.until("the session is open", func() bool { return d.m.(bubble.Model).Exit().SessionID != "" })
			d.waitFor(tc.newline)

			d.typeText("start")
			d.key(tea.KeyEnter, 0)
			d.until("the model thinking", func() bool { return len(llm.Requests()) == 1 })
			d.waitFor("enter send now · tab queue") // the footer while the agent works
			d.typeText("after the run")
			d.key(tea.KeyTab, 0)
			d.waitFor("1. after the run") // the detailed view lists the queue
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
		})
	}
}
