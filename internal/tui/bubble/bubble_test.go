package bubble_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"

	uaharness "github.com/viktordanov/uagent/harness"
	"github.com/viktordanov/uagent/testing/fixtures"

	"github.com/viktordanov/uagent-harness/internal/engine/process"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/tui/bubble"
	"github.com/viktordanov/uagent-harness/testing/harnesstest"
)

// deps opens sessions on the real process engine with the fake runner.
func deps(t *testing.T, fixture string) bubble.Deps {
	t.Helper()
	env := harnesstest.NewEnv(t)
	t.Setenv("FAKERUNNER_FIXTURE", fixtures.Path(fixture))
	t.Setenv("FAKERUNNER_ECHO", "1")
	eng := process.New(uaharness.Config{
		RunnerPath: harnesstest.FakeRunner(t), StateDir: env.StateDir, KillGrace: 300 * time.Millisecond, Getenv: env.Getenv,
	})
	settings := session.Settings{Provider: "openai-codex", Model: "gpt-6-sol", Effort: "high", Workspace: env.Workspace}

	return bubble.Deps{
		Open: func(ctx context.Context, id string) (*session.Session, []session.LoadedRun, error) {
			s, err := session.Open(ctx, eng, session.Options{ID: id, Resumed: id != "", Settings: settings})
			if err != nil || id == "" {
				return s, nil, err
			}
			history, err := session.Load(env.StateDir, id)

			return s, history, err
		},
		Sessions: func() ([]session.Info, error) { return session.Sessions(env.StateDir) },
	}
}

// driver runs a model like tea.Program does: commands run in goroutines and
// their messages go back through Update. Checks read the rendered view, which
// is exact, unlike the renderer's cell-diff output.
type driver struct {
	t    *testing.T
	m    tea.Model
	msgs chan tea.Msg
	quit bool
}

func start(t *testing.T, d bubble.Deps) *driver {
	t.Helper()
	dr := &driver{t: t, m: bubble.New(context.Background(), d), msgs: make(chan tea.Msg, 256)}
	dr.send(tea.WindowSizeMsg{Width: 100, Height: 30})
	dr.exec(dr.m.Init())

	return dr
}

func (d *driver) exec(cmd tea.Cmd) {
	if cmd != nil {
		go func() {
			if msg := cmd(); msg != nil {
				d.msgs <- msg
			}
		}()
	}
}

func (d *driver) send(msg tea.Msg) {
	switch msg := msg.(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			d.exec(c)
		}

		return
	case tea.QuitMsg:
		d.quit = true

		return
	}
	next, cmd := d.m.Update(msg)
	d.m = next
	d.exec(cmd)
}

func (d *driver) view() string { return ansi.Strip(d.m.View().Content) }

func (d *driver) typeText(s string) {
	for _, r := range s {
		d.send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func (d *driver) key(code rune, mod tea.KeyMod) { d.send(tea.KeyPressMsg{Code: code, Mod: mod}) }

// until processes messages until check passes.
func (d *driver) until(what string, check func() bool) {
	d.t.Helper()
	deadline := time.After(10 * time.Second)
	for !check() {
		select {
		case msg := <-d.msgs:
			d.send(msg)
		case <-deadline:
			d.t.Fatalf("timed out waiting for %s; screen:\n%s", what, d.view())
		}
	}
}

func (d *driver) waitFor(text string) {
	d.t.Helper()
	d.until(fmt.Sprintf("%q", text), func() bool { return strings.Contains(d.view(), text) })
}

func (d *driver) waitQuit() {
	d.t.Helper()
	d.until("quit", func() bool { return d.quit })
}

func TestTUI_SendAMessageAndQuit(t *testing.T) {
	d := start(t, deps(t, "simple.jsonl"))
	assert.Contains(t, d.view(), "Opening the session")

	d.typeText("hi there") // typed before the session opens: it is held, not lost
	d.key(tea.KeyEnter, 0)
	d.waitFor("● hello")
	assert.Contains(t, d.view(), "› hi there")
	assert.NotContains(t, d.view(), "1 run ·", "the compact view hides totals")

	d.key('t', tea.ModCtrl)
	d.waitFor("1 run ·")
	d.waitFor("● answer")
	d.key('t', tea.ModCtrl)
	d.until("the compact view again", func() bool { return !strings.Contains(d.view(), "1 run ·") })

	d.typeText("/quit")
	d.key(tea.KeyEnter, 0)
	d.waitQuit()
}

func TestTUI_CommandsAndPrompt(t *testing.T) {
	deps := deps(t, "simple.jsonl")
	deps.Prompt = "first prompt"
	d := start(t, deps)
	d.waitFor("hello")

	d.typeText("/re")
	assert.Contains(t, d.view(), "/resume [id]", "typing a command shows completions")
	d.key('u', tea.ModCtrl) // no effect; clear the draft with ctrl+c instead
	d.key('c', tea.ModCtrl)
	assert.NotContains(t, d.view(), "/resume [id]")

	d.typeText("/effort low")
	d.key(tea.KeyEnter, 0)
	d.waitFor("effort low, applies from the next run")
	assert.Contains(t, d.view(), "· low ·", "the footer shows the new effort")

	d.typeText("/nope")
	d.key(tea.KeyEnter, 0)
	d.waitFor("unknown command /nope")

	d.key('c', tea.ModCtrl)
	d.waitQuit()
}

func TestTUI_ResumeFromThePicker(t *testing.T) {
	deps := deps(t, "simple.jsonl")
	deps.Prompt = "remember this"
	d := start(t, deps)
	d.waitFor("hello")

	d.typeText("/new")
	d.key(tea.KeyEnter, 0)
	d.until("a new, empty session", func() bool {
		v := d.view()

		return !strings.Contains(v, "remember this") && !strings.Contains(v, "hello")
	})

	d.key('s', tea.ModCtrl)
	d.waitFor("Resume a session")
	d.typeText("remember")
	d.key(tea.KeyEnter, 0)
	d.waitFor("› remember this")
	d.waitFor("● hello")

	d.key('c', tea.ModCtrl)
	d.waitQuit()
}

func TestTUI_QueueInterruptAndEdit(t *testing.T) {
	deps := deps(t, "timeout.jsonl")
	t.Setenv("FAKERUNNER_HANG", "1") // the run keeps a tool running until interrupted
	deps.Prompt = "start the long job"
	d := start(t, deps)
	d.waitFor("esc to interrupt")
	d.waitFor("Running sleep 300")

	d.typeText("then update the README")
	d.key(tea.KeyEnter, 0)
	d.waitFor("↳ queued: then update the README")

	d.key(tea.KeyEscape, 0)
	d.waitFor("press esc again to interrupt")
	d.key(tea.KeyEscape, 0)
	d.waitFor("■ interrupted")
	d.until("idle", func() bool { return !strings.Contains(d.view(), "esc to interrupt") })
	assert.Contains(t, d.view(), "↳ queued: then update the README", "an interrupt keeps the queue")
	assert.Contains(t, d.view(), "stopped", "the unfinished tool is shown as stopped")

	d.key(tea.KeyUp, 0)
	d.until("the queued message back in the composer", func() bool {
		v := d.view()

		return strings.Contains(v, "› then update the README") && !strings.Contains(v, "queued: then update the README")
	})

	d.key('c', tea.ModCtrl) // clears the draft
	d.key('c', tea.ModCtrl) // quits: the session is idle
	d.waitQuit()
}
