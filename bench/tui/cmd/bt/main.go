// Spike: Bubble Tea v2, idiomatic: bubbles/v2 viewport + textarea, lipgloss/v2
// styles, one tea.Msg per event via Program.Send, viewport.SetContentLines
// after every event (what most Bubble Tea apps and examples do).
//
// Build with -tags fastview for the variant that keeps the bubbles textarea but
// replaces the viewport with a windowed renderer that only joins the visible
// lines (see transcript_fast.go).
package main

import (
	"flag"
	"os"
	"strings"
	"time"
	"tuibench/internal/sim"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var (
	stTS  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	stTag = map[sim.Kind]lipgloss.Style{
		sim.KText:   lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true),
		sim.KTool:   lipgloss.NewStyle().Foreground(lipgloss.Color("6")),
		sim.KResult: lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		sim.KError:  lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true),
	}
	stSep    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	stStatus = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("#87afff"))
)

type (
	evMsg    sim.Event
	batchMsg []sim.Event
	quitMsg  struct{}
)

type model struct {
	cfg    sim.Config
	tr     transcript
	ta     textarea.Model
	c      sim.Counters
	w, h   int
	quitAt bool
}

func renderLine(e sim.Event) string {
	return stTS.Render(e.TS) + " " + stTag[e.Kind].Render(e.Kind.Tag()) + " " + e.Text
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.tr.SetSize(m.w, max(1, m.h-5))
		m.ta.SetWidth(m.w)
	case evMsg:
		e := sim.Event(msg)
		m.tr.Append(renderLine(e))
		m.c.Apply(e)
	case batchMsg:
		batch := make([]string, len(msg))
		for i, e := range msg {
			batch[i] = renderLine(e)
			m.c.Apply(e)
		}
		m.tr.Append(batch...)
	case quitMsg:
		return m, tea.Quit
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "enter":
			if v := m.ta.Value(); v != "" {
				m.tr.Append(stTS.Render("--:--:--.---") + " " + stTag[sim.KText].Render("text ") + " you: " + v)
				m.ta.Reset()
			}
			return m, nil
		case "pgup", "pgdown":
			m.tr.Key(msg)
			return m, nil
		}
		var cmd tea.Cmd
		m.ta, cmd = m.ta.Update(msg)
		cmds = append(cmds, cmd)
	default:
		var cmd tea.Cmd
		m.ta, cmd = m.ta.Update(msg)
		cmds = append(cmds, cmd)
	}
	if m.cfg.Exit && m.c.Done() && !m.quitAt {
		m.quitAt = true
		// Give the renderer (60 fps ticker) time to flush the final frame.
		cmds = append(cmds, tea.Tick(m.cfg.ExitDelay+20*time.Millisecond, func(time.Time) tea.Msg { return quitMsg{} }))
	}
	return m, tea.Batch(cmds...)
}

func (m model) View() tea.View {
	sim.Frames.Add(1) // counts View() calls, not terminal flushes
	if m.c.Done() {
		sim.MarkDoneFrame() // approximate: flush happens on the next renderer tick
	}
	var b strings.Builder
	b.WriteString(m.tr.View())
	b.WriteByte('\n')
	b.WriteString(stSep.Render(strings.Repeat("─", m.w)))
	b.WriteByte('\n')
	b.WriteString(m.ta.View())
	b.WriteByte('\n')
	b.WriteString(stStatus.Width(m.w).MaxWidth(m.w).Render(m.c.Status()))
	v := tea.NewView(b.String())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	if c := m.ta.Cursor(); c != nil {
		c.Y += m.h - 4
		v.Cursor = c
	}
	return v
}

var (
	batch    = flag.Bool("batch", false, "coalesce queued events into one message")
	batchWin = flag.Duration("batchwin", 0, "with -batch: after the first event, keep collecting for this long (e.g. 16ms)")
)

func main() {
	cfg := sim.ParseFlags()
	ta := textarea.New()
	ta.Placeholder = sim.Placeholder
	ta.ShowLineNumbers = false
	ta.Prompt = "› "
	ta.SetHeight(3)
	ta.SetVirtualCursor(false) // real terminal cursor, no blink ticks (like Crush)
	ta.Focus()                 // must happen here: Init has a value receiver, focusing there is lost
	m := model{cfg: cfg, ta: ta, c: sim.Counters{N: cfg.N}, tr: newTranscript()}
	opts := []tea.ProgramOption{}
	if cfg.FPS > 0 {
		opts = append(opts, tea.WithFPS(cfg.FPS))
	}
	p := tea.NewProgram(m, opts...)
	if *batch {
		// Drain-what-is-available batching: no added latency, but one
		// Update/View per burst instead of one per event.
		ch := make(chan sim.Event, 1024)
		go func() { sim.Produce(cfg, func(e sim.Event) { ch <- e }); close(ch) }()
		go func() {
			for e := range ch {
				b := batchMsg{e}
				if *batchWin > 0 {
					// time-window batching: at most one Update/View per window
					t := time.NewTimer(*batchWin)
				collect:
					for {
						select {
						case e2, ok := <-ch:
							if !ok {
								break collect
							}
							b = append(b, e2)
						case <-t.C:
							break collect
						}
					}
					t.Stop()
				}
			drain:
				for len(b) < 4096 {
					select {
					case e2, ok := <-ch:
						if !ok {
							break drain
						}
						b = append(b, e2)
					default:
						break drain
					}
				}
				p.Send(b)
			}
		}()
	} else {
		go sim.Produce(cfg, func(e sim.Event) { p.Send(evMsg(e)) })
	}
	final, err := p.Run()
	if err != nil {
		os.Exit(1)
	}
	fm := final.(model)
	sim.WriteStats(cfg, framework, fm.c.Count, fm.tr.Len(), &fm)
}
