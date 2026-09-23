// Spike: tview (retained widget tree on tcell v2): TextView + TextArea + status TextView.
package main

import (
	"strings"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"tuibench/internal/sim"
)

var tagColor = map[sim.Kind]string{
	sim.KText:   "[white::b]",
	sim.KTool:   "[teal]",
	sim.KResult: "[green]",
	sim.KError:  "[red::b]",
}

func main() {
	cfg := sim.ParseFlags()
	app, counts := build(cfg)
	if err := app.Run(); err != nil {
		panic(err)
	}
	count, lines := counts()
	sim.WriteStats(cfg, "tview", count, lines, app)
}

// build wires the widget tree and starts the producer; the caller runs the
// app (tests inject a tcell.SimulationScreen with app.SetScreen first).
func build(cfg sim.Config) (*tview.Application, func() (int, int)) {
	app := tview.NewApplication().EnableMouse(true).EnablePaste(true)

	transcript := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWrap(false)
	transcript.SetBorder(false)
	transcript.ScrollToEnd()

	input := tview.NewTextArea().SetPlaceholder("steer the agent… (/model /effort /fast)")
	input.SetBorder(false)

	sep := tview.NewBox().SetDrawFunc(func(screen tcell.Screen, x, y, w, h int) (int, int, int, int) {
		st := tcell.StyleDefault.Foreground(tcell.ColorDarkGray)
		for i := x; i < x+w; i++ {
			screen.SetContent(i, y, '─', nil, st)
		}
		return x, y, w, h
	})

	status := tview.NewTextView().SetDynamicColors(false)
	status.SetBackgroundColor(tcell.NewRGBColor(0x87, 0xaf, 0xff))
	status.SetTextColor(tcell.ColorBlack)

	inputRow := tview.NewFlex().
		AddItem(tview.NewTextView().SetText("›").SetTextColor(tcell.ColorFuchsia), 2, 0, false).
		AddItem(input, 0, 1, true)

	root := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(transcript, 0, 1, false).
		AddItem(sep, 1, 0, false).
		AddItem(inputRow, 3, 0, true).
		AddItem(status, 1, 0, false)

	c := sim.Counters{N: cfg.N}
	status.SetText(c.Status())
	lines := 0
	var exiting bool

	app.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		switch ev.Key() {
		case tcell.KeyEnter:
			if txt := input.GetText(); txt != "" {
				_, _ = transcript.Write([]byte("\n[gray]--:--:--.---[-] [white::b]text [-:-:-] you: " + tview.Escape(txt)))
				lines++
				input.SetText("", false)
			}
			return nil
		case tcell.KeyPgUp, tcell.KeyPgDn:
			transcript.InputHandler()(ev, func(tview.Primitive) {})
			return nil
		}
		return ev
	})

	app.SetAfterDrawFunc(func(tcell.Screen) {
		sim.Frames.Add(1)
		if c.Done() {
			sim.MarkDoneFrame()
			if cfg.Exit && !exiting {
				exiting = true
				time.AfterFunc(cfg.ExitDelay, app.Stop)
			}
		}
	})

	var sb strings.Builder
	apply := func(e sim.Event) {
		sb.Reset()
		if lines > 0 {
			sb.WriteByte('\n') // leading newline: no empty last line in the view
		}
		sb.WriteString("[gray]")
		sb.WriteString(e.TS)
		sb.WriteString("[-] ")
		sb.WriteString(tagColor[e.Kind])
		sb.WriteString(e.Kind.Tag())
		sb.WriteString("[-:-:-] ")
		sb.WriteString(e.Text)
		_, _ = transcript.Write([]byte(sb.String()))
		lines++
		c.Apply(e)
		status.SetText(c.Status())
	}

	// Frame coalescing: the first event after a frame arms a one-shot timer;
	// no ticker runs while idle.
	var scheduled atomic.Bool
	schedule := func() {
		if scheduled.CompareAndSwap(false, true) {
			time.AfterFunc(cfg.FrameInterval(), func() {
				scheduled.Store(false)
				app.QueueUpdateDraw(func() {})
			})
		}
	}
	go sim.Produce(cfg, func(e sim.Event) {
		if cfg.FPS <= 0 {
			app.QueueUpdateDraw(func() { apply(e) })
			return
		}
		app.QueueUpdate(func() { apply(e) })
		schedule()
	})

	app.SetRoot(root, true).SetFocus(input)
	return app, func() (int, int) { return c.Count, lines }
}
