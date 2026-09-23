// Spike: vaxis (immediate mode, own widgets, frame throttle).
package main

import (
	"fmt"
	"os"
	"time"
	"tuibench/internal/sim"

	"go.rockorager.dev/vaxis"
)

type evMsg sim.Event

var (
	stTS  = vaxis.Style{Foreground: vaxis.IndexColor(8)}
	stTag = map[sim.Kind]vaxis.Style{
		sim.KText:   {Foreground: vaxis.IndexColor(15), Attribute: vaxis.AttrBold},
		sim.KTool:   {Foreground: vaxis.IndexColor(6)},
		sim.KResult: {Foreground: vaxis.IndexColor(2)},
		sim.KError:  {Foreground: vaxis.IndexColor(9), Attribute: vaxis.AttrBold},
	}
	stSep    = vaxis.Style{Foreground: vaxis.IndexColor(8)}
	stPrompt = vaxis.Style{Foreground: vaxis.IndexColor(13), Attribute: vaxis.AttrBold}
	stPh     = vaxis.Style{Foreground: vaxis.IndexColor(8)}
	stStatus = vaxis.Style{Foreground: vaxis.IndexColor(0), Background: vaxis.RGBColor(0x87, 0xaf, 0xff)}
)

type app struct {
	vx     *vaxis.Vaxis
	lines  []sim.Line
	c      sim.Counters
	in     sim.Input
	scroll int
}

func (a *app) draw() {
	win := a.vx.Window()
	win.Clear()
	w, h := win.Size()
	th := max(1, h-5)
	end := len(a.lines) - a.scroll
	start := max(0, end-th)
	tw := win.New(0, 0, w, th)
	for i, y := start, 0; i < end; i, y = i+1, y+1 {
		l := &a.lines[i]
		tw.Println(y,
			vaxis.Segment{Text: l.TS, Style: stTS},
			vaxis.Segment{Text: " "},
			vaxis.Segment{Text: l.Kind.Tag(), Style: stTag[l.Kind]},
			vaxis.Segment{Text: " "},
			vaxis.Segment{Text: l.Text},
		)
	}
	sep := win.New(0, th, w, 1)
	for x := 0; x < w; x++ {
		sep.SetCell(x, 0, vaxis.Cell{Character: vaxis.Character{Grapheme: "─", Width: 1}, Style: stSep})
	}
	inw := win.New(0, th+1, w, 3)
	inw.Println(0, vaxis.Segment{Text: "›", Style: stPrompt})
	body := inw.New(2, 0, w-2, 3)
	if len(a.in.Buf) == 0 {
		body.Println(0, vaxis.Segment{Text: sim.Placeholder, Style: stPh})
		body.ShowCursor(0, 0, vaxis.CursorBlock)
	} else {
		rows, cr, cc := a.in.Wrap(w-2, 3)
		for i, r := range rows {
			body.Println(i, vaxis.Segment{Text: r})
		}
		body.ShowCursor(cc, cr, vaxis.CursorBlock)
	}
	st := win.New(0, h-1, w, 1)
	st.Fill(vaxis.Cell{Character: vaxis.Character{Grapheme: " ", Width: 1}, Style: stStatus})
	st.Println(0, vaxis.Segment{Text: a.c.Status(), Style: stStatus})
	a.vx.Render()
	sim.Frames.Add(1)
	if a.c.Done() {
		sim.MarkDoneFrame()
	}
}

func main() {
	cfg := sim.ParseFlags()
	vx, err := vaxis.New(vaxis.Options{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	a := &app{vx: vx, c: sim.Counters{N: cfg.N}, lines: make([]sim.Line, 0, 1024)}
	quit := func() {
		vx.Close()
		sim.WriteStats(cfg, "vaxis", a.c.Count, len(a.lines), a)
		os.Exit(0)
	}
	// NB: PostEvent drops events when the 1024-slot queue is full; use the
	// blocking variant for a lossless producer.
	go sim.Produce(cfg, func(e sim.Event) { vx.PostEventBlocking(evMsg(e)) })

	thr := &sim.Throttle{Interval: cfg.FrameInterval()}
	request := func() {
		if thr.Request() {
			a.draw()
		}
	}
	var exitC <-chan time.Time
	a.draw()
	for {
		select {
		case <-thr.C():
			thr.Fired()
			a.draw()
		case <-exitC:
			quit()
		case ev := <-vx.Events():
			switch ev := ev.(type) {
			case evMsg:
				e := sim.Event(ev)
				a.lines = append(a.lines, sim.Line{Kind: e.Kind, TS: e.TS, Text: e.Text})
				a.c.Apply(e)
				request()
			case vaxis.Resize, vaxis.Redraw:
				a.draw()
			case vaxis.Key:
				if ev.EventType == vaxis.EventRelease {
					break
				}
				switch {
				case ev.Matches('c', vaxis.ModCtrl), ev.Matches(vaxis.KeyEsc):
					quit()
				case ev.Matches(vaxis.KeyEnter):
					if txt := a.in.Submit(); txt != "" {
						a.lines = append(a.lines, sim.Line{Kind: sim.KText, TS: "--:--:--.---", Text: "you: " + txt})
					}
				case ev.Matches('j', vaxis.ModCtrl):
					a.in.Insert("\n")
				case ev.Matches(vaxis.KeyBackspace):
					a.in.Backspace()
				case ev.Matches(vaxis.KeyLeft):
					a.in.Left()
				case ev.Matches(vaxis.KeyRight):
					a.in.Right()
				case ev.Matches(vaxis.KeyPgUp):
					a.scroll = min(a.scroll+10, max(0, len(a.lines)-1))
				case ev.Matches(vaxis.KeyPgDown):
					a.scroll = max(a.scroll-10, 0)
				case ev.Text != "":
					a.in.Insert(ev.Text)
				}
				request()
			}
		}
		if cfg.Exit && a.c.Done() && exitC == nil && sim.DoneFrame.Load() != 0 {
			exitC = time.After(cfg.ExitDelay)
		}
	}
}
