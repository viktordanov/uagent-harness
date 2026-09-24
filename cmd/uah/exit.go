package main

import (
	"fmt"
	"io"
	"strconv"

	"github.com/viktordanov/uagent-harness/internal/tui/bubble"
)

// printExit is what uah prints when the TUI quits, as Codex does
// (codex-rs/tui/src/app/exit_summary.rs): the session's token usage and the
// command that continues it.
func printExit(w io.Writer, e bubble.Exit) {
	if !e.Resumable {
		return
	}
	t := e.Tokens
	if t.InputTokens+t.OutputTokens > 0 {
		// Codex's FinalOutput: the total counts uncached input and output.
		input := max(t.InputTokens-t.CachedInputTokens, 0)
		cached := ""
		if t.CachedInputTokens > 0 {
			cached = " (+ " + thousands(t.CachedInputTokens) + " cached)"
		}
		reasoning := ""
		if t.ReasoningTokens > 0 {
			reasoning = " (reasoning " + thousands(t.ReasoningTokens) + ")"
		}
		fmt.Fprintf(w, "Token usage: total=%s input=%s%s output=%s%s\n",
			thousands(input+t.OutputTokens), thousands(input), cached, thousands(t.OutputTokens), reasoning)
	}
	fmt.Fprintf(w, "To continue this session, run:\n  uah resume %s\n", e.SessionID)
}

// thousands writes n with comma separators, as Codex's usage line does.
func thousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0 && s[i-1] != '-'; i -= 3 {
		s = s[:i] + "," + s[i:]
	}

	return s
}
