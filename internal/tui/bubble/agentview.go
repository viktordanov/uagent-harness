package bubble

import (
	tea "charm.land/bubbletea/v2"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// Messages of the agent view: a watch that opened, and a batch of the
// watched agent's events. gen drops batches of a watch that ended.
type (
	agentOpenedMsg struct{ w *session.AgentWatch }
	agentEventsMsg struct {
		gen     int
		id      string
		events  []core.Event
		batches <-chan []core.Event
	}
	// agentWatchEndedMsg says a watch's events ended.
	agentWatchEndedMsg struct {
		gen int
		id  string
	}
)

// onAgentMsg routes the agent view's messages.
func (m Model) onAgentMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case agentOpenedMsg:
		return m.onAgentOpened(msg)
	case agentEventsMsg:
		return m.onAgentEvents(msg)
	case agentWatchEndedMsg:
		return m.onAgentWatchEnded(msg)
	}

	return m, nil
}

// watchAgent starts following an agent of the session.
func (m Model) watchAgent(id string) tea.Cmd {
	sess := m.sess

	return func() tea.Msg {
		if sess == nil {
			return state.Failed{Err: errNoSession}
		}
		w, err := sess.WatchAgent(id)
		if err != nil {
			return state.Failed{Err: err}
		}

		return agentOpenedMsg{w: w}
	}
}

// onAgentOpened shows the agent's transcript and follows its events, in
// batches as the session's are.
func (m Model) onAgentOpened(msg agentOpenedMsg) (tea.Model, tea.Cmd) {
	m.stopWatch()
	m.watch = msg.w
	m.watchGen++
	batches := make(chan []core.Event)
	go batch(msg.w.Next, batches)
	w := msg.w
	updated, cmd := m.dispatch(state.AgentViewOpened{ID: w.ID, Nickname: w.Nickname, History: w.History, Events: w.Events})

	return updated, tea.Batch(cmd, nextAgent(m.watchGen, w.ID, batches))
}

func (m Model) onAgentEvents(msg agentEventsMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.watchGen {
		// A watch that ended: keep reading until its batches close, so its
		// batch goroutine is not left blocked on a send.
		return m, nextAgent(msg.gen, msg.id, msg.batches)
	}
	m.st, _ = state.Reduce(m.st, state.AgentEvents{ID: msg.id, Events: msg.events})

	return m, tea.Batch(m.afterChange(), nextAgent(msg.gen, msg.id, msg.batches))
}

// onAgentWatchEnded reopens the view when the watch closed while it was
// still shown: the manager closes a view that fell a whole queue behind,
// and opening it again catches up.
func (m Model) onAgentWatchEnded(msg agentWatchEndedMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.watchGen || m.watch == nil || m.st.View == nil || m.st.View.ID != msg.id {
		return m, nil
	}

	return m, m.watchAgent(msg.id)
}

// nextAgent waits for the watched agent's next batch, and reports when its
// events end.
func nextAgent(gen int, id string, batches <-chan []core.Event) tea.Cmd {
	return func() tea.Msg {
		events, ok := <-batches
		if !ok {
			return agentWatchEndedMsg{gen: gen, id: id}
		}

		return agentEventsMsg{gen: gen, id: id, events: events, batches: batches}
	}
}

// stopWatch ends the current watch, if any.
func (m *Model) stopWatch() {
	if m.watch != nil {
		m.watch.Stop()
		m.watch = nil
		m.watchGen++
	}
}

// sendToAgent gives the watched agent a message.
func (m Model) sendToAgent(text string, now bool) tea.Cmd {
	w := m.watch

	return func() tea.Msg {
		if w == nil {
			return nil
		}
		if err := w.Send(text, now); err != nil {
			return state.Failed{Err: err}
		}

		return nil
	}
}
