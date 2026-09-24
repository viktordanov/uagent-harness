package bubble

import (
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

var (
	errNoSession = errors.New("no session is open")
	errNoConfig  = errors.New("settings are not available here")
)

// run turns an effect into a command that does its I/O off the update loop.
func (m Model) run(e state.Effect) tea.Cmd { //nolint:gocyclo // a dispatch switch over a closed set; see docs/documentation/architecture.md
	sess := m.sess
	fail := func(err error) tea.Msg { return state.Failed{Err: err} }
	withSession := func(fn func(*session.Session) error) tea.Cmd {
		return func() tea.Msg {
			if sess == nil {
				return fail(errNoSession)
			}
			if err := fn(sess); err != nil {
				return fail(err)
			}

			return nil
		}
	}
	if cmd, ok := m.runImage(e); ok {
		return cmd
	}
	switch e := e.(type) {
	case state.EffSubmit:
		return withSession(func(s *session.Session) error { _, err := s.Submit(e.Text); return err })
	case state.EffSteer:
		return withSession(func(s *session.Session) error { _, err := s.SteerNow(e.Text); return err })
	case state.EffShell:
		ctx := m.ctx

		return withSession(func(s *session.Session) error { _, err := s.RunShell(ctx, e.Command); return err })
	case state.EffInterrupt:
		return withSession(func(s *session.Session) error { return s.Interrupt() })
	case state.EffClear:
		return withSession(func(s *session.Session) error { return s.Clear() })
	case state.EffCompact:
		return withSession(func(s *session.Session) error { return s.CompactWith(e.Focus) })
	case state.EffResolve:
		return withSession(func(s *session.Session) error { return s.Resolve(e.ID, e.Answer) })
	case state.EffSetSettings:
		return withSession(func(s *session.Session) error { _, err := s.SetSettings(e.Settings); return err })
	case state.EffWithdraw:
		return func() tea.Msg {
			if sess == nil {
				return fail(errNoSession)
			}
			ok, err := sess.Withdraw(e.ID)
			if err != nil {
				return fail(err)
			}
			if !ok {
				return nil // already sent
			}

			return withdrawnMsg{text: e.Text}
		}
	case state.EffLoadSessions:
		return func() tea.Msg {
			infos, err := m.deps.Sessions()
			if err != nil {
				return fail(err)
			}
			local := infos
			if m.deps.Cwd != "" {
				local = session.InDir(infos, m.deps.Cwd)
			}

			return state.SessionsLoaded{Sessions: infos, Local: local, All: m.deps.AllSessions || m.deps.Cwd == ""}
		}
	case state.EffLoadActivity:
		if m.deps.Activity == nil {
			return nil
		}

		return func() tea.Msg {
			counts, err := m.deps.Activity()
			if err != nil {
				return fail(err)
			}

			return state.ActivityLoaded{Counts: counts}
		}
	case state.EffLoadModels:
		if m.deps.Models == nil {
			return nil
		}

		return func() tea.Msg { return state.ModelsLoaded{Catalog: m.deps.Models(m.ctx, e.Provider)} }
	case state.EffLoadFiles:
		dir := m.deps.Cwd

		return func() tea.Msg { return state.FilesLoaded{Paths: workspaceFiles(m.ctx, dir)} }
	case state.EffLoadConfig:
		if m.deps.Config == nil {
			return func() tea.Msg { return state.ConfigLoaded{Err: errNoConfig} }
		}

		return func() tea.Msg { return m.deps.Config(m.ctx) }
	case state.EffSaveConfig:
		if m.deps.SaveConfig == nil {
			return func() tea.Msg { return state.ConfigSaved{Key: e.Key, Value: e.Value, Err: errNoConfig} }
		}

		return func() tea.Msg {
			return state.ConfigSaved{Key: e.Key, Value: e.Value, Err: m.deps.SaveConfig(e.Key, e.Value)}
		}
	case state.EffListMCP:
		return func() tea.Msg {
			if sess == nil {
				return fail(errNoSession)
			}
			servers, ok := sess.MCPServers()

			return state.MCPListed{Servers: servers, Supported: ok, Verbose: e.Verbose}
		}
	case state.EffContext:
		return func() tea.Msg {
			if sess == nil {
				return fail(errNoSession)
			}
			u, ok := sess.ContextUsage()

			return state.ContextShown{Usage: u, OK: ok}
		}
	case state.EffOpenSession:
		return m.switchTo(e.ID)
	case state.EffViewAgent:
		return m.watchAgent(e.ID)
	case state.EffAgentSend:
		return m.sendToAgent(e.Text, e.Now)
	case state.EffAgentInterrupt:
		if w := m.watch; w != nil && w.Interrupt != nil {
			w.Interrupt()
		}

		return nil
	case state.EffQuit:
		return func() tea.Msg {
			if sess != nil {
				_ = sess.Close()
			}

			return quitMsg{}
		}
	}

	return nil
}

// open opens a session and reports it with its history.
func (m Model) open(id string) tea.Cmd {
	return func() tea.Msg {
		s, history, err := m.deps.Open(m.ctx, id)
		if err != nil {
			return state.Failed{Err: fmt.Errorf("failed to open session: %w", err)}
		}

		return openedMsg{sess: s, history: history}
	}
}

// switchTo closes the current session and opens another.
func (m Model) switchTo(id string) tea.Cmd {
	sess := m.sess
	next := m.open(id)

	return func() tea.Msg {
		if sess != nil {
			_ = sess.Close()
		}

		return next()
	}
}
