package clipboard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// ErrNoTextTool means no tool to write the clipboard can be run: pbcopy on
// macOS, wl-copy (Wayland) or xclip (X11) on Linux.
var ErrNoTextTool = errors.New("no clipboard tool: pbcopy, wl-copy, or xclip")

// writeTimeout bounds a clipboard tool that does not return.
const writeTimeout = 5 * time.Second

// Input runs a command with stdin as its standard input.
type Input func(ctx context.Context, stdin, name string, args ...string) error

// TextWriter writes text to the clipboard with the system's own tool, the
// way the TUI copies a selection next to OSC 52.
type TextWriter struct {
	Run      Input
	LookPath func(string) (string, error)
	Getenv   func(string) string
	GOOS     string
}

// SystemWriter is the host's writer, with the real commands and environment.
func SystemWriter() TextWriter {
	return TextWriter{Run: runInput, LookPath: exec.LookPath, Getenv: os.Getenv, GOOS: runtime.GOOS}
}

func runInput(ctx context.Context, stdin, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(stdin)

	return cmd.Run() //nolint:wrapcheck // WriteText explains the failure
}

// WriteText puts text on the clipboard, or returns ErrNoTextTool.
func (w TextWriter) WriteText(ctx context.Context, text string) error {
	name, args, ok := w.tool()
	if !ok {
		return ErrNoTextTool
	}
	if err := w.Run(ctx, text, name, args...); err != nil {
		return fmt.Errorf("failed to copy with %s: %w", name, err)
	}

	return nil
}

// tool picks pbcopy on macOS, and wl-copy in a Wayland session or xclip in
// an X11 one on Linux, when installed.
func (w TextWriter) tool() (name string, args []string, ok bool) {
	has := func(name string) bool { _, err := w.LookPath(name); return err == nil }
	switch {
	case w.GOOS == "darwin" && has("pbcopy"):
		return "pbcopy", nil, true
	case w.GOOS != "linux":
		return "", nil, false
	case w.Getenv("WAYLAND_DISPLAY") != "" && has("wl-copy"):
		return "wl-copy", nil, true
	case w.Getenv("DISPLAY") != "" && has("xclip"):
		return xclip.name, xclipArgs(), true
	}

	return "", nil, false
}
