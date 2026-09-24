// Package state is the TUI's model and reducer. It has no terminal or
// framework code: Reduce folds events and user intents into State and returns
// effects for the shell to run, so any renderer can drive it and plain tests
// can check it.
package state

import (
	"time"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/compaction"
	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/images"
	"github.com/viktordanov/uagent-harness/internal/session"
)

// Mode is the screen being shown.
type Mode int

const (
	ModeChat Mode = iota
	ModePicker
)

// Queued is a message waiting for the agent.
type Queued struct {
	ID   string
	Text string
}

// Live is the run in progress.
type Live struct {
	RunID     string
	Started   time.Time
	TurnSince time.Time // zero when the model is not generating
	Tools     int       // tools running now
}

// Totals add up the session's finished runs.
type Totals struct {
	Runs        int
	Turns       int
	ToolCalls   int
	MaxParallel int
	Tokens      core.Tokens
	Overlap     time.Duration
	ToolBusy    time.Duration
}

// Picker is the session list. Like Codex, it shows the sessions of the
// current directory unless All is set.
type Picker struct {
	Local    []session.Info // sessions whose workspace is the current directory
	Sessions []session.Info // every session
	All      bool
	Filter   string
	Selected int
}

// State is everything the TUI shows.
type State struct {
	Mode      Mode
	SessionID string
	Resumed   bool
	Engine    string
	Caps      engine.Capabilities
	Settings  session.Settings
	Files     []string // instruction files in the prompt

	Items []Item
	index map[string]int

	Queue []Queued
	// Approvals are commands waiting for the user, in order.
	Approvals []Approval
	Live      *Live
	Busy      bool // from a message sent until the session is idle
	Totals    Totals
	// ContextUsed is the tokens the last response used (0: unknown).
	ContextUsed int64

	ShowReasoning bool
	// Details shows turns, run dividers, and token totals; the default is a
	// compact, Codex-like view.
	Details bool
	Scroll  int // lines scrolled up from the bottom
	Picker  Picker
	Menu    Menu
	Now     time.Time
	// Windows finds a model's context window in the session's model
	// catalog, for the footer's "N% context left" (nil: the default
	// window). The shell sets it; the reducer only calls it.
	Windows compaction.WindowLookup
	// agentIDs are the subagents' IDs in the order they started, so lookups
	// of the agents do not walk the whole transcript (Agents).
	agentIDs []string
	// viewGen numbers the agent views opened (AgentView.Gen).
	viewGen int
	// View, when set, shows a subagent's transcript instead of the
	// session's (see agentview.go).
	View *AgentView
	// Config, when set, is the /config panel (see config.go).
	Config *ConfigPanel
	// Mouse reports the mouse to the TUI, so the wheel scrolls.
	Mouse bool
	// Attached are the images pasted into the composer, in order; each
	// placeholder in the draft names one (see images.go).
	Attached []images.Image

	// Status is a transient hint in the footer, such as a pending confirmation.
	Status      string
	escArmed    time.Time
	quitArmed   time.Time
	Quitting    bool
	nextNoticeN int
}

// New returns an empty state.
func New(now time.Time) State {
	return State{index: map[string]int{}, Now: now}
}

// Intents are what the user asks for, translated from keys by the shell.
type (
	// Submit is Enter with the composer text; text starting with "/" is a command.
	Submit struct{ Text string }
	// Steer is Ctrl+Enter with the composer text.
	Steer struct{ Text string }
	// Esc is the Escape key; twice while busy interrupts.
	Esc struct{}
	// Quit is Ctrl+C with an empty composer; twice while busy quits.
	Quit struct{}
	// EditLastQueued is Up on an empty composer.
	EditLastQueued struct{}
	// ToggleReasoning shows or hides reasoning summaries.
	ToggleReasoning struct{}
	// ToggleDetails switches between the compact and the detailed view.
	ToggleDetails struct{}
	// ScrollBy scrolls the transcript; positive is up.
	ScrollBy struct{ Lines int }
	// ScrollToBottom follows new output again.
	ScrollToBottom struct{}
	// StepEffort lowers (-1) or raises (+1) the effort.
	StepEffort struct{ Delta int }
	// OpenPicker loads the session list.
	OpenPicker struct{}
	// Tick advances the clock for timers and spinners.
	Tick struct{ Now time.Time }
	// HistoryLoaded fills the transcript of a resumed session before it opens.
	HistoryLoaded struct {
		SessionID string
		Runs      []session.LoadedRun
	}
	// SessionsLoaded fills the picker. Local holds the current directory's
	// sessions; All shows every session from the start.
	SessionsLoaded struct {
		Sessions []session.Info
		Local    []session.Info
		All      bool
	}
	// ActivityLoaded carries runs per day (YYYY-MM-DD) for /status.
	ActivityLoaded struct {
		Counts map[string]int
	}
	// PickerToggleAll switches between this directory and all sessions.
	PickerToggleAll struct{}
	// PickerMove moves the picker selection.
	PickerMove struct{ Delta int }
	// PickerType edits the picker filter; Backspace is Text "\b".
	PickerType struct{ Text string }
	// PickerChoose opens the selected session.
	PickerChoose struct{}
	// PickerCancel closes the picker.
	PickerCancel struct{}
	// Failed reports an effect that failed.
	Failed struct{ Err error }
)

// Filtered returns the picker sessions in scope that match the filter.
func (p Picker) Filtered() []session.Info {
	list := p.Local
	if p.All {
		list = p.Sessions
	}
	if p.Filter == "" {
		return list
	}
	var out []session.Info
	for _, s := range list {
		if containsFold(s.ID, p.Filter) || containsFold(s.FirstPrompt, p.Filter) || containsFold(s.Model, p.Filter) {
			out = append(out, s)
		}
	}

	return out
}

// Item returns the item with key, if any.
func (s State) Item(key string) (Item, bool) {
	i, ok := s.index[key]
	if !ok {
		return Item{}, false
	}

	return s.Items[i], true
}
