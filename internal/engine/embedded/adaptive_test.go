package embedded_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/engine"
	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/testing/fakellm"
)

// openAdaptive opens a new session at adaptive effort value.
func (e *env) openAdaptive(t *testing.T, value string) (*session.Session, *events) {
	t.Helper()
	settings := e.settings()
	settings.AdaptiveEffort = value
	s, err := session.Open(t.Context(), e.embedded(), session.Options{Settings: settings})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	return s, &events{t: t, s: s}
}

// TestAdaptiveEffort: with adaptive effort on, the first request and one with a user
// message go at the session's effort, and one after tool results only, a
// read as much as a command that confirms, goes one or two levels lower.
// Off, every request keeps it.
func TestAdaptiveEffort(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		command string
		want    []string
	}{
		{"off", "off", "echo one", []string{"high", "high", "high"}},
		{"1 step", "1-step", "echo one", []string{"high", "medium", "high"}},
		{"1 step after a read", "1-step", "cat go.mod", []string{"high", "medium", "high"}},
		{"2 steps", "2-steps", "echo one", []string{"high", "low", "high"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, fakellm.Reply{Commands: []string{tt.command}}, fakellm.Reply{Text: "done"}, fakellm.Reply{Text: "again"})
			s, ev := e.openAdaptive(t, tt.value)
			_, err := s.Submit("run it")
			require.NoError(t, err)
			ev.finished()
			ev.idle()
			_, err = s.Submit("more")
			require.NoError(t, err)
			ev.finished()

			var efforts []string
			for _, r := range e.llm.Requests() {
				efforts = append(efforts, r.Effort)
			}
			assert.Equal(t, tt.want, efforts)
			if tt.value != session.AdaptiveTwoSteps {
				return
			}
			logs, _ := filepath.Glob(filepath.Join(e.StateDir, "runs", "*", "stderr.log"))
			var all string
			for _, l := range logs {
				all += readFile(t, l)
			}
			assert.Contains(t, all, `"effort":"low","effort_reason":"2-steps: tool results only"`, "the attempt's diagnostics say why")
			assert.Contains(t, all, `"effort":"high","effort_reason":"2-steps: first request"`)
			assert.Contains(t, all, `"effort":"high","effort_reason":"2-steps: user message"`)
		})
	}
}

// TestAdaptiveEffort_ChangesLive: turning adaptive effort on during a run
// lowers the run's next follow-up, without priming the session; turning it
// off brings the next follow-up back to the session's effort.
func TestAdaptiveEffort_ChangesLive(t *testing.T) {
	gate, again := make(chan struct{}), make(chan struct{})
	e := newEnv(t, fakellm.Reply{Commands: []string{"true"}, Gate: gate}, fakellm.Reply{Commands: []string{"true"}, Gate: again}, fakellm.Reply{Text: "done"})
	s, ev := e.openAdaptive(t, session.AdaptiveOff)

	_, err := s.Submit("go")
	require.NoError(t, err)
	waitSeen(t, e.llm, 1)
	next := e.settings()
	next.AdaptiveEffort = session.AdaptiveTwoSteps
	applied, err := s.SetSettings(next)
	require.NoError(t, err)
	assert.Equal(t, session.AppliedLive, applied)
	close(gate)
	waitSeen(t, e.llm, 2)
	ev.until("the held follow-up's progress at low", func(e core.Event) bool {
		p, ok := e.(engine.ModelProgress)

		return ok && p.Effort == "low" // what the TUI shows as high→low
	})
	next.AdaptiveEffort = session.AdaptiveOff
	_, err = s.SetSettings(next)
	require.NoError(t, err)
	close(again)
	ev.finished()

	var efforts []string
	for _, r := range e.llm.Requests() {
		efforts = append(efforts, r.Effort)
	}
	assert.Equal(t, []string{"high", "low", "high"}, efforts)
	assert.Equal(t, []string{"go"}, e.llm.Requests()[0].UserTexts, "no workspace context")
}
