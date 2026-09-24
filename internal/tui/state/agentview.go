package state

import (
	"fmt"
	"strings"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/session"
)

// AgentView is a subagent's live transcript, shown in place of the
// session's own while the session keeps running: St is reduced from the
// agent's events as the session's state is from its own.
type AgentView struct {
	ID, Nickname string
	St           *State
}

// Messages and intents of the agent view.
type (
	// AgentViewOpened carries what the agent did so far: its runs from
	// before this process and its session's events since.
	AgentViewOpened struct {
		ID, Nickname string
		History      []session.LoadedRun
		Events       []core.Event
	}
	// AgentEvents are the viewed agent's next events.
	AgentEvents struct {
		ID     string
		Events []core.Event
	}
	// CloseAgentView returns to the session's own transcript.
	CloseAgentView struct{}
)

// Effects of the agent view.
type (
	// EffViewAgent starts following an agent, by ID.
	EffViewAgent struct{ ID string }
	// EffCloseAgentView stops following it.
	EffCloseAgentView struct{}
	// EffAgentSend gives the viewed agent a message, as send_input does.
	EffAgentSend struct{ ID, Text string }
)

func (EffViewAgent) effect()      {}
func (EffCloseAgentView) effect() {}
func (EffAgentSend) effect()      {}

// cmdAgentsName is the /agents command's name.
const cmdAgentsName = "agents"

// cmdAgents lists the subagents, or opens one's transcript by nickname or
// ID (prefix).
func cmdAgents(s *State, args string) []Effect {
	if args == "" {
		listAgents(s)

		return nil
	}
	for _, it := range s.Items {
		if it.Kind == KindAgent && (strings.EqualFold(it.Name, args) || strings.HasPrefix(it.Text, args)) {
			return []Effect{EffViewAgent{ID: it.Text}}
		}
	}
	s.notice(session.LevelError, fmt.Sprintf("no agent %q in this session (see /agents)", args))

	return nil
}

// agentNames are the nicknames /agents completes.
func (s State) agentNames() []string {
	var names []string
	for _, it := range s.Items {
		if it.Kind == KindAgent {
			names = append(names, it.Name)
		}
	}

	return names
}

// openAgentView shows the agent's transcript, rebuilt from what it did so
// far with the reducer the session's transcript uses.
func (s *State) openAgentView(e AgentViewOpened) {
	st := New(s.Now)
	st.Caps, st.Details, st.ShowReasoning = s.Caps, s.Details, s.ShowReasoning
	if len(e.History) > 0 {
		st, _ = Reduce(st, HistoryLoaded{SessionID: e.ID, Runs: e.History})
	}
	for _, ev := range e.Events {
		st, _ = Reduce(st, ev)
	}
	s.View = &AgentView{ID: e.ID, Nickname: e.Nickname, St: &st}
}

// onAgentView handles what the agent view takes over while it is open:
// the agent's events, messages (they go to the agent), esc (back to the
// session), and the view keys. Commands other than /agents and /quit
// belong to the session and wait until esc. It reports false for the rest,
// which the session's state handles.
func (s *State) onAgentView(ev any) ([]Effect, bool) {
	v := s.View
	switch e := ev.(type) {
	case AgentEvents:
		if e.ID == v.ID {
			for _, x := range e.Events {
				*v.St, _ = Reduce(*v.St, x)
			}
		}
	case CloseAgentView, Esc:
		s.View = nil

		return []Effect{EffCloseAgentView{}}, true
	case Submit, Steer:
		text := strings.TrimSpace(textOf(e))
		name, _, _ := strings.Cut(strings.TrimPrefix(text, "/"), " ")
		switch {
		case text == "":
		case strings.HasPrefix(text, "/") && (name == cmdAgentsName || name == "quit" || name == "exit"):
			return nil, false
		case strings.HasPrefix(text, "/"):
			v.St.notice(session.LevelWarning, fmt.Sprintf("/%s is for the main agent; esc returns to it", name))
		default:
			v.St.Scroll = 0

			return []Effect{EffAgentSend{ID: v.ID, Text: text}}, true
		}
	case ScrollBy, ScrollToBottom, ToggleDetails, ToggleReasoning:
		*v.St, _ = Reduce(*v.St, ev)
	default:
		return nil, false
	}

	return nil, true
}

func textOf(ev any) string {
	switch e := ev.(type) {
	case Submit:
		return e.Text
	case Steer:
		return e.Text
	}

	return ""
}
