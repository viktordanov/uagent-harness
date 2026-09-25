package render

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// longTranscript is a session idle after n runs, each answered in Markdown.
func longTranscript(n int) state.State {
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	s := state.New(at)
	s.Caps.Rewind = true
	evs := []any{session.SessionOpened{At: at, ID: "3f2a1b2c-0000-4000-8000-000000000000", Engine: "embedded"}}
	for i := 1; i <= n; i++ {
		run := fmt.Sprintf("run-%d", i)
		evs = append(evs,
			core.RunStarted{At: at, RunID: run},
			core.UserMessage{At: at, ID: fmt.Sprintf("m%d", i), Text: fmt.Sprintf("message %d", i)},
			core.AssistantMessage{At: at, Text: fmt.Sprintf("## Answer %d\n\n- **one** and `two`\n- three\n\n```go\nfunc f() int { return %d }\n```", i, i), Final: true},
			core.RunFinished{At: at, Result: core.Result{Request: core.Request{RunID: run}, Status: core.StatusOK}},
			session.Idle{At: at},
		)
	}
	for _, ev := range evs {
		s, _ = state.Reduce(s, ev)
	}

	return s
}

func frame(s state.State, c *Cache) string {
	out, _ := Screen(s, c, Frame{Width: 100, Height: 40, Composer: "λ ", ComposerHeight: 1})

	return out
}

// TestBacktrackKeepsTheCache: entering and leaving the selection draws no
// item again; the cache keeps the same lines, so no Markdown is rendered.
func TestBacktrackKeepsTheCache(t *testing.T) {
	s := longTranscript(50)
	selecting := s
	for _, ev := range []any{state.Esc{Empty: true}, state.Esc{Empty: true}, state.BacktrackMove{Delta: -40}} {
		selecting, _ = state.Reduce(selecting, ev)
	}
	c := NewCache(Amber)
	frame(selecting, c)
	before := map[string][]string{}
	for k, e := range c.entries {
		before[k] = e.lines
	}
	cancelled, _ := state.Reduce(selecting, state.BacktrackCancel{})
	frame(cancelled, c)
	frame(selecting, c)
	assert.Len(t, c.entries, len(before))
	for k, e := range c.entries {
		if len(e.lines) > 0 {
			assert.Same(t, &before[k][0], &e.lines[0], "%s is drawn again", k)
		}
	}
}

// BenchmarkBacktrackFrame draws a frame of a 200-run transcript with an
// older message selected, from a warm cache: only the window is faded.
func BenchmarkBacktrackFrame(b *testing.B) {
	s := longTranscript(200)
	for _, ev := range []any{state.Esc{Empty: true}, state.Esc{Empty: true}, state.BacktrackMove{Delta: -100}} {
		s, _ = state.Reduce(s, ev)
	}
	c := NewCache(Amber)
	frame(s, c)
	b.ReportAllocs()
	for b.Loop() {
		frame(s, c)
	}
}
