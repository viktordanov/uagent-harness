// Command uah is a general-purpose harness for unreal-agent-runner: long-lived
// sessions, a terminal UI, and headless runs built on uagent.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/urfave/cli/v3"
)

const (
	exitOK     = 0
	exitFailed = 1
	exitUsage  = 2
)

// version is set with -ldflags "-X main.version=..."; go install builds use the module version.
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := newApp().Run(ctx, os.Args)
	stop()
	os.Exit(exitCode(err))
}

func newApp() *cli.Command {
	return &cli.Command{
		Name:    "uah",
		Usage:   "a general-purpose harness for unreal-agent-runner",
		Version: buildVersion(),
		// Errors are printed once, by main, with the right exit code.
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
		OnUsageError:   onUsageError,
		ArgsUsage:      "[prompt]",
		Description: "Without a command, uah opens the terminal UI: a live session you can steer.\n" +
			"enter sends (queueing while the agent works), ctrl+enter sends now, esc esc interrupts,\n" +
			"/help lists commands. Resume with `uah resume`, --session <id or prefix>, or ctrl+s inside.",
		Flags:    sessionFlags(),
		Action:   tuiAction,
		Commands: []*cli.Command{runCommand(), resumeCommand(), sessionsCommand(), hooksCommand(), doctorCommand()},
	}
}

// exitCode maps the app error to a process exit code and prints it once.
func exitCode(err error) int {
	if err == nil {
		return exitOK
	}
	code := exitFailed
	if exitErr, ok := errors.AsType[cli.ExitCoder](err); ok {
		code = exitErr.ExitCode()
	}
	if msg := err.Error(); msg != "" {
		fmt.Fprintf(os.Stderr, "uah: %s\n", msg)
	}

	return code
}

func onUsageError(_ context.Context, _ *cli.Command, err error, _ bool) error {
	return cli.Exit(fmt.Sprintf("%v (see --help)", err), exitUsage)
}

func buildVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}

	return version
}
