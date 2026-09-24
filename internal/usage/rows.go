package usage

import (
	"strconv"
	"strings"
	"time"
)

// Row is one window as uah shows it: in `uah usage`, /status, the footer,
// and the warnings.
type Row struct {
	// LimitID is the limit the window belongs to ("codex" for the ordinary one).
	LimitID string
	// Label names the window by its length ("5h", "weekly"), after the
	// limit's name for an additional limit.
	Label  string
	Window Window
}

// Rows lists every window: the ordinary limit's first, primary before
// secondary, then the additional limits'.
func (s Snapshot) Rows() []Row {
	var rows []Row
	for _, l := range s.Limits {
		for i, w := range []*Window{l.Primary, l.Secondary} {
			if w == nil {
				continue
			}
			label := w.Label(i == 1)
			if l.ID != CodexLimitID {
				name := l.Name
				if name == "" {
					name = l.ID
				}
				label = name + " " + label
			}
			rows = append(rows, Row{LimitID: l.ID, Label: label, Window: *w})
		}
	}

	return rows
}

// Tightest is the ordinary limit's window with the least left, or any
// limit's when the ordinary one has no window. ok is false without windows.
func (s Snapshot) Tightest() (Row, bool) {
	var best Row
	found := false
	for _, codexOnly := range []bool{true, false} {
		for _, r := range s.Rows() {
			if codexOnly && r.LimitID != CodexLimitID {
				continue
			}
			if !found || r.Window.UsedPercent > best.Window.UsedPercent {
				best, found = r, true
			}
		}
		if found {
			return best, true
		}
	}

	return Row{}, false
}

// Left is "78% left", the percent rounded down so 99.6% used never reads as
// 1% left when less is.
func (r Row) Left() string {
	return strconv.Itoa(int(r.Window.LeftPercent())) + "% left"
}

// Text is "weekly 78% left (resets 15:44 on 26 Sep)".
func (r Row) Text(now time.Time) string {
	s := r.Label + " " + r.Left()
	if !r.Window.ResetsAt.IsZero() {
		s += " (resets " + ResetLabel(r.Window.ResetsAt, now) + ")"
	}

	return s
}

// BarCells is the width of Bar in cells, as Codex's status card draws it.
const BarCells = 20

// Bar draws the percent left as filled cells: "[███████████░░░░░░░░░]".
func Bar(left float64) string {
	filled := int(min(max(left, 0), 100) * BarCells / 100)

	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", BarCells-filled) + "]"
}

// Reached reports whether a limit is used up: the backend says so, or a
// window is at 100%.
func (s Snapshot) Reached() bool {
	for _, l := range s.Limits {
		if l.Reached != nil && *l.Reached {
			return true
		}
	}
	for _, r := range s.Rows() {
		if r.Window.UsedPercent >= 100 {
			return true
		}
	}

	return false
}

// BlockedUntil is when the usage frees up: the latest reset among the
// windows at 100%, else the tightest window's reset. ok is false without a
// reset time.
func (s Snapshot) BlockedUntil() (time.Time, bool) {
	var at time.Time
	for _, r := range s.Rows() {
		if r.Window.UsedPercent >= 100 && r.Window.ResetsAt.After(at) {
			at = r.Window.ResetsAt
		}
	}
	if at.IsZero() {
		if r, ok := s.Tightest(); ok {
			at = r.Window.ResetsAt
		}
	}

	return at, !at.IsZero()
}

// WarnAt are the percents used at which uah warns once per window.
var WarnAt = []float64{75, 90, 95}

// Crossed is the highest of WarnAt the window has reached (0 for none).
func (w Window) Crossed() float64 {
	crossed := 0.0
	for _, t := range WarnAt {
		if w.UsedPercent >= t {
			crossed = t
		}
	}

	return crossed
}
