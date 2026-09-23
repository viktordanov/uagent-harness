// Spike: raw tcell v3 (immediate mode, own widgets, frame throttle).
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"

	"tuibench/internal/sim"
)

var (
	stTS  = tcell.StyleDefault.Foreground(color.Gray)
	stTag = map[sim.Kind]tcell.Style{
		sim.KText:   tcell.StyleDefault.Foreground(color.White).Bold(true),
		sim.KTool:   tcell.StyleDefault.Foreground(color.Teal),
		sim.KResult: tcell.StyleDefault.Foreground(color.Green),
		sim.KError:  tcell.StyleDefault.Foreground(color.Red).Bold(true),
	}
	stText   = tcell.StyleDefault
	stSep    = tcell.StyleDefault.Foreground(color.DarkGray)
	stPrompt = tcell.StyleDefault.Foreground(color.Fuchsia).Bold(true)
	stStatus = tcell.StyleDefault.Foreground(color.Black).Background(color.NewRGBColor(0x87, 0xaf, 0xff))
)

type app struct {
	s      tcell.Screen
	lines  []sim.Line
	c      sim.Counters
	in     sim.Input
	scroll int // lines scrolled up from bottom
}

// put draws str at x,y clipped to maxX, returns next x.
func (a *app) put(x, y, maxX int, str string, st tcell.Style) int {
	for str != "" && x < maxX {
		var w int
		str, w = a.s.Put(x, y, str, st)
		if w == 0 {
			w = 1
		}
		x += w
	}
	return x
}

func (a *app) draw() {
	s := a.s
	w, h := s.Size()
	s.Clear()
	th := h - 5
	if th < 1 {
		th = 1
	}
	end := len(a.lines) - a.scroll
	start := end - th
	if start < 0 {
		start = 0
	}
	for i, y := start, 0; i < end; i, y = i+1, y+1 {
		l := &a.lines[i]
		x := a.put(0, y, w, l.TS, stTS)
		x = a.put(x+1, y, w, l.Kind.Tag(), stTag[l.Kind])
		a.put(x+1, y, w, l.Text, stText)
	}
	for x := 0; x < w; x++ {
		s.Put(x, th, "─", stSep)
	}
	a.put(0, th+1, w, "›", stPrompt)
	if len(a.in.Buf) == 0 {
		a.put(2, th+1, w, sim.Placeholder, stTS)
		s.ShowCursor(2, th+1)
	} else {
		rows, cr, cc := a.in.Wrap(w-2, 3)
		for i, r := range rows {
			a.put(2, th+1+i, w, r, tcell.StyleDefault)
		}
		s.ShowCursor(2+cc, th+1+cr)
	}
	st := a.c.Status()
	x := a.put(0, h-1, w, st, stStatus)
	for ; x < w; x++ {
		s.Put(x, h-1, " ", stStatus)
	}
	s.Show()
	sim.Frames.Add(1)
	if a.c.Done() {
		sim.MarkDoneFrame()
	}
}

func main() {
	cfg := sim.ParseFlags()
	s, err := tcell.NewScreen()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := s.Init(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	s.EnablePaste()
	s.EnableMouse()
	a := &app{s: s, c: sim.Counters{N: cfg.N}}
	a.lines = make([]sim.Line, 0, 1024)

	evCh := make(chan sim.Event, 256)
	go func() {
		sim.Produce(cfg, func(e sim.Event) { evCh <- e })
	}()

	thr := &sim.Throttle{Interval: cfg.FrameInterval()}
	var exitC <-chan time.Time
	a.draw()
	quit := func() {
		s.Fini()
		sim.WriteStats(cfg, "tcell", a.c.Count, len(a.lines), a)
		os.Exit(0)
	}
	request := func() {
		if thr.Request() {
			a.draw()
		}
	}
	for {
		select {
		case e := <-evCh:
			a.lines = append(a.lines, sim.Line{Kind: e.Kind, TS: e.TS, Text: e.Text})
			a.c.Apply(e)
			request()
		case <-thr.C():
			thr.Fired()
			a.draw()
		case <-exitC:
			quit()
		case ev := <-s.EventQ():
			switch ev := ev.(type) {
			case *tcell.EventResize:
				s.Sync()
				a.draw()
			case *tcell.EventKey:
				switch ev.Key() {
				case tcell.KeyCtrlC, tcell.KeyEscape:
					quit()
				case tcell.KeyEnter:
					if txt := a.in.Submit(); txt != "" {
						a.lines = append(a.lines, sim.Line{Kind: sim.KText, TS: "--:--:--.---", Text: "you: " + txt})
					}
				case tcell.KeyCtrlJ:
					a.in.Insert("\n")
				case tcell.KeyBackspace:
					a.in.Backspace()
				case tcell.KeyLeft:
					a.in.Left()
				case tcell.KeyRight:
					a.in.Right()
				case tcell.KeyPgUp:
					a.scroll = min(a.scroll+10, max(0, len(a.lines)-1))
				case tcell.KeyPgDn:
					a.scroll = max(a.scroll-10, 0)
				case tcell.KeyRune:
					a.in.Insert(ev.Str())
				}
				request()
			case *tcell.EventPaste:
				// bracketed paste start/end markers; text arrives as key events
			}
		}
		if cfg.Exit && a.c.Done() && exitC == nil && sim.DoneFrame.Load() != 0 {
			exitC = time.After(cfg.ExitDelay)
		}
	}
}
