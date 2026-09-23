// Spike: Charm Ultraviolet used directly (Bubble Tea v2's renderer and input
// layer without the Elm loop): immediate mode, own widgets, frame throttle.
package main

import (
	"flag"
	"image/color"
	"os"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/ultraviolet/screen"
	"github.com/charmbracelet/x/ansi"

	"tuibench/internal/sim"
)

type evMsg sim.Event

var (
	stTS  = uv.Style{Fg: ansi.IndexedColor(8)}
	stTag = map[sim.Kind]uv.Style{
		sim.KText:   {Fg: ansi.IndexedColor(15), Attrs: uv.AttrBold},
		sim.KTool:   {Fg: ansi.IndexedColor(6)},
		sim.KResult: {Fg: ansi.IndexedColor(2)},
		sim.KError:  {Fg: ansi.IndexedColor(9), Attrs: uv.AttrBold},
	}
	stSep    = uv.Style{Fg: ansi.IndexedColor(8)}
	stPrompt = uv.Style{Fg: ansi.IndexedColor(13), Attrs: uv.AttrBold}
	stStatus = uv.Style{Fg: ansi.IndexedColor(0), Bg: color.RGBA{0x87, 0xaf, 0xff, 0xff}}
)

type app struct {
	scr *uv.TerminalScreen
	tgt uv.Screen // what draw() paints into: scr, or buf when -hardscroll
	ctx *screen.Context
	// -hardscroll: drive uv.TerminalRenderer directly (like Bubble Tea's
	// renderer does) so the scroll-region optimisation can be enabled;
	// TerminalScreen does not expose SetScrollOptim.
	rend   *uv.TerminalRenderer
	buf    uv.ScreenBuffer
	cx, cy int
	lines  []sim.Line
	c      sim.Counters
	in     sim.Input
	scroll int
}

func (a *app) text(x, y int, s string, st uv.Style) int {
	a.ctx.SetStyle(st)
	a.ctx.DrawString(s, x, y)
	return x + ansi.StringWidth(s)
}

func (a *app) draw() {
	scr := a.tgt
	screen.Clear(scr)
	b := scr.Bounds()
	w, h := b.Dx(), b.Dy()
	th := max(1, h-5)
	end := len(a.lines) - a.scroll
	start := max(0, end-th)
	for i, y := start, 0; i < end; i, y = i+1, y+1 {
		l := &a.lines[i]
		x := a.text(0, y, l.TS, stTS)
		x = a.text(x+1, y, l.Kind.Tag(), stTag[l.Kind])
		a.text(x+1, y, l.Text, uv.Style{})
	}
	sep := uv.Cell{Content: "─", Width: 1, Style: stSep}
	for x := 0; x < w; x++ {
		scr.SetCell(x, th, &sep)
	}
	a.text(0, th+1, "›", stPrompt)
	if len(a.in.Buf) == 0 {
		a.text(2, th+1, sim.Placeholder, stTS)
		a.cx, a.cy = 2, th+1
	} else {
		rows, cr, cc := a.in.Wrap(w-2, 3)
		for i, r := range rows {
			a.text(2, th+1+i, r, uv.Style{})
		}
		a.cx, a.cy = 2+cc, th+1+cr
	}
	fill := uv.Cell{Content: " ", Width: 1, Style: stStatus}
	for x := 0; x < w; x++ {
		scr.SetCell(x, h-1, &fill)
	}
	a.text(0, h-1, a.c.Status(), stStatus)
	if a.rend != nil {
		a.rend.Render(a.buf.RenderBuffer)
		a.rend.MoveTo(a.cx, a.cy)
		_ = a.rend.Flush()
	} else {
		a.scr.SetCursorPosition(a.cx, a.cy)
		a.scr.Render()
		_ = a.scr.Flush()
	}
	sim.Frames.Add(1)
	if a.c.Done() {
		sim.MarkDoneFrame()
	}
}

var hardscroll = flag.Bool("hardscroll", false, "use uv.TerminalRenderer directly with scroll optimisation")

func main() {
	cfg := sim.ParseFlags()
	t := uv.DefaultTerminal()
	scr := t.Screen()
	if !*hardscroll {
		scr.EnterAltScreen()
	}
	scr.EnableBracketedPaste()
	scr.SetMouseMode(uv.MouseModeClick)
	scr.ShowCursor()
	if err := t.Start(); err != nil {
		os.Exit(1)
	}
	a := &app{scr: scr, tgt: scr, c: sim.Counters{N: cfg.N}, lines: make([]sim.Line, 0, 1024)}
	w, h, _ := t.GetSize()
	resize := func(w, h int) {
		if a.rend != nil {
			a.buf.Resize(w, h)
			a.rend.Resize(w, h)
			a.rend.Erase()
		} else {
			scr.Resize(w, h)
		}
	}
	if *hardscroll {
		_, _ = t.Write([]byte(ansi.SetModeAltScreenSaveCursor))
		a.rend = uv.NewTerminalRenderer(t, os.Environ())
		a.rend.SetFullscreen(true)
		a.rend.SetRelativeCursor(false)
		a.rend.SetScrollOptim(true)
		a.buf = uv.NewScreenBuffer(w, h)
		a.tgt = a.buf
		a.rend.Resize(w, h)
		a.rend.Erase()
	} else {
		scr.Resize(w, h)
	}
	a.ctx = screen.NewContext(a.tgt)
	quit := func() {
		if a.rend != nil {
			_, _ = t.Write([]byte(ansi.ResetModeAltScreenSaveCursor))
		}
		_ = t.Stop()
		sim.WriteStats(cfg, "ultraviolet", a.c.Count, len(a.lines), a)
		os.Exit(0)
	}
	go sim.Produce(cfg, func(e sim.Event) { t.SendEvent(evMsg(e)) })

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
		case ev := <-t.Events():
			switch ev := ev.(type) {
			case evMsg:
				e := sim.Event(ev)
				a.lines = append(a.lines, sim.Line{Kind: e.Kind, TS: e.TS, Text: e.Text})
				a.c.Apply(e)
				request()
			case uv.WindowSizeEvent:
				resize(ev.Width, ev.Height)
				a.draw()
			case uv.KeyPressEvent:
				switch {
				case ev.MatchString("ctrl+c", "esc"):
					quit()
				case ev.MatchString("enter"):
					if txt := a.in.Submit(); txt != "" {
						a.lines = append(a.lines, sim.Line{Kind: sim.KText, TS: "--:--:--.---", Text: "you: " + txt})
					}
				case ev.MatchString("ctrl+j"):
					a.in.Insert("\n")
				case ev.MatchString("backspace"):
					a.in.Backspace()
				case ev.MatchString("left"):
					a.in.Left()
				case ev.MatchString("right"):
					a.in.Right()
				case ev.MatchString("pgup"):
					a.scroll = min(a.scroll+10, max(0, len(a.lines)-1))
				case ev.MatchString("pgdown"):
					a.scroll = max(a.scroll-10, 0)
				case ev.Text != "":
					a.in.Insert(ev.Text)
				}
				request()
			case uv.PasteEvent:
				a.in.Insert(ev.Content)
				request()
			}
		}
		if cfg.Exit && a.c.Done() && exitC == nil && sim.DoneFrame.Load() != 0 {
			exitC = time.After(cfg.ExitDelay)
		}
	}
}
