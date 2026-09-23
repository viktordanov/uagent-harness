//go:build !fastview

package main

import (
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

const framework = "bubbletea-viewport"

// transcript wraps bubbles/v2 viewport, fed with SetContentLines per event.
type transcript struct {
	vp    viewport.Model
	lines []string
}

func newTranscript() transcript {
	return transcript{vp: viewport.New(), lines: make([]string, 0, 1024)}
}

func (t *transcript) SetSize(w, h int) {
	t.vp.SetWidth(w)
	t.vp.SetHeight(h)
	t.vp.GotoBottom()
}

func (t *transcript) Append(s ...string) {
	follow := t.vp.AtBottom()
	t.lines = append(t.lines, s...)
	t.vp.SetContentLines(t.lines)
	if follow {
		t.vp.GotoBottom()
	}
}

func (t *transcript) Key(msg tea.KeyPressMsg) {
	t.vp, _ = t.vp.Update(msg)
}

func (t transcript) View() string { return t.vp.View() }
func (t transcript) Len() int     { return len(t.lines) }
