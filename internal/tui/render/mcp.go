package render

import (
	"fmt"

	"github.com/charmbracelet/x/ansi"

	"github.com/viktordanov/uagent-harness/internal/mcp"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// mcpLines draws the /mcp panel as Codex's "MCP Tools" cell: a line per
// server with its state, transport, and tool count, then what to do about
// a failure. Verbose (/mcp verbose or the detailed view) adds the command
// or URL, an HTTP server's auth state, and each tool with its approval
// mode and description.
func mcpLines(it state.Item, w int, details bool) []string {
	verbose := details || it.Final
	out := []string{"", bold.Render("• MCP servers")}
	for _, s := range it.MCP {
		out = append(out, ansi.Truncate(serverLine(s), w, "…"))
		if hint := serverHint(s); hint != "" {
			out = append(out, styleLines(wrapPrefixed(hint, w, "      ", "      "), stateStyle(s.State))...)
		}
		if !verbose {
			continue
		}
		if s.Transport == mcp.TransportHTTP {
			out = append(out, dimLine(w, "      url: "+s.Target), dimLine(w, "      auth: "+s.Auth.Text()))
		} else {
			out = append(out, dimLine(w, "      command: "+s.Target))
		}
		for _, t := range s.Tools {
			out = append(out, toolEntry(t, w))
		}
	}
	if !verbose {
		out = append(out, dim.Render("  /mcp verbose (or ctrl+t) lists each tool and its approval mode"))
	}

	return out
}

// serverLine is "  • docs: ready · stdio · 3 tools".
func serverLine(s mcp.ServerStatus) string {
	line := fmt.Sprintf("  • %s: %s · %s", tool.Render(s.Name), stateStyle(s.State).Render(stateText(s.State)), s.Transport)
	if s.State == mcp.StateReady {
		n := len(s.Tools)
		plural := "s"
		if n == 1 {
			plural = ""
		}
		line += fmt.Sprintf(" · %d tool%s", n, plural)
	}

	return line
}

// serverHint says what went wrong and how to fix it.
func serverHint(s mcp.ServerStatus) string {
	switch s.State {
	case mcp.StateNeedsLogin:
		return "run `uah mcp login " + s.Name + "`, then /new or restart uah"
	case mcp.StateFailed:
		return s.Error
	case mcp.StateStarting, mcp.StateReady, mcp.StateDisabled:
	}

	return ""
}

func stateText(st mcp.State) string {
	if st == mcp.StateNeedsLogin {
		return "needs login"
	}

	return string(st)
}

func stateStyle(st mcp.State) interface{ Render(...string) string } {
	switch st {
	case mcp.StateReady:
		return ok
	case mcp.StateFailed:
		return bad
	case mcp.StateNeedsLogin, mcp.StateStarting:
		return warn
	case mcp.StateDisabled:
	}

	return dim
}

// toolEntry is "      mcp__docs__search · approve · Search the docs.".
func toolEntry(t mcp.Tool, w int) string {
	mode := string(t.Approval)
	if t.NeedsApproval() {
		mode += ", asks"
	}
	line := "      " + t.Name + dim.Render(" · "+mode)
	if t.Description != "" {
		line += dim.Render(" · " + oneLine(t.Description))
	}

	return ansi.Truncate(line, w, "…")
}

func dimLine(w int, s string) string { return dim.Render(ansi.Truncate(s, w, "…")) }
