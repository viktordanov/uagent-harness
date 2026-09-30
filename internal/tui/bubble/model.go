// Package bubble is the Bubble Tea shell around the TUI state and renderer:
// it turns keys into intents, runs effects against the session, batches
// session events, and draws frames. Everything else lives in state and render.
package bubble

import (
	"context"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/compaction"
	"github.com/viktordanov/uagent-harness/internal/history"
	"github.com/viktordanov/uagent-harness/internal/images"
	"github.com/viktordanov/uagent-harness/internal/images/clipboard"
	"github.com/viktordanov/uagent-harness/internal/models"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/tui/render"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
	"github.com/viktordanov/uagent-harness/internal/usage"
)

const (
	batchWindow  = 16 * time.Millisecond
	tickInterval = 100 * time.Millisecond
)

// Deps are what the TUI needs from the command that starts it.
type Deps struct {
	// Open opens a session: "" starts a new one, otherwise it resumes that ID.
	// It returns the saved runs of a resumed session.
	Open func(ctx context.Context, id string) (*session.Session, []session.LoadedRun, error)
	// Sessions lists sessions for the picker.
	Sessions func() ([]session.Info, error)
	// Models lists the provider's models for /model (optional). It may call
	// the provider, so it runs off the update loop.
	Models func(ctx context.Context, provider string) models.Catalog
	// Activity counts recent runs per day for /status (optional).
	Activity func() (map[string]int, error)
	// SessionID is the session to open first ("" for a new one).
	SessionID string
	// Prompt, when set, is sent once the first session is open.
	Prompt string
	// Cwd scopes the picker to sessions of this directory, as Codex does.
	Cwd string
	// Picker opens the session picker first instead of a session.
	Picker bool
	// AllSessions starts the picker showing every directory.
	AllSessions bool
	// Details starts in the detailed view: turns, run dividers, and tokens.
	Details bool
	// Mouse reports the mouse, so the wheel scrolls and a drag selects
	// transcript text; off, the terminal selects text and its wheel sends
	// ↑ and ↓.
	Mouse bool
	// Title shows idle, working, or waiting for an approval in the
	// terminal's title; off, uah leaves the title alone.
	Title bool
	// SteerKey picks the send-now key ([tui] steer_key); auto follows the
	// terminal's answer to the keyboard enhancement query.
	SteerKey state.SteerKey
	// CopyText writes text to the system clipboard with its own tool, next
	// to OSC 52 (optional; internal/images/clipboard.WriteText).
	CopyText func(ctx context.Context, text string) error
	// Config loads the effective configuration for /config: the user file
	// and each key's value and source (optional). SaveConfig writes one key
	// to the user file, keeping its comments; a nil value removes it.
	Config     func(ctx context.Context) state.ConfigLoaded
	SaveConfig func(key string, value any) error
	// Version is uah's version, for the banner.
	Version string
	// Windows finds a model's context window in the model catalog, for the
	// footer's context meter (nil: the default window).
	Windows compaction.WindowLookup
	// Usage reads the subscription's usage for /status, the footer, and the
	// warnings (nil: none, as for a provider without usage).
	Usage usage.Reader
	// Now is the clock (default time.Now).
	Now func() time.Time
	// Images stores images pasted into the composer, and Clipboard reads
	// the clipboard's image for ctrl+v (optional; without them pasting an
	// image says it cannot).
	Images    *images.Store
	Clipboard clipboard.Reader
	// WritableRoots are the sandbox's extra writable roots, absolute; ctrl+g
	// checks that sandboxed commands cannot write its draft file.
	WritableRoots []string
	// Exec runs the editor for ctrl+g with the terminal released (default
	// tea.Exec); tests run it directly.
	Exec func(tea.ExecCommand, tea.ExecCallback) tea.Cmd
	// History is the prompt history file for ↑ and ctrl+r (nil: this
	// process's prompts only).
	History *history.File
}

// Model is the Bubble Tea model.
type Model struct {
	ctx   context.Context
	deps  Deps
	st    state.State
	cache *render.Cache
	// theme is the cache's theme, for what uah prints after the TUI.
	theme    render.Theme
	composer textarea.Model
	w, h     int

	sess     *session.Session
	gen      int // increases with each session; stale events are dropped
	ticking  bool
	prompted bool
	// held are effects that need a session, made before the first one opened.
	held []state.Effect
	// watch follows the agent the view shows; watchGen drops its events
	// once it ends.
	watch    *session.AgentWatch
	watchGen int
	// prompts appends to the history file in order (nil without one).
	prompts *history.Recorder
}

// Messages from goroutines and commands.
type (
	eventsMsg struct {
		gen     int
		events  []core.Event
		batches <-chan []core.Event
	}
	sessionClosedMsg struct{ gen int }
	openedMsg        struct {
		sess    *session.Session
		history []session.LoadedRun
	}
	withdrawnMsg struct{ text string }
	quitMsg      struct{}
	tickMsg      time.Time
)

// New returns the model; attach it to a program with Run.
func New(ctx context.Context, deps Deps) Model {
	if deps.Now == nil {
		deps.Now = time.Now
	}

	st := state.New(deps.Now())
	st.Details, st.Mouse, st.Title, st.Windows = deps.Details, deps.Mouse, deps.Title, deps.Windows
	st.Home, _ = os.UserHomeDir()
	st.Keys.Steer = deps.SteerKey

	m := Model{
		ctx: ctx, deps: deps, st: st,
		cache: render.NewCache(render.Amber), theme: render.Amber, composer: newComposer(render.NewStyles(render.Amber)),
	}
	if deps.History != nil {
		m.prompts = history.NewRecorder(*deps.History)
	}

	return m
}

// onBackground picks the theme for the terminal's background.
func (m Model) onBackground(msg tea.BackgroundColorMsg) Model {
	m.theme = render.ThemeFor(msg.Color)
	m.cache = render.NewCache(m.theme)
	m.composer.SetStyles(composerStyles(m.cache.Styles()))

	return m
}

// Run starts the program and blocks until it exits.
func Run(ctx context.Context, deps Deps, opts ...tea.ProgramOption) (Exit, error) {
	p := tea.NewProgram(New(ctx, deps), append([]tea.ProgramOption{tea.WithContext(ctx)}, opts...)...)
	final, err := p.Run()
	fm, ok := final.(Model)
	if !ok {
		return Exit{}, err //nolint:wrapcheck // the caller wraps it
	}
	if fm.sess != nil {
		_ = fm.sess.Close() // the program ended without /quit, for example on SIGTERM
	}

	return fm.Exit(), err //nolint:wrapcheck // the caller wraps it
}

func (m Model) Init() tea.Cmd {
	// The theme follows the terminal's background once it answers.
	if m.deps.Picker {
		return tea.Batch(tea.RequestBackgroundColor, m.run(state.EffLoadPrompts{}), m.run(state.EffLoadSessions{}))
	}

	return tea.Batch(tea.RequestBackgroundColor, m.run(state.EffLoadPrompts{}), m.open(m.deps.SessionID))
}

// onTerminalReport takes the terminal's answers to Bubble Tea's startup
// queries: its background color, and whether it tells ctrl+enter from
// enter (no answer at all, as from tmux, keeps the plain-key bindings).
func (m Model) onTerminalReport(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		return m.onBackground(msg), nil
	case tea.KeyboardEnhancementsMsg:
		return m.dispatch(state.KeyboardReported{Disambiguates: msg.SupportsKeyDisambiguation()})
	}

	return m, nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.resizeComposer()

		return m, nil
	case tea.BackgroundColorMsg, tea.KeyboardEnhancementsMsg:
		return m.onTerminalReport(msg)
	case tea.MouseWheelMsg:
		return m.onWheel(msg)
	case tea.MouseClickMsg, tea.MouseMotionMsg, tea.MouseReleaseMsg:
		return m.onMouse(msg)
	case tea.KeyPressMsg:
		return m.now().onKey(msg)
	case tea.PasteMsg:
		return m.now().onPaste(msg)
	case eventsMsg:
		if msg.gen != m.gen {
			return m, next(msg.gen, msg.batches) // drain a closed session's last events
		}
		cmds := []tea.Cmd{next(m.gen, msg.batches)}
		for _, e := range msg.events {
			var effects []state.Effect
			m.st, effects = state.Reduce(m.st, e)
			for _, eff := range effects { // such as reading the usage after a run
				cmds = append(cmds, m.run(eff))
			}
		}
		// After the events: a batch that starts a run starts the clock.
		cmds = append(cmds, m.afterChange())

		return m, tea.Batch(cmds...)
	case sessionClosedMsg:
		if msg.gen == m.gen {
			m.sess = nil
		}

		return m, nil
	case openedMsg:
		return m.onOpened(msg)
	case agentOpenedMsg, agentEventsMsg, agentWatchEndedMsg:
		return m.onAgentMsg(msg)
	case withdrawnMsg:
		return m.dispatch(state.DraftRestored{Text: msg.text})
	case tickMsg:
		m = m.now()
		m.ticking = false

		return m, m.afterChange()
	case quitMsg:
		m.sess = nil

		return m, tea.Quit
	case state.Failed, state.SessionsLoaded, state.ActivityLoaded, state.FilesLoaded, state.MCPListed, state.ContextShown,
		state.ModelsLoaded, state.ConfigLoaded, state.ConfigSaved, state.ImageAttached, state.ImageFailed, state.DraftEdited:
		return m.dispatch(msg)
	case state.UsageLoaded, state.Copied, state.DiffShown, state.ReviewTargetsLoaded, state.PromptsLoaded:
		return m.dispatch(msg)
	}
	var cmd tea.Cmd
	m.composer, cmd = m.composer.Update(msg)

	return m, cmd
}

func (m Model) onOpened(msg openedMsg) (tea.Model, tea.Cmd) {
	m.gen++
	m.stopWatch()
	m.sess = msg.sess
	m.st.Priority = msg.sess.Priority()
	m.st.Yolo = msg.sess.Yolo()
	if len(msg.history) > 0 {
		m.st, _ = state.Reduce(m.st, state.HistoryLoaded{SessionID: msg.sess.ID(), Runs: msg.history})
	}
	batches := make(chan []core.Event)
	go batch(msg.sess.Events(), batches)
	cmds := []tea.Cmd{next(m.gen, batches)}
	for _, e := range m.held {
		cmds = append(cmds, m.run(e))
	}
	m.held = nil
	if !m.prompted && m.deps.Prompt != "" {
		m.prompted = true
		updated, cmd := m.dispatch(state.Submit{Text: m.deps.Prompt})

		return updated, tea.Batch(append(cmds, cmd)...)
	}

	return m, tea.Batch(cmds...)
}

// dispatch reduces an intent and runs the effects it returns.
func (m Model) dispatch(intent any) (tea.Model, tea.Cmd) {
	var effects []state.Effect
	shell := m.st.Shell
	m.st, effects = state.Reduce(m.st, intent)
	m.syncShell(shell)
	cmds := []tea.Cmd{m.afterChange()}
	for _, e := range effects {
		if d, ok := e.(state.EffSetDraft); ok {
			m.composer.SetValue(d.Text)
			m.composer.CursorEnd()

			continue
		}
		if ins, ok := e.(state.EffInsertText); ok {
			m.composer.InsertString(ins.Text)

			continue
		}
		if _, ok := e.(state.EffCloseAgentView); ok {
			m.stopWatch()

			continue
		}
		if m.sess == nil && needsSession(e) {
			m.held = append(m.held, e) // sent once the session opens

			continue
		}
		cmds = append(cmds, m.run(e))
	}

	return m, tea.Batch(cmds...)
}

// now sets the state's clock to deps.Now. The clock ticks only while
// something moves on screen, so a key sets it first: a first esc after the
// session sat idle is timed from the key, not from the last tick.
func (m Model) now() Model {
	m.st, _ = state.Reduce(m.st, state.Tick{Now: m.deps.Now()})

	return m
}

// afterChange keeps the clock ticking while anything moves on screen.
func (m *Model) afterChange() tea.Cmd {
	moving := m.st.Busy || m.st.Live != nil || m.st.Status != "" || m.st.AgentsRunning() || m.st.ShellRunning()
	if v := m.st.View; v != nil {
		moving = moving || v.St.Busy || v.St.Live != nil // the viewed agent's spinner
	}
	if m.ticking || !moving {
		return nil
	}
	m.ticking = true

	return tea.Tick(tickInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// frame is what render.Screen draws around the state.
func (m Model) frame() render.Frame {
	return render.Frame{
		Width: m.w, Height: m.h, Composer: m.composer.View(), ComposerHeight: m.composer.Height(), Draft: m.composer.Value(), Version: m.deps.Version,
	}
}

func (m Model) View() tea.View {
	content, composerRow := render.Screen(m.st, m.cache, m.frame())
	v := tea.NewView(content)
	v.AltScreen = true
	// With the mouse reported, which is the default, wheel events scroll
	// the transcript and a drag selects its text (mouse.go); the terminal's
	// own selection needs its modifier (Option in iTerm2 and Terminal,
	// Shift in most others). Without it, the terminal selects text and
	// turns the wheel into ↑ and ↓ (keys.go).
	if m.st.Mouse {
		v.MouseMode = tea.MouseModeCellMotion
	}
	// Bubble Tea clears the title when the program ends.
	v.WindowTitle = m.st.WindowTitle()
	if c := m.composer.Cursor(); c != nil && composerRow >= 0 {
		c.Y += composerRow
		v.Cursor = c
		m.searchCursor(c, composerRow)
	}

	return v
}

func trimmed(s string) string { return strings.TrimSpace(s) }

// needsSession reports whether an effect talks to the open session.
func needsSession(e state.Effect) bool {
	switch e.(type) {
	case state.EffSubmit, state.EffSteer, state.EffSteerQueued, state.EffSetSettings, state.EffShell:
		return true
	}

	return false
}
