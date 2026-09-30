package state

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/dustin/go-humanize"

	"github.com/viktordanov/uah/internal/engine"
	"github.com/viktordanov/uah/internal/patch"
)

// Wait is what the live run waits on, since when. Hint replaces "esc to
// interrupt"; Warn marks a retry or a stall.
type Wait struct {
	What, Hint string
	Since      time.Time
	Warn       bool
}

// CurrentWait is what the live run waits on, the most pressing first.
func (s State) CurrentWait() (Wait, bool) {
	l := s.Live
	a, asked := s.PendingApproval()
	switch {
	case l == nil:
		return Wait{}, false
	case !l.Stopping.IsZero():
		return Wait{What: "Stopping", Hint: "esc again to force", Since: l.Stopping}, true
	case asked && !a.Answered:
		return Wait{What: "Waiting for approval · " + oneLine(a.Command), Since: a.Since}, true
	case l.Aside != nil:
		return *l.Aside, true
	case l.Retry != nil:
		r, next := l.Retry, "connecting"
		if left := r.At.Add(r.Delay).Sub(s.Now); left > 0 {
			next = "retry in ~" + (left + time.Second - 1).Truncate(time.Second).String() // rounded up
		}

		return Wait{What: joinDetail("Reconnecting", r.Reason, fmt.Sprintf("attempt %d/%d", r.Attempt, r.MaxAttempts), next), Since: l.Turn, Warn: true}, true
	case !l.Turn.IsZero() || s.Writing(): // streamed text can come before the turn
		return s.modelWait(l.Progress), true
	}

	return s.toolWait(), true
}

// modelWait is the model request's: sending, waiting, writing a tool call
// or the answer, else thinking, and how long no data came.
func (s State) modelWait(p engine.ModelProgress) Wait {
	w := Wait{What: "Thinking", Since: s.Live.Turn}
	switch {
	case p.Phase == engine.PhaseSending:
		w.What = "Sending the request · " + humanize.Bytes(uint64(p.Bytes))
	case p.Phase == engine.PhaseWaiting || p.Phase == engine.PhaseConnecting:
		w.What = "Waiting for the model"
	case p.Tool == patch.ToolName:
		w.What = joinDetail("Writing a patch", p.Target, humanize.Bytes(uint64(p.ToolBytes)))
	case p.Tool != "":
		w.What = "Preparing " + p.Tool + " · " + humanize.Bytes(uint64(p.ToolBytes))
	case s.Writing():
		w.What = "Writing"
	}
	if gap := s.Now.Sub(p.At); !p.At.IsZero() && gap >= 30*time.Second { // a stall
		w.What, w.Warn = w.What+" · no data for "+gap.Truncate(time.Second).String(), true
	}

	return w
}

// toolWait names the agents or the MCP server a call waits on, else the
// commands running, else a call not started yet.
func (s State) toolWait() Wait {
	called, running := "", 0
	for _, it := range slices.Backward(s.Items) {
		switch {
		case it.Kind != KindTool:
		case it.Tool == ToolCalled:
			called = cmp.Or(called, it.Name)
		case it.Tool != ToolRunning:
		case it.Name == "wait_agent":
			return Wait{What: joinDetail("Waiting for agents", it.Label), Since: it.Started}
		case strings.HasPrefix(it.Name, "mcp__"):
			return Wait{What: "Calling server tool · " + strings.TrimPrefix(it.Name, "mcp__"), Since: it.Started}
		default:
			running++
		}
	}
	switch {
	case running > 0:
		return Wait{What: fmt.Sprintf("Running %d command", running) + strings.Repeat("s", min(running-1, 1))}
	case called != "":
		return Wait{What: "Preparing " + called}
	}

	return Wait{What: "Working"}
}

// onProgress follows the model request; PhaseDone ends it, before or
// after the runner's response.
func (s *State) onProgress(e engine.ModelProgress) {
	l := s.live()
	l.Turn, l.Progress = cmp.Or(l.Turn, e.At), e
	switch e.Phase {
	case engine.PhaseStreaming: // the attempt got through
		l.Retry = nil
	case engine.PhaseDone:
		l.Turn, l.Progress, l.Retry = time.Time{}, engine.ModelProgress{}, nil
	}
}

// live is the live run, or a throwaway one when none is.
func (s *State) live() *Live {
	if s.Live == nil {
		return &Live{}
	}

	return s.Live
}

// interrupt stops the live run, and forces the stop once it is stopping.
func (s *State) interrupt() []Effect {
	s.escArmed, s.Status = time.Time{}, ""
	if l := s.live(); l.Stopping.IsZero() {
		l.Stopping = s.Now
	} else {
		s.escArmed, s.Status = s.Now, "forcing the stop…"
	}

	return []Effect{EffInterrupt{}}
}

// joinDetail joins the parts that are not empty with " · ".
func joinDetail(parts ...string) string {
	return strings.Join(slices.DeleteFunc(parts, func(p string) bool { return p == "" }), " · ")
}
