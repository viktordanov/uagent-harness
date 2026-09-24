package render_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/tui/state"
	"github.com/viktordanov/uagent-harness/internal/usage"
)

// proWeekly is a Pro login's usage: only a weekly window, sent as the
// primary one.
func proWeekly(used float64) usage.Snapshot {
	return usage.Snapshot{Plan: "pro", CapturedAt: t0, Limits: []usage.Limit{{
		ID: usage.CodexLimitID, Primary: &usage.Window{UsedPercent: used, Minutes: 7 * 24 * 60, ResetsAt: t0.Add(51 * time.Hour)},
	}}}
}

func TestFooterShowsTheTightestWindow(t *testing.T) {
	s := apply(finishedRun(t), core.ModelResponded{At: t0, Turn: 3, Usage: core.Tokens{InputTokens: 180_000, OutputTokens: 2_000}})
	assert.NotContains(t, footer(s), "weekly", "hidden before a read")

	s = apply(s, state.UsageLoaded{Reason: state.UsageAfterRun, Snapshot: proWeekly(22), At: t0})
	assert.Contains(t, footer(s), "weekly 78% left · 35% context left · ctrl+t details")

	s.Details = true
	assert.Contains(t, footer(s), "35% context left · weekly 78%")
}

func TestStatusShowsTheUsage(t *testing.T) {
	s := apply(base(), state.Submit{Text: "/status"},
		state.UsageLoaded{Reason: state.UsageStatus, Snapshot: proWeekly(22), At: t0},
		state.UsageLoaded{Reason: state.UsageAfterRun, Snapshot: proWeekly(91), At: t0})
	golden(t, "usage", screen(s, ""))
	assert.True(t, strings.Contains(screen(s, ""), "weekly [███████████████░░░░░] 78% left (resets 15:00 on 26 Sep)"))
}
