// Package usage reads the ChatGPT subscription's rate limits for the
// openai-codex provider, as Codex does: the windows (for example 5 hours and
// a week), the percent used, and when each resets. A Reader serves uah usage,
// /status, the TUI's footer and warnings, and uah doctor;
// docs/design/usage.md has the design.
package usage

import (
	"strconv"
	"time"
)

// Snapshot is the subscription's usage at one moment.
type Snapshot struct {
	// Plan is the ChatGPT plan, such as "plus" or "pro"; empty when unknown.
	Plan string
	// Limits holds the ordinary Codex limit first (ID "codex"), then any
	// per-model limits the backend adds.
	Limits []Limit
	// Credits is nil when the backend sent no credits.
	Credits *Credits
	// ReachedType is the backend's reason when a limit is reached, such as
	// "rate_limit_reached" or "workspace_member_credits_depleted".
	ReachedType string
	// CapturedAt is when uah read the snapshot.
	CapturedAt time.Time
}

// Limit is one metered limit with up to two windows.
type Limit struct {
	// ID is "codex" for the ordinary limit, else the backend's metered feature.
	ID string
	// Name is the display name the backend gives an additional limit.
	Name string
	// Allowed and Reached are the backend's own verdicts; nil when unknown.
	Allowed *bool
	Reached *bool
	// Primary is the shorter window (5 hours today) and Secondary the longer
	// one (a week today). Either can be nil.
	Primary   *Window
	Secondary *Window
}

// Window is one rolling usage window.
type Window struct {
	// UsedPercent is 0 to 100.
	UsedPercent float64
	// Minutes is the window's length; 0 when unknown.
	Minutes int64
	// ResetsAt is zero when unknown.
	ResetsAt time.Time
}

// Credits is the account's extra-usage credit state.
type Credits struct {
	HasCredits bool `json:"has_credits"`
	Unlimited  bool `json:"unlimited"`
	// Balance is the backend's string, such as "12.50"; empty when not sent.
	Balance string `json:"balance,omitempty"`
}

// Codex returns the ordinary Codex limit, the one Codex shows first.
func (s Snapshot) Codex() (Limit, bool) {
	for _, l := range s.Limits {
		if l.ID == CodexLimitID {
			return l, true
		}
	}

	return Limit{}, false
}

// CodexLimitID is the ID of the ordinary limit.
const CodexLimitID = "codex"

// LeftPercent is the percent left, clamped to 0..100.
func (w Window) LeftPercent() float64 {
	return min(max(100-w.UsedPercent, 0), 100)
}

// Label names a window as Codex does: "5h", "daily", "weekly", "monthly",
// "annual" when the length is within 5% of one of them, else the fallback
// (Codex rust-v0.156.1, tui/src/chatwidget/rate_limits.rs:103-146).
func (w Window) Label(secondary bool) string {
	const hour, day = 60, 24 * 60
	for _, n := range []struct {
		minutes int64
		name    string
	}{{5 * hour, "5h"}, {day, "daily"}, {7 * day, "weekly"}, {30 * day, "monthly"}, {365 * day, "annual"}} {
		m, e := float64(w.Minutes), float64(n.minutes)
		if w.Minutes > 0 && m >= e*0.95 && m <= e*1.05 {
			return n.name
		}
	}
	if secondary {
		return "secondary usage"
	}

	return "usage"
}

// String is a one-line summary, such as "5h 55% left (resets 09:25)".
func (w Window) String(secondary bool, now time.Time) string {
	s := w.Label(secondary) + " " + strconv.FormatFloat(w.LeftPercent(), 'f', 0, 64) + "% left"
	if !w.ResetsAt.IsZero() {
		s += " (resets " + ResetLabel(w.ResetsAt, now) + ")"
	}

	return s
}

// ResetLabel formats a reset time as Codex's status card does: the clock
// time when it is today, else the time and the day ("09:25 on 26 Sep";
// tui/src/status/helpers.rs:183-190).
func ResetLabel(at, now time.Time) string {
	at = at.In(now.Location())
	y1, m1, d1 := at.Date()
	y2, m2, d2 := now.Date()
	if y1 == y2 && m1 == m2 && d1 == d2 {
		return at.Format("15:04")
	}

	return at.Format("15:04 on 2 Jan")
}

// Stale reports whether the snapshot is older than Codex's 15-minute
// threshold (tui/src/status/rate_limits.rs:64-65).
func (s Snapshot) Stale(now time.Time) bool {
	return now.Sub(s.CapturedAt) > StaleAfter
}

// StaleAfter is how old a snapshot can get before it is shown as stale.
const StaleAfter = 15 * time.Minute
