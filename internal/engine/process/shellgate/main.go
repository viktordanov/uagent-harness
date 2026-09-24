package shellgate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"syscall"
)

// Exit codes of the gate. A refused command exits 1, as a failed command
// does, with the reason on standard error for the model.
const (
	exitRefused = 1
	exitBroken  = 127
)

// Main runs the gate: args are the config and the shell's arguments. It
// execs the chosen shell, so it returns only when the command does not run.
func Main(args []string, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "uah: the shell gate needs its configuration")

		return exitBroken
	}
	var c Config
	if err := json.Unmarshal([]byte(args[0]), &c); err != nil {
		fmt.Fprintf(stderr, "uah: the shell gate's configuration is invalid: %v\n", err)

		return exitBroken
	}
	cwd, _ := os.Getwd()
	argv, refused := c.Decide(context.Background(), args[1:], cwd)
	if argv == nil {
		fmt.Fprintln(stderr, refused)

		return exitRefused
	}
	err := syscall.Exec(argv[0], argv, os.Environ()) //nolint:gosec // the shell uah wrote, with the runner's arguments
	fmt.Fprintf(stderr, "uah: failed to run %s: %v\n", argv[0], err)

	return exitBroken
}
