package render_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/tui/render"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
	"github.com/viktordanov/uagent-harness/internal/usershell"
)

func TestShellModePromptAndFooter(t *testing.T) {
	s := base()
	assert.Equal(t, "λ ", render.ShellPrompt(s))
	assert.NotContains(t, footer(s), "shell mode")

	s = apply(s, state.EnterShell{})

	assert.Equal(t, "! ", render.ShellPrompt(s), "the composer's mark in shell mode")
	assert.Contains(t, render.ShellPlaceholder(s), "esc to leave shell mode")
	assert.Contains(t, footer(s), "! shell mode · enter runs the command · esc leaves")
	s.Details = true
	assert.Contains(t, footer(s), "! shell mode · enter runs the command · esc leaves", "the detailed view's footer too")

	s = apply(s, state.LeaveShell{})
	assert.Equal(t, "λ ", render.ShellPrompt(s))
}

// TestShellCommandItems draws commands the user ran: one that failed with
// long output folded, waiting for the agent; one the agent has seen; one
// a rule refused; and one still running.
func TestShellCommandItems(t *testing.T) {
	var long strings.Builder
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&long, "line %d\n", i)
	}
	failed := usershell.Result{Record: usershell.Record{Command: "go test ./...", ExitCode: 1, Duration: 2300 * time.Millisecond, Output: long.String()}}
	seen := usershell.Result{Record: usershell.Record{Command: "git status --short", Duration: 40 * time.Millisecond}}
	refused := usershell.Result{Record: usershell.Record{Command: "rm -rf build", ExitCode: usershell.ExitNotRun}, Refused: "not run: a rule forbids this command."}
	s := apply(base(),
		session.ShellStarted{At: t0, ID: "a", Command: "go test ./..."},
		session.ShellFinished{At: t0, ID: "a", Result: failed},
		session.ShellStarted{At: t0, ID: "b", Command: "git status --short"},
		session.ShellFinished{At: t0, ID: "b", Result: seen},
		core.UserMessage{At: t0, ID: "b", Text: seen.Text()},
		session.ShellStarted{At: t0, ID: "c", Command: "rm -rf build"},
		session.ShellFinished{At: t0, ID: "c", Result: refused},
		session.ShellStarted{At: t0, ID: "d", Command: "sleep 5"},
		session.ShellOutput{At: t0, ID: "d", Text: "waiting\n"},
		state.Tick{Now: t0.Add(3 * time.Second)},
	)
	s.Now = t0.Add(3 * time.Second)

	out := screenTall(s)
	golden(t, "shell", out)
	assert.Contains(t, out, "! go test ./...  ✗ exit 1 · 2.3s")
	assert.Contains(t, out, "… 20 more lines")
	assert.Contains(t, out, "! git status --short  ✓ · 0.0s\n\n  (no output)\n\n\n! rm", "no waiting note once the agent has it")
	assert.Contains(t, out, "not run: a rule forbids this command.")

	s.Details = true
	assert.NotContains(t, screenTall(s), "more lines", "the detailed view shows up to 50 rows")
}

// screenTall draws a screen tall enough for the whole transcript.
func screenTall(s state.State) string {
	out, _ := render.Screen(s, render.NewCache(render.Amber), render.Frame{Width: 80, Height: 90, Composer: "λ ", ComposerHeight: 1})
	lines := strings.Split(ansi.Strip(out), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}

	return strings.Join(lines, "\n") + "\n"
}
