//go:build fastview

package main

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

const framework = "bubbletea-window"

// transcript keeps pre-rendered lines and only joins the visible window in
// View, so per-event cost is O(visible) instead of O(total).
type transcript struct {
	lines  []string
	w, h   int
	scroll int // lines scrolled up from the bottom
}

func newTranscript() transcript { return transcript{lines: make([]string, 0, 1024)} }

func (t *transcript) SetSize(w, h int) { t.w, t.h = w, h }

func (t *transcript) Append(s ...string) {
	t.lines = append(t.lines, s...)
	if t.scroll > 0 {
		t.scroll += len(s) // keep the viewport anchored when the user scrolled up
	}
}

func (t *transcript) Key(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "pgup":
		t.scroll = min(t.scroll+t.h, max(0, len(t.lines)-t.h))
	case "pgdown":
		t.scroll = max(0, t.scroll-t.h)
	}
}

func (t transcript) View() string {
	end := len(t.lines) - t.scroll
	start := max(0, end-t.h)
	var b strings.Builder
	for i := start; i < end; i++ {
		if i > start {
			b.WriteByte('\n')
		}
		b.WriteString(t.lines[i])
	}
	// n joined lines contain n-1 newlines; pad to exactly h lines.
	for i := max(1, end-start); i < t.h; i++ {
		b.WriteByte('\n')
	}
	return b.String()
}

func (t transcript) Len() int { return len(t.lines) }
