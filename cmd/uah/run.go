package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/session"
)

const (
	exitDiskLimit = 3
	exitTimeout   = 124
	exitInterrupt = 130
)

// runCommand is `uah run`: a headless session that prints progress and answers.
func runCommand() *cli.Command {
	flags := append(sessionFlags(),
		&cli.BoolFlag{Name: "stdin", Usage: "after the prompt, read more messages from stdin, one per line; they queue while the agent works"},
		&cli.BoolFlag{Name: "stream", Usage: "write JSONL events (runs and session) to stdout instead of answers"},
		&cli.BoolFlag{Name: "quiet", Aliases: []string{"q"}, Usage: "no progress on stderr"},
		&cli.BoolFlag{Name: "verbose", Usage: "also show reasoning summaries"},
		&cli.BoolFlag{Name: "last", Usage: "continue this directory's most recent session"},
		&cli.BoolFlag{Name: flagAll, Usage: "with --last: the most recent session in any directory"},
	)

	return &cli.Command{
		Name:      "run",
		Usage:     "run a headless session",
		ArgsUsage: "[prompt]",
		Description: "Sends the prompt, prints progress on stderr and each answer on stdout, and exits when\n" +
			"the session is idle. With --stdin, each further line of stdin is another message:\n" +
			"it starts a run when the agent is idle and queues while it works.\n" +
			"Resume a session with --session <id or prefix>, or --last for this directory's most recent.",
		Flags:        flags,
		OnUsageError: onUsageError,
		Action:       runAction,
	}
}

func runAction(ctx context.Context, cmd *cli.Command) error {
	prompt := strings.TrimSpace(strings.Join(cmd.Args().Slice(), " "))
	followStdin := cmd.Bool("stdin")
	if prompt == "" && !followStdin {
		return cli.Exit("no prompt: pass one as an argument, or use --stdin", exitUsage)
	}
	ref := cmd.String("session")
	if cmd.Bool("last") {
		info, err := latestSession(cmd)
		if err != nil {
			return err
		}
		ref = info.ID
	}
	st, err := setupFor(cmd, os.Stderr, ref)
	if err != nil {
		return err
	}
	// The session gets its own context so Ctrl+C can close it gracefully.
	s, err := session.Open(context.WithoutCancel(ctx), st.engine, st.options)
	if err != nil {
		return cli.Exit(err.Error(), exitUsage)
	}

	out := runOutput{stdout: os.Stdout}
	switch {
	case cmd.Bool("stream"):
		out.jsonl = newJSONLWriter(os.Stdout)
	case !cmd.Bool("quiet"):
		out.progress = newPrinter(os.Stderr, cmd.Bool("verbose"))
	}
	var lines <-chan string
	if followStdin {
		lines = readLines(os.Stdin)
	}
	if prompt != "" {
		if _, err := s.Submit(prompt); err != nil {
			return fmt.Errorf("failed to send the prompt: %w", err)
		}
	}

	return drive(ctx, s, lines, prompt != "", &out)
}

// drive feeds stdin lines into the session and prints its events until the
// session is idle with no more input, or the context ends.
func drive(ctx context.Context, s *session.Session, lines <-chan string, busy bool, out *runOutput) error {
	events := s.Events()
	closing := false
	closeSession := func() {
		if !closing {
			closing = true
			go func() { _ = s.Close() }()
		}
	}
	if !busy && lines == nil {
		closeSession()
	}
	done := ctx.Done()
	interrupted := false
	for {
		select {
		case <-done:
			done = nil // handle the signal once
			interrupted = true
			closeSession()
		case line, ok := <-lines:
			if !ok {
				lines = nil
				if !busy {
					closeSession()
				}

				continue
			}
			if strings.TrimSpace(line) == "" || closing {
				continue
			}
			if _, err := s.Submit(line); err == nil {
				busy = true
			}
		case e, ok := <-events:
			if !ok {
				return out.exit(interrupted)
			}
			out.handle(e)
			if _, idle := e.(session.Idle); idle {
				busy = false
				if lines == nil {
					closeSession()
				}
			}
		}
	}
}

// runOutput prints events and remembers what decides the exit code.
type runOutput struct {
	stdout     io.Writer
	progress   *printer
	jsonl      *jsonlWriter
	last       *core.Result
	startError bool
}

func (o *runOutput) handle(e core.Event) {
	if o.progress != nil {
		o.progress.print(e)
	}
	if o.jsonl != nil {
		o.jsonl.write(e)
	}
	switch v := e.(type) {
	case core.RunFinished:
		result := v.Result
		o.last = &result
		if o.jsonl == nil && result.Answer != "" {
			fmt.Fprintln(o.stdout, result.Answer)
		}
	case session.Notice:
		if v.Level == "error" && o.last == nil {
			o.startError = true
		}
	}
}

func (o *runOutput) exit(interrupted bool) error {
	if o.jsonl != nil && o.jsonl.err != nil {
		return o.jsonl.err
	}
	switch {
	case interrupted:
		return cli.Exit("", exitInterrupt)
	case o.last == nil && o.startError:
		return cli.Exit("", exitFailed)
	case o.last == nil:
		return nil
	}
	switch o.last.Status {
	case core.StatusOK:
		return nil
	case core.StatusTimeout:
		return cli.Exit("", exitTimeout)
	case core.StatusInterrupted:
		return cli.Exit("", exitInterrupt)
	case core.StatusDiskLimit:
		return cli.Exit("", exitDiskLimit)
	case core.StatusRunning, core.StatusFailed:
	}

	return cli.Exit("", exitFailed)
}

// readLines streams lines from r and closes the channel at EOF.
func readLines(r io.Reader) <-chan string {
	lines := make(chan string)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()

	return lines
}
