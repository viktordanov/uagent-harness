package bubble

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"mvdan.cc/sh/v3/shell"

	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

var errNoEditor = errors.New("the editor command is empty")

// editDraft runs the editor on the draft with the terminal released, as
// ctrl+g in Claude Code and Codex does: Bubble Tea leaves the alt screen,
// the mouse, and bracketed paste while it runs, and restores them after.
func (m Model) editDraft(text string) tea.Cmd {
	args, err := editorCommand(os.Getenv)
	if err != nil {
		return func() tea.Msg { return state.DraftEdited{Err: err} }
	}
	run := &editorRun{ctx: m.ctx, args: args, text: text}
	start := m.deps.Exec
	if start == nil {
		start = tea.Exec
	}

	return start(run, func(err error) tea.Msg {
		if err != nil {
			return state.DraftEdited{Err: err}
		}

		return state.DraftEdited{Text: run.saved}
	})
}

// editorCommand is $VISUAL, else $EDITOR, split into words as a shell
// would (`code --wait`, `nvim -f`), else vim, or vi without vim.
func editorCommand(getenv func(string) string) ([]string, error) {
	raw := strings.TrimSpace(getenv("VISUAL"))
	if raw == "" {
		raw = strings.TrimSpace(getenv("EDITOR"))
	}
	if raw == "" {
		if _, err := exec.LookPath("vim"); err == nil {
			return []string{"vim"}, nil
		}

		return []string{"vi"}, nil
	}
	args, err := shell.Fields(raw, getenv)
	if err != nil {
		return nil, fmt.Errorf("cannot read the editor command %q: %w", raw, err)
	}
	if len(args) == 0 || args[0] == "" {
		return nil, errNoEditor
	}

	return args, nil
}

// editorRun is one edit, a tea.ExecCommand: it writes the draft to a
// temporary .md file (0600, in the system's temporary directory, not the
// workspace), runs the editor on it, reads it back into saved, and removes
// it.
type editorRun struct {
	ctx            context.Context //nolint:containedctx // a tea.ExecCommand's Run takes none
	args           []string
	text           string
	saved          string
	stdin          io.Reader
	stdout, stderr io.Writer
}

func (r *editorRun) SetStdin(in io.Reader)   { r.stdin = in }
func (r *editorRun) SetStdout(out io.Writer) { r.stdout = out }
func (r *editorRun) SetStderr(out io.Writer) { r.stderr = out }

func (r *editorRun) Run() error {
	f, err := os.CreateTemp("", "uah-prompt-*.md") // CreateTemp makes it 0600
	if err != nil {
		return fmt.Errorf("failed to create the file to edit: %w", err)
	}
	path := f.Name()
	defer func() { _ = os.Remove(path) }()
	_, err = f.WriteString(r.text)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("failed to write the file to edit: %w", err)
	}
	//nolint:gosec // the user's own editor, from $VISUAL or $EDITOR
	cmd := exec.CommandContext(r.ctx, r.args[0], append(r.args[1:], path)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = r.stdin, r.stdout, r.stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(r.args[0]), err)
	}
	saved, err := os.ReadFile(path) //nolint:gosec // the file this run made
	if err != nil {
		return fmt.Errorf("failed to read the edited file: %w", err)
	}
	r.saved = string(saved)

	return nil
}
