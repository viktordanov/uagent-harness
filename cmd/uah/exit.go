package main

import (
	"fmt"
	"io"
	"os"
	"strconv"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/viktordanov/uagent-harness/internal/tui/bubble"
)

// printExit is what uah prints when the TUI quits, as Codex does
// (codex-rs/tui/src/app/exit_summary.rs): the session's token usage and the
// command that continues it, after how many queued messages it kept. It uses the TUI's theme: labels dim, numbers
// in the text color, and the command in the accent, on its own line so it
// copies whole. A writer that is not a terminal, or NO_COLOR, gets plain
// text.
func printExit(w io.Writer, e bubble.Exit) {
	if !e.Resumable {
		return
	}
	out := colorprofile.NewWriter(w, os.Environ())
	dim := lipgloss.NewStyle().Foreground(e.Theme.Dim)
	accent := lipgloss.NewStyle().Foreground(e.Theme.Accent).Bold(true)
	t := e.Tokens
	if t.InputTokens+t.OutputTokens > 0 {
		// Codex's FinalOutput: the total counts uncached input and output.
		input := max(t.InputTokens-t.CachedInputTokens, 0)
		line := dim.Render("Token usage:") +
			" " + dim.Render("total=") + thousands(input+t.OutputTokens) +
			" " + dim.Render("input=") + thousands(input)
		if t.CachedInputTokens > 0 {
			line += dim.Render(" (+ " + thousands(t.CachedInputTokens) + " cached)")
		}
		line += " " + dim.Render("output=") + thousands(t.OutputTokens)
		if t.ReasoningTokens > 0 {
			line += dim.Render(" (reasoning " + thousands(t.ReasoningTokens) + ")")
		}
		fmt.Fprintln(out, line)
	}
	if e.Queued > 0 {
		fmt.Fprintln(out, dim.Render("Queued messages kept in the session:")+" "+strconv.Itoa(e.Queued))
	}
	fmt.Fprintln(out, dim.Render("To continue this session, run:"))
	fmt.Fprintln(out, accent.Render("uah resume "+e.SessionID))
}

// thousands writes n with comma separators, as Codex's usage line does.
func thousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0 && s[i-1] != '-'; i -= 3 {
		s = s[:i] + "," + s[i:]
	}

	return s
}
