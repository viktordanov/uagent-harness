// Package usershell runs a command the user types in the TUI's shell mode
// (`!` in the composer), as Codex's user shell commands and Claude Code's
// bash mode do, and writes the record the agent sees of it. By default the
// command runs as the user's own, outside the sandbox and the command
// rules, as in Codex; Sandboxed runs it like the agent's commands instead.
package usershell

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"

	"github.com/unreallabsai/unreal-agent/harness/operation"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/sandbox"
)

const (
	// DefaultTimeout stops a command after an hour, as Codex's
	// USER_SHELL_TIMEOUT_MS.
	DefaultTimeout = time.Hour
	// OutputLimit is how many characters of output the agent sees: the
	// runner's Bash default, about Codex's 10,000-token cut.
	OutputLimit = operation.DefaultMaxOutputLength
	// ExitNotRun is the exit code of a command that did not run or did not
	// finish, as Codex records -1.
	ExitNotRun = -1
)

var errTimedOut = errors.New("the command timed out")

// Runner runs the user's commands. The zero value is not usable: Shell and
// Policy.Workspace must be set.
type Runner struct {
	// Dir holds the sandbox scripts (sandbox.Shell).
	Dir string
	// Policy is the sandbox's roots and network; its mode comes from each
	// request's permission mode.
	Policy sandbox.Policy
	Env    sandbox.EnvPolicy
	// Shell is the user's shell; commands run as `<shell> -c <command>`,
	// as the agent's Bash commands do.
	Shell string
	// Sandboxed runs commands like the agent's: refused by forbidden
	// rules, and in the sandbox of the request's permission mode unless an
	// allow rule says otherwise. Off, they run outside the sandbox and the
	// rules, as Codex and Claude Code run the user's own commands.
	Sandboxed bool
	// Approver holds the command rules for Sandboxed (nil: no rules).
	Approver *approval.Approver
	// Timeout stops a command (0: DefaultTimeout).
	Timeout time.Duration
}

// Request is one command the user typed.
type Request struct {
	Command string
	// Mode is the session's permission mode, which picks the sandbox.
	Mode approval.Mode
	// Stream, when set, gets the output as it arrives.
	Stream func(chunk string)
}

// Result is how a command went. Record is what the agent sees.
type Result struct {
	Record

	// Sandbox is the sandbox the command ran in; FullAccess means none.
	Sandbox sandbox.Mode
	// Refused says why the command did not run ("" when it ran).
	Refused string
	// TimedOut and Canceled mean the command was stopped.
	TimedOut, Canceled bool
}

// Run runs the command in the workspace and returns its record, also when
// it fails, is refused, or is stopped: the agent hears of it either way.
func (r *Runner) Run(ctx context.Context, req Request) Result {
	var res Result
	res.Command, res.ExitCode = req.Command, ExitNotRun
	shell, mode, refused := r.shellFor(req) //nolint:contextcheck // on Linux, the sandbox probes bwrap once per process, with its own timeout
	res.Sandbox = mode
	if refused != "" {
		res.Refused, res.Output = refused, refused

		return res
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeoutCause(ctx, timeout, errTimedOut)
	defer cancel()
	out := &capture{keep: 4 * OutputLimit, stream: req.Stream}
	cmd := exec.CommandContext(ctx, shell, "-c", req.Command)
	cmd.Dir = r.Policy.Workspace
	cmd.Stdout, cmd.Stderr = out, out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	started := time.Now()
	err := cmd.Run()
	res.Duration = time.Since(started)
	res.Output = out.bounded(OutputLimit)
	var exitErr *exec.ExitError
	switch {
	case errors.Is(context.Cause(ctx), errTimedOut):
		res.TimedOut = true
		res.Output = fmt.Sprintf("command timed out after %d milliseconds\n%s", timeout.Milliseconds(), res.Output)
	case ctx.Err() != nil:
		res.Canceled = true
		res.Output = "command aborted by user\n" + res.Output
	case err == nil || errors.As(err, &exitErr):
		res.ExitCode = cmd.ProcessState.ExitCode()
	default:
		res.Output = fmt.Sprintf("execution error: %v", err)
	}

	return res
}

// shellFor picks the shell that runs the request: the sandboxing script of
// its permission mode, or the plain shell (with the environment policy).
// refused is why the command must not run.
func (r *Runner) shellFor(req Request) (shell string, mode sandbox.Mode, refused string) {
	mode = sandbox.FullAccess
	if r.Sandboxed {
		d := approval.Decision{Run: approval.Sandboxed}
		if r.Approver != nil {
			d = r.Approver.DecideTyped(req.Command)
		}
		switch d.Run {
		case approval.Deny:
			return "", mode, d.Reason
		case approval.Sandboxed:
			mode = req.Mode.Sandbox()
		case approval.Unsandboxed:
		}
	}
	p := r.Policy
	p.Mode = mode
	shell, err := sandbox.Shell(r.Dir, p, r.Env, r.Shell)
	if errors.Is(err, sandbox.ErrUnavailable) {
		// No sandbox on this system: the command runs without one, as the
		// process engine runs the agent's.
		p.Mode, mode = sandbox.FullAccess, sandbox.FullAccess
		shell, err = sandbox.Shell(r.Dir, p, r.Env, r.Shell)
	}
	if err != nil {
		return "", mode, "not run: " + err.Error()
	}

	return shell, mode, ""
}
