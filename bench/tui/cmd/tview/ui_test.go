package main

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"tuibench/internal/sim"
)

// tcell v2's SimulationScreen gives a headless cell grid (tcell v3 removed it).
func screenText(s tcell.SimulationScreen) string {
	cells, w, h := s.GetContents()
	var b strings.Builder
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r := cells[y*w+x].Runes
			if len(r) == 0 {
				b.WriteByte(' ')
			} else {
				b.WriteString(string(r))
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func TestSimulationScreen(t *testing.T) {
	scr := tcell.NewSimulationScreen("UTF-8")
	app, _ := build(sim.Config{N: 30, FPS: 60})
	app.SetScreen(scr) // calls scr.Init(), which resets the size
	scr.SetSize(120, 20)
	go func() { _ = app.Run() }()
	defer app.Stop()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var out string
		app.QueueUpdate(func() {}) // sync with event loop
		out = screenText(scr)
		if strings.Contains(out, "events 30/30") && strings.Contains(out, "DONE") {
			if !strings.Contains(out, "file_00029.go") {
				t.Fatalf("transcript missing:\n%s", out)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout; screen:\n%s", out)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
