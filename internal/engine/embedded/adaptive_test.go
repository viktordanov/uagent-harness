package embedded_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/engine"
	"github.com/viktordanov/uah/internal/engine/embedded"
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

// openUpdating opens a new session on a model that takes effort updates
// (gpt-6.1-sol in the bundled catalog) at adaptive effort value, with
// effort_updates set to updates.
func (e *env) openUpdating(t *testing.T, model, value string, updates bool) (*session.Session, *events) {
	t.Helper()
	settings := e.settings()
	settings.Model, settings.AdaptiveEffort = model, value
	eng := embedded.New(embedded.Config{StateDir: e.StateDir, Provider: "openai", Getenv: e.getenv, EffortUpdates: updates})
	s, err := session.Open(t.Context(), eng, session.Options{Settings: settings})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	return s, &events{t: t, s: s}
}

// TestAdaptiveEffort_Updates: on a model that takes effort updates, every
// request carries the session's effort and a configuration_update item
// lowers it before a follow-up and raises it again before a user message;
// a new effort goes the same way. Each request's input starts with the one
// before it, so the prompt cache holds across the switches and the runs.
// Without effort_updates, or on a model without them, the request's effort
// changes as before.
func TestAdaptiveEffort_Updates(t *testing.T) {
	tests := []struct {
		name    string
		model   string
		updates bool
		efforts []string
		set     [][]string // each request's updates, in order
	}{
		{"updates", "gpt-6.1-sol", true, []string{"high", "high", "high", "high"},
			[][]string{{"high"}, {"high", "low"}, {"high", "low", "high"}, {"high", "low", "high", "medium"}}},
		{"effort_updates off", "gpt-6.1-sol", false, []string{"high", "low", "high", "medium"}, [][]string{nil, nil, nil, nil}},
		{"a model without them", "gpt-test", true, []string{"high", "low", "high", "medium"}, [][]string{nil, nil, nil, nil}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, fakellm.Reply{Commands: []string{"echo one"}}, fakellm.Reply{Text: "done"}, fakellm.Reply{Text: "again"}, fakellm.Reply{Text: "once more"})
			s, ev := e.openUpdating(t, tt.model, session.AdaptiveTwoSteps, tt.updates)
			for i, text := range []string{"run it", "more", "and more"} {
				if i == 2 {
					next := e.settings()
					next.Model, next.AdaptiveEffort, next.Effort = tt.model, session.AdaptiveTwoSteps, "medium"
					_, err := s.SetSettings(next)
					require.NoError(t, err)
				}
				_, err := s.Submit(text)
				require.NoError(t, err)
				ev.finished()
				ev.idle()
			}

			reqs := e.llm.Requests()
			require.Len(t, reqs, 4)
			var efforts []string
			var set [][]string
			for i, r := range reqs {
				efforts, set = append(efforts, r.Effort), append(set, r.EffortUpdates)
				if i > 0 && tt.updates && tt.model != "gpt-test" {
					assert.Equal(t, reqs[i-1].Input, r.Input[:len(reqs[i-1].Input)], "request %d extends the one before", i+1)
				}
			}
			assert.Equal(t, tt.efforts, efforts, "the requests' efforts")
			assert.Equal(t, tt.set, set, "the updates in each request")
			if !tt.updates || tt.model == "gpt-test" {
				return
			}
			logs, _ := filepath.Glob(filepath.Join(e.StateDir, "runs", "*", "stderr.log"))
			var all string
			for _, l := range logs {
				all += readFile(t, l)
			}
			assert.Contains(t, all, `"effort":"low","effort_reason":"2-steps: tool results only","request_effort":"high","effort_update":true`, "the attempt's line says the update")
			assert.Contains(t, all, `"effort":"high","effort_reason":"2-steps: first request","request_effort":"high"`)
			assert.Contains(t, all, `"effort":"medium","effort_reason":"2-steps: user message","request_effort":"high","effort_update":true`)
		})
	}
}
