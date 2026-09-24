package state

import (
	"fmt"
	"slices"
	"strings"

	"github.com/viktordanov/uagent-harness/internal/session"
)

// Command is one slash command.
type Command struct {
	Name    string
	Aliases []string
	Args    string
	Help    string
	// WhileBusy allows the command while a run is live.
	WhileBusy bool
	run       func(s *State, args string) []Effect
}

// Commands lists every slash command in the order /help shows them.
func Commands() []Command {
	return []Command{
		{Name: "model", Args: "<id>", Help: "use another model", WhileBusy: true, run: cmdModel},
		{Name: "effort", Args: "<level>", Help: "set the thinking level: " + strings.Join(session.Efforts, ", "), WhileBusy: true, run: cmdEffort},
		{Name: "fast", Help: "priority processing (needs the embedded engine)", WhileBusy: true, run: cmdFast},
		{Name: "resume", Args: "[id]", Help: "open the session picker, or resume a session by ID prefix", run: cmdResume},
		{Name: "new", Aliases: []string{"clear"}, Help: "start a new session", run: func(*State, string) []Effect { return []Effect{EffOpenSession{}} }},
		{Name: "stop", Help: "interrupt the live run; queued messages stay", WhileBusy: true, run: func(*State, string) []Effect { return []Effect{EffInterrupt{}} }},
		{Name: "compact", Help: "summarize the context to free it; your messages stay as written (embedded engine)", WhileBusy: true, run: cmdCompact},
		{Name: "context", Help: "what fills the context window: prompt, instructions, skills, tools, messages", WhileBusy: true, run: cmdContext},
		{Name: "status", Help: "session, settings, and totals", WhileBusy: true, run: cmdStatus},
		{Name: "mcp", Help: "MCP servers, their state, and their tools", WhileBusy: true, run: cmdMCP},
		{Name: "agents", Help: "subagents the agent started, and their state", WhileBusy: true, run: cmdAgents},
		{Name: "sandbox", Help: "what commands may do: the sandbox mode (set it with --sandbox or sandbox_mode)", WhileBusy: true, run: cmdSandbox},
		{Name: "reasoning", Help: "show or hide reasoning summaries", WhileBusy: true, run: func(s *State, _ string) []Effect { s.ShowReasoning = !s.ShowReasoning; return nil }},
		{Name: "details", Help: "show or hide turns, run dividers, and token totals", WhileBusy: true, run: func(s *State, _ string) []Effect { s.Details = !s.Details; return nil }},
		{Name: "help", Help: "commands and keys", WhileBusy: true, run: cmdHelp},
		{Name: "quit", Aliases: []string{"exit"}, Help: "close the session and exit", WhileBusy: true, run: func(s *State, _ string) []Effect {
			s.Quitting, s.Status = true, "stopping…"
			return []Effect{EffQuit{}}
		}},
	}
}

// FindCommand returns the command named name or aliased to it.
func FindCommand(name string) (Command, bool) {
	for _, c := range Commands() {
		if c.Name == name || slices.Contains(c.Aliases, name) {
			return c, true
		}
	}

	return Command{}, false
}

// Complete returns the commands whose name starts with prefix (without "/").
func Complete(prefix string) []Command {
	var out []Command
	for _, c := range Commands() {
		if strings.HasPrefix(c.Name, prefix) {
			out = append(out, c)
		}
	}

	return out
}

func (s *State) command(text string) (State, []Effect) {
	name, args, _ := strings.Cut(strings.TrimPrefix(text, "/"), " ")
	args = strings.TrimSpace(args)
	cmd, ok := FindCommand(name)
	if !ok {
		s.notice(session.LevelError, fmt.Sprintf("unknown command /%s (see /help)", name))

		return *s, nil
	}
	if s.Busy && !cmd.WhileBusy {
		s.notice(session.LevelWarning, fmt.Sprintf("/%s waits until the agent is idle; press esc twice to interrupt it", cmd.Name))

		return *s, nil
	}

	return *s, cmd.run(s, args)
}

func cmdModel(s *State, args string) []Effect {
	if args == "" {
		s.notice(session.LevelInfo, fmt.Sprintf("model: %s/%s (change it with /model <id>)", s.Settings.Provider, s.Settings.Model))

		return nil
	}
	next := s.Settings
	next.Model = args
	if err := next.Validate(); err != nil {
		s.notice(session.LevelError, err.Error())

		return nil
	}

	return []Effect{EffSetSettings{Settings: next}}
}

func cmdEffort(s *State, args string) []Effect {
	if !slices.Contains(session.Efforts, args) {
		s.notice(session.LevelInfo, fmt.Sprintf("effort: %s (set it with /effort %s)", s.Settings.Effort, strings.Join(session.Efforts, "|")))

		return nil
	}
	next := s.Settings
	next.Effort = args

	return []Effect{EffSetSettings{Settings: next}}
}

func cmdFast(s *State, _ string) []Effect {
	if !s.Caps.ServiceTier {
		s.notice(session.LevelWarning, "/fast needs the embedded engine and the openai or openai-codex provider")

		return nil
	}
	next := s.Settings
	if next.ServiceTier == "" {
		next.ServiceTier = "priority"
	} else {
		next.ServiceTier = ""
	}

	return []Effect{EffSetSettings{Settings: next}}
}

func cmdResume(s *State, args string) []Effect {
	if args == "" {
		return []Effect{EffLoadSessions{}}
	}
	for _, info := range s.Picker.Sessions { // IDs are global, as in Codex
		if strings.HasPrefix(info.ID, args) {
			return []Effect{EffOpenSession{ID: info.ID}}
		}
	}

	return []Effect{EffOpenSession{ID: args}}
}

func cmdStatus(s *State, _ string) []Effect {
	t := s.Totals
	files := "none"
	if len(s.Files) > 0 {
		files = strings.Join(s.Files, ", ")
	}
	s.notice(session.LevelInfo, fmt.Sprintf("session %s · %s engine · %s/%s · effort %s · %s", s.SessionID, s.Engine, s.Settings.Provider, s.Settings.Model, s.Settings.Effort, s.Settings.Workspace))
	s.notice(session.LevelInfo, fmt.Sprintf("%d runs · %d turns · %d tool calls (max %d parallel) · %d in / %d out tokens · tools overlapped the model %s", t.Runs, t.Turns, t.ToolCalls, t.MaxParallel, t.Tokens.InputTokens, t.Tokens.OutputTokens, t.Overlap.Round(100_000_000)))
	s.notice(session.LevelInfo, "instructions: "+files)

	return []Effect{EffLoadActivity{}}
}

func cmdHelp(s *State, _ string) []Effect {
	var b strings.Builder
	for _, c := range Commands() {
		name := "/" + c.Name
		if c.Args != "" {
			name += " " + c.Args
		}
		fmt.Fprintf(&b, "%-18s %s\n", name, c.Help)
	}
	b.WriteString("\nenter send (queues while the agent works) · ctrl+enter or alt+enter send now · shift+enter or ctrl+j new line\n")
	b.WriteString("esc esc interrupt · ↑ edit the last queued message · alt+, alt+. effort · ctrl+s sessions · ctrl+n new · ctrl+t details · ctrl+r reasoning · wheel, shift+↑↓, pgup/pgdn scroll (end: bottom) · ctrl+c ctrl+c quit")
	s.notice(session.LevelInfo, b.String())

	return nil
}

func cmdSandbox(s *State, _ string) []Effect {
	var text string
	switch s.Settings.Sandbox {
	case "read-only":
		text = "sandbox read-only: commands can read files but write nothing, without network"
	case "workspace-write":
		text = "sandbox workspace-write: commands can read any file, write the workspace and temporary directories " +
			"(.git, .uagent, .agents, and .codex stay read-only), without network"
	case "danger-full-access", "":
		text = "no sandbox: commands can do anything your user can"
	default:
		text = "sandbox " + s.Settings.Sandbox
	}
	s.notice(session.LevelInfo, text+". Change it with --sandbox or sandbox_mode in the configuration.")

	return nil
}
