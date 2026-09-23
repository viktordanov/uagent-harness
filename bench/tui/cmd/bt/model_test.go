package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"tuibench/internal/sim"
)

func newModel(n int) model {
	ta := textarea.New()
	ta.Placeholder = sim.Placeholder
	ta.ShowLineNumbers = false
	ta.Prompt = "› "
	ta.SetHeight(3)
	ta.SetVirtualCursor(false)
	ta.Focus()
	return model{cfg: sim.Config{N: n}, ta: ta, c: sim.Counters{N: n}, tr: newTranscript()}
}

// Pure Elm-style test: no terminal, no goroutines. Update/View are plain
// functions, so the screen is a string we can golden-compare.
func TestViewGolden(t *testing.T) {
	var m tea.Model = newModel(50)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	for i := 1; i <= 50; i++ {
		m, _ = m.Update(evMsg(sim.Gen(i)))
	}
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	out := strings.Join(lines, "\n")
	if !strings.Contains(out, "events 50/50") || !strings.Contains(out, "DONE") {
		t.Fatalf("unexpected view:\n%s", out)
	}
	golden.RequireEqual(t, []byte(out)) // go test -update to regenerate
}

// End-to-end through a real tea.Program with teatest/v2 (x/exp, unversioned).
func TestProgramTeatest(t *testing.T) {
	tm := teatest.NewTestModel(t, newModel(3), teatest.WithInitialTermSize(120, 20))
	for i := 1; i <= 3; i++ {
		tm.Send(evMsg(sim.Gen(i)))
	}
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool { return bytes.Contains(b, []byte("DONE")) },
		teatest.WithDuration(3*time.Second))
	tm.Type("/model opus")
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
	if err := tm.Quit(); err != nil {
		t.Fatal(err)
	}
	fm := tm.FinalModel(t).(model)
	if fm.tr.Len() != 4 { // 3 events + submitted steering message
		t.Fatalf("lines = %d", fm.tr.Len())
	}
}
