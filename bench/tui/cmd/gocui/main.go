// Spike: awesome-gocui v1.1.0 (views over tcell v2; ANSI-parsed view content;
// every Update redraws all views). Events are buffered and flushed into the
// view by a one-shot frame timer so it gets the same coalescing as the others.
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/awesome-gocui/gocui"

	"tuibench/internal/sim"
)

var tagANSI = map[sim.Kind]string{
	sim.KText:   "\x1b[1;97m",
	sim.KTool:   "\x1b[36m",
	sim.KResult: "\x1b[32m",
	sim.KError:  "\x1b[1;91m",
}

func main() {
	cfg := sim.ParseFlags()
	g, err := gocui.NewGui(gocui.OutputTrue, true)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	g.Mouse = true
	g.Cursor = true
	c := sim.Counters{N: cfg.N}
	lines := 0
	exiting := false

	var mu sync.Mutex
	var pending []sim.Event

	layout := func(g *gocui.Gui) error {
		w, h := g.Size()
		th := max(1, h-5)
		v, err := g.SetView("transcript", -1, -1, w, th, 0)
		if err != nil && !errors.Is(err, gocui.ErrUnknownView) {
			return err
		}
		if err != nil {
			v.Frame = false
			v.Autoscroll = true
			v.Wrap = false
		}
		s, err := g.SetView("sep", -1, th-1, w, th+1, 0)
		if err != nil && !errors.Is(err, gocui.ErrUnknownView) {
			return err
		}
		if err != nil {
			s.Frame = false
			fmt.Fprint(s, "\x1b[90m"+strings.Repeat("─", w)+"\x1b[0m")
		}
		p, err := g.SetView("prompt", -1, th, 2, th+4, 0)
		if err != nil && !errors.Is(err, gocui.ErrUnknownView) {
			return err
		}
		if err != nil {
			p.Frame = false
			fmt.Fprint(p, "\x1b[1;95m›\x1b[0m")
		}
		in, err := g.SetView("input", 1, th, w, th+4, 0)
		if err != nil && !errors.Is(err, gocui.ErrUnknownView) {
			return err
		}
		if err != nil {
			in.Frame = false
			in.Editable = true
			in.Wrap = true
			if _, err := g.SetCurrentView("input"); err != nil {
				return err
			}
		}
		st, err := g.SetView("status", -1, h-2, w, h, 0)
		if err != nil && !errors.Is(err, gocui.ErrUnknownView) {
			return err
		}
		if err != nil {
			st.Frame = false
			st.BgColor = gocui.NewRGBColor(0x87, 0xaf, 0xff)
			st.FgColor = gocui.ColorBlack
		}
		st.Clear()
		fmt.Fprint(st, c.Status())
		sim.Frames.Add(1)
		if c.Done() {
			sim.MarkDoneFrame()
			if cfg.Exit && !exiting {
				exiting = true
				time.AfterFunc(cfg.ExitDelay, func() { g.Update(func(*gocui.Gui) error { return gocui.ErrQuit }) })
			}
		}
		return nil
	}
	g.SetManagerFunc(layout)
	quit := func(*gocui.Gui, *gocui.View) error { return gocui.ErrQuit }
	_ = g.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, quit)
	_ = g.SetKeybinding("input", gocui.KeyEnter, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		txt := strings.TrimSpace(v.Buffer())
		if txt != "" {
			tv, _ := g.View("transcript")
			fmt.Fprintf(tv, "\x1b[90m--:--:--.---\x1b[0m \x1b[1;97mtext \x1b[0m you: %s\n", txt)
			lines++
		}
		v.Clear()
		_ = v.SetCursor(0, 0)
		return nil
	})

	var sb strings.Builder
	flush := func(g *gocui.Gui) error {
		mu.Lock()
		evs := pending
		pending = nil
		mu.Unlock()
		tv, err := g.View("transcript")
		if err != nil {
			return nil
		}
		for _, e := range evs {
			sb.Reset()
			sb.WriteString("\x1b[90m")
			sb.WriteString(e.TS)
			sb.WriteString("\x1b[0m ")
			sb.WriteString(tagANSI[e.Kind])
			sb.WriteString(e.Kind.Tag())
			sb.WriteString("\x1b[0m ")
			sb.WriteString(e.Text)
			sb.WriteByte('\n')
			_, _ = tv.Write([]byte(sb.String()))
			lines++
			c.Apply(e)
		}
		return nil
	}
	var scheduled atomic.Bool
	go sim.Produce(cfg, func(e sim.Event) {
		if cfg.FPS <= 0 {
			g.Update(func(g *gocui.Gui) error { mu.Lock(); pending = append(pending, e); mu.Unlock(); return flush(g) })
			return
		}
		mu.Lock()
		pending = append(pending, e)
		mu.Unlock()
		// one-shot timer per frame; nothing runs while idle
		if scheduled.CompareAndSwap(false, true) {
			time.AfterFunc(cfg.FrameInterval(), func() {
				scheduled.Store(false)
				g.Update(flush)
			})
		}
	})
	if err := g.MainLoop(); err != nil && !errors.Is(err, gocui.ErrQuit) {
		g.Close()
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	g.Close()
	sim.WriteStats(cfg, "gocui", c.Count, lines, g)
}
