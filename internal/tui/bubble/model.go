// Package bubble is the Bubble Tea shell around the TUI state and renderer:
// it turns keys into intents, runs effects against the session, batches
// session events, and draws frames. Everything else lives in state and render.
package bubble

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/tui/render"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
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
	// Now is the clock (default time.Now).
	Now func() time.Time
}

// Model is the Bubble Tea model.
type Model struct {
	ctx      context.Context
	deps     Deps
	st       state.State
	cache    *render.Cache
	composer textarea.Model
	w, h     int

	sess     *session.Session
	gen      int // increases with each session; stale events are dropped
	ticking  bool
	prompted bool
	// held are effects that need a session, made before the first one opened.
	held []state.Effect
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
	st.Details = deps.Details

	return Model{
		ctx: ctx, deps: deps, st: st,
		cache: render.NewCache(), composer: newComposer(),
	}
}

func newComposer() textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "message · / for commands · ctrl+enter sends while the agent works"
	ta.ShowLineNumbers = false
	ta.Prompt = "› "
	ta.DynamicHeight = true
	ta.MinHeight = 1
	ta.MaxHeight = 8
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "ctrl+j"))
	styles := textarea.DefaultStyles(true)
	styles.Focused.CursorLine = styles.Focused.Text
	ta.SetStyles(styles)
	ta.SetVirtualCursor(false)
	ta.Focus()

	return ta
}

// Run starts the program and blocks until it exits.
func Run(ctx context.Context, deps Deps, opts ...tea.ProgramOption) error {
	p := tea.NewProgram(New(ctx, deps), append([]tea.ProgramOption{tea.WithContext(ctx)}, opts...)...)
	final, err := p.Run()
	if fm, ok := final.(Model); ok && fm.sess != nil {
		_ = fm.sess.Close() // the program ended without /quit, for example on SIGTERM
	}

	return err
}

func (m Model) Init() tea.Cmd {
	if m.deps.Picker {
		return m.run(state.EffLoadSessions{})
	}

	return m.open(m.deps.SessionID)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.composer.SetWidth(msg.Width)

		return m, nil
	case tea.MouseWheelMsg:
		return m.onWheel(msg)
	case tea.KeyPressMsg:
		return m.onKey(msg)
	case tea.PasteMsg:
		var cmd tea.Cmd
		m.composer, cmd = m.composer.Update(msg)

		return m, cmd
	case eventsMsg:
		if msg.gen != m.gen {
			return m, next(msg.gen, msg.batches) // drain a closed session's last events
		}
		for _, e := range msg.events {
			m.st, _ = state.Reduce(m.st, e)
		}

		return m, tea.Batch(m.afterChange(), next(m.gen, msg.batches))
	case sessionClosedMsg:
		if msg.gen == m.gen {
			m.sess = nil
		}

		return m, nil
	case openedMsg:
		return m.onOpened(msg)
	case withdrawnMsg:
		m.composer.SetValue(msg.text)
		m.composer.CursorEnd()

		return m, nil
	case tickMsg:
		m.st, _ = state.Reduce(m.st, state.Tick{Now: time.Time(msg)})
		m.ticking = false

		return m, m.afterChange()
	case quitMsg:
		m.sess = nil

		return m, tea.Quit
	case state.Failed, state.SessionsLoaded:
		return m.dispatch(msg)
	}
	var cmd tea.Cmd
	m.composer, cmd = m.composer.Update(msg)

	return m, cmd
}

func (m Model) onOpened(msg openedMsg) (tea.Model, tea.Cmd) {
	m.gen++
	m.sess = msg.sess
	m.st.Caps = msg.sess.Capabilities()
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
	m.st, effects = state.Reduce(m.st, intent)
	cmds := []tea.Cmd{m.afterChange()}
	for _, e := range effects {
		if m.sess == nil && needsSession(e) {
			m.held = append(m.held, e) // sent once the session opens

			continue
		}
		cmds = append(cmds, m.run(e))
	}

	return m, tea.Batch(cmds...)
}

// afterChange keeps the clock ticking while anything moves on screen.
func (m *Model) afterChange() tea.Cmd {
	if m.ticking || (!m.st.Busy && m.st.Live == nil && m.st.Status == "") {
		return nil
	}
	m.ticking = true

	return tea.Tick(tickInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) View() tea.View {
	draft := m.composer.Value()
	content, composerRow := render.Screen(m.st, m.cache, render.Frame{
		Width: m.w, Height: m.h, Composer: m.composer.View(), ComposerHeight: m.composer.Height(), Draft: draft,
	})
	v := tea.NewView(content)
	v.AltScreen = true
	// Wheel events scroll the transcript. Terminals still select text with
	// the modifier they use while an app reports the mouse (Option in iTerm2
	// and Terminal, Shift in most others).
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "uah"
	if c := m.composer.Cursor(); c != nil && composerRow >= 0 {
		c.Y += composerRow
		v.Cursor = c
	}

	return v
}

// next waits for the next batch of a session's events. Update re-arms it
// after each batch, so exactly one waits at a time and order is kept.
func next(gen int, batches <-chan []core.Event) tea.Cmd {
	return func() tea.Msg {
		events, ok := <-batches
		if !ok {
			return sessionClosedMsg{gen: gen}
		}

		return eventsMsg{gen: gen, events: events, batches: batches}
	}
}

// batch groups session events into 16 ms batches, so a burst costs one
// update and one frame. It closes out when the session's events end.
func batch(events <-chan core.Event, out chan<- []core.Event) {
	defer close(out)
	for first := range events {
		batchOut := []core.Event{first}
		timer := time.NewTimer(batchWindow)
	collect:
		for {
			select {
			case e, ok := <-events:
				if !ok {
					break collect
				}
				batchOut = append(batchOut, e)
			case <-timer.C:
				break collect
			}
		}
		timer.Stop()
		out <- batchOut
	}
}

func trimmed(s string) string { return strings.TrimSpace(s) }

// needsSession reports whether an effect talks to the open session.
func needsSession(e state.Effect) bool {
	switch e.(type) {
	case state.EffSubmit, state.EffSteer, state.EffSetSettings:
		return true
	}

	return false
}
