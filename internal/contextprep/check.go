package contextprep

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// CheckTimeout is how long a module's check may run.
const CheckTimeout = 2 * time.Second

// ErrNoSandbox is why a check does not run where no sandbox can hold it.
var ErrNoSandbox = errors.New("no sandbox to run checks in")

// Checker runs a module's check, argv in dir, and returns nil when it
// exited 0.
type Checker func(ctx context.Context, argv []string, dir string) error

// ExecChecker runs a check as a process, never through a shell: argv[0]
// is found on PATH (or is absolute), the words are passed as they are, and
// wrap puts the command in the sandbox (read-only, no network). It has no
// stdin, its output is discarded, its environment is env, and it is killed
// after CheckTimeout. A nil wrap runs no checks: a check never runs
// unsandboxed.
func ExecChecker(wrap func(argv []string) ([]string, error), env []string) Checker {
	return func(ctx context.Context, argv []string, dir string) error {
		if wrap == nil {
			return ErrNoSandbox
		}
		bin, err := exec.LookPath(argv[0])
		if err != nil {
			return fmt.Errorf("failed to find %s: %w", argv[0], err)
		}
		full, err := wrap(append([]string{bin}, argv[1:]...))
		if err != nil {
			return fmt.Errorf("%w: %w", ErrNoSandbox, err)
		}
		ctx, cancel := context.WithTimeout(ctx, CheckTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, full[0], full[1:]...) //nolint:gosec // argv from a trusted module's front matter, run without a shell in the sandbox
		cmd.Dir, cmd.Env = dir, env
		cmd.WaitDelay = 500 * time.Millisecond
		if err := cmd.Run(); err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("timed out after %s", CheckTimeout)
			}

			return err // the exit status says what failed
		}

		return nil
	}
}
