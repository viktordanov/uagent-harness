package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/session"
)

// printer writes one progress line per notable event, for people.
type printer struct {
	w       io.Writer
	verbose bool
	origin  time.Time
	running bool
	// agents are the subagents' nicknames by ID.
	agents map[string]string
}

func newPrinter(w io.Writer, verbose bool) *printer {
	return &printer{w: w, verbose: verbose, origin: time.Now(), agents: map[string]string{}}
}

func (p *printer) print(event core.Event) {
	switch e := event.(type) {
	case session.SessionOpened:
		resumed := ""
		if e.Resumed {
			resumed = " (resumed)"
		}
		fast := ""
		if e.Settings.ServiceTier != "" {
			fast = " · fast"
		}
		fmt.Fprintf(p.w, "uah · session %s%s · %s/%s · effort %s%s · %s engine · sandbox %s · %s\n",
			short(e.ID), resumed, e.Settings.Provider, modelLabel(e.Settings.Model), e.Settings.Effort, fast, e.Engine, sandboxLabel(e.Settings.Sandbox), e.Settings.Workspace)
	case session.InstructionsLoaded:
		note := ""
		if e.Truncated {
			note = ", cut at the size limit"
		}
		p.say(fmt.Sprintf("instructions: %s (%s bytes%s)", strings.Join(e.Files, ", "), commas(int64(e.Bytes)), note))
	case session.InputQueued:
		suffix := ""
		if p.running {
			suffix = "  (queued)"
		}
		p.say("› " + oneLine(e.Input.Text, 160) + suffix)
	case session.InputFailed:
		p.say(fmt.Sprintf("✗ %d message(s) not delivered: %s", len(e.IDs), e.Reason))
	case session.InputWithdrawn:
		p.say("↩ queued message withdrawn")
	case session.SettingsChanged:
		when := "from the next run"
		if e.Applied == session.AppliedLive {
			when = "now"
		}
		p.say(fmt.Sprintf("settings: %s/%s · effort %s, applies %s", e.Settings.Provider, modelLabel(e.Settings.Model), e.Settings.Effort, when))
	case session.Notice:
		p.say(e.Level + ": " + e.Message)
	case session.Idle:
		p.say("· idle")
	case core.RunStarted:
		p.running = true
		p.say("run " + e.RunID)
	case core.PreflightWarning:
		p.say("warning: " + e.Message)
	case core.ModelResponded:
		p.say(fmt.Sprintf("turn %d  %s in · %s out  %.1fs", e.Turn, commas(e.Usage.InputTokens), commas(e.Usage.OutputTokens), e.Duration.Seconds()))
		if e.Failure != "" {
			p.say("  ! failure: " + e.Failure)
		}
	case core.ToolCalled:
		p.say(fmt.Sprintf("  → %s  %s", e.Name, e.Label))
	case core.ToolFinished:
		mark := "  ← "
		if !e.OK {
			mark = "  ✗ "
		}
		p.say(fmt.Sprintf("%s%s  %s (%s, %.1fs)", mark, e.Name, e.Label, e.Detail, e.Duration.Seconds()))
	case core.AssistantMessage:
		if e.Final {
			p.say(fmt.Sprintf("  ✓ answer (%d chars)", len(e.Text)))
		} else if strings.TrimSpace(e.Text) != "" {
			p.say("  · " + oneLine(e.Text, 160))
		}
	case core.ReasoningSummary:
		if p.verbose {
			p.say("  ~ " + oneLine(e.Text, 200))
		}
	case core.RunnerError:
		p.say("error: " + e.Message)
	case engine.CompactionStarted:
		p.say(fmt.Sprintf("compacting the context (%s, %s tokens in use)", e.Trigger, commas(e.Tokens)))
	case engine.Compacted:
		if e.Interrupted {
			p.say("compaction interrupted")
		} else if e.Err != "" {
			p.say("compaction failed: " + e.Err)
		} else {
			p.say(fmt.Sprintf("context compacted (%d-char summary)", len(e.Summary)))
			if e.Warning != "" {
				p.say("warning: " + e.Warning)
			}
		}
	case engine.AutoReviewed:
		p.say(fmt.Sprintf("auto-review: %s (%s risk) %s — %s", e.Outcome, e.Risk, oneLine(e.Command, 80), e.Reason))
	case engine.AgentUpdated:
		p.agents[e.ID] = e.Nickname
		if e.State == engine.AgentErrored && e.Message != "" {
			p.say(fmt.Sprintf("agent %s: failed: %s", e.Nickname, e.Message))
		} else {
			p.say(fmt.Sprintf("agent %s: %s", e.Nickname, e.State))
		}
	case engine.AgentActivity:
		if f, ok := e.Event.(core.ToolFinished); ok && p.verbose {
			p.say(fmt.Sprintf("  agent %s: %s  %s (%s, %.1fs)", p.agents[e.ID], f.Name, f.Label, f.Detail, f.Duration.Seconds()))
		}
	case core.RunFinished:
		p.running = false
		r := e.Result
		p.say(fmt.Sprintf("run %s · %.1fs · %d turns · %s tokens", r.Status, r.Wall.Seconds(), r.Stats.Turns,
			commas(r.Stats.Tokens.InputTokens+r.Stats.Tokens.OutputTokens)))
	}
}

func (p *printer) say(msg string) {
	fmt.Fprintf(p.w, "[%6.1fs] %s\n", time.Since(p.origin).Seconds(), msg)
}

func short(id string) string { return session.ShortID(id) }

func modelLabel(model string) string {
	if model == "" {
		return "(provider default)"
	}

	return model
}

func oneLine(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > limit {
		return string(r[:limit-1]) + "…"
	}

	return s
}

func commas(n int64) string {
	s := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 && c != '-' {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}

	return b.String()
}

func sandboxLabel(mode string) string {
	if mode == "" {
		return "none"
	}

	return mode
}
