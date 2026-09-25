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

// TestTUI_CtrlEnterSendsTheQueue: while the model thinks, two messages
// queue; ctrl+enter on the empty composer gives both to the working
// agent, in order, before its next model request.
func TestTUI_CtrlEnterSendsTheQueue(t *testing.T) {
	gate := make(chan struct{})
	llm := fakellm.New(t, fakellm.Reply{Text: "never shown", Gate: gate})
	t.Cleanup(func() { close(gate) }) // before the server closes
	d := start(t, liveDeps(t, llm))
	d.until("the session is open", func() bool { return d.m.(bubble.Model).Exit().SessionID != "" })

	d.typeText("start")
	d.key(tea.KeyEnter, 0)
	d.until("the model thinking", func() bool { return len(llm.Requests()) == 1 })
	d.typeText("first queued")
	d.key(tea.KeyEnter, 0)
	d.typeText("second queued")
	d.key(tea.KeyEnter, 0)
	d.waitFor("↳ queued: second queued")
	assert.Contains(t, d.view(), "↳ queued: first queued")

	d.key(tea.KeyEnter, tea.ModCtrl)
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
}
