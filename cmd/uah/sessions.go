package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/session"
)

// sessionsCommand is `uah sessions`: list sessions, or show one.
func sessionsCommand() *cli.Command {
	stateDir := &cli.StringFlag{
		Name: "state-dir", Usage: "sessions, logs, and run records",
		Value: defaultStateDir(), Sources: cli.EnvVars("UAGENT_STATE_DIR"), TakesFile: true,
	}
	jsonFlag := &cli.BoolFlag{Name: "json", Usage: "print JSON"}
	all := &cli.BoolFlag{Name: flagAll, Usage: "list sessions from every directory"}
	workspace := &cli.StringFlag{Name: flagWorkspace, Aliases: []string{"C"}, Usage: "list this directory's sessions", DefaultText: "the current directory", TakesFile: true}

	return &cli.Command{
		Name:         "sessions",
		Usage:        "list this directory's sessions, most recent first (--all for every directory)",
		Flags:        []cli.Flag{stateDir, jsonFlag, all, workspace},
		OnUsageError: onUsageError,
		Action:       listSessions,
		Commands: []*cli.Command{{
			Name:         "show",
			Usage:        "print a session's transcript",
			ArgsUsage:    "<id or unique prefix>",
			Flags:        []cli.Flag{stateDir, jsonFlag},
			OnUsageError: onUsageError,
			Action:       showSession,
		}},
	}
}

func listSessions(_ context.Context, cmd *cli.Command) error {
	stateDir, err := filepath.Abs(cmd.String("state-dir"))
	if err != nil {
		return fmt.Errorf("failed to resolve state dir: %w", err)
	}
	infos, err := session.Sessions(stateDir)
	if err != nil {
		return err
	}
	cwd, err := currentDir(cmd)
	if err != nil {
		return err
	}
	showAll := cmd.Bool(flagAll)
	if !showAll {
		infos = session.InDir(infos, cwd)
	}
	if cmd.Bool("json") {
		return writeJSON(os.Stdout, infos)
	}
	if len(infos) == 0 {
		if showAll {
			fmt.Fprintln(os.Stderr, "no sessions in "+stateDir)
		} else {
			fmt.Fprintln(os.Stderr, "no sessions in "+cwd+" (--all lists every directory)")
		}

		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if showAll {
		fmt.Fprintln(tw, "SESSION\tACTIVE\tRUNS\tSTATUS\tMODEL\tFROM\tDIRECTORY\tFIRST PROMPT")
	} else {
		fmt.Fprintln(tw, "SESSION\tACTIVE\tRUNS\tSTATUS\tMODEL\tFROM\tFIRST PROMPT")
	}
	for _, in := range infos {
		dir := ""
		if showAll {
			dir = homeShort(in.Workspace) + "\t"
		}
		from := in.Source
		if from == "" {
			from = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\t%s%s\n", short(in.ID), ago(in.LastActivity), in.Runs, in.Status, modelLabel(in.Model), from, dir, oneLine(in.FirstPrompt, 60))
	}

	return tw.Flush()
}

func showSession(_ context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() != 1 {
		return cli.Exit("usage: uah sessions show <id or unique prefix>", exitUsage)
	}
	stateDir, err := filepath.Abs(cmd.String("state-dir"))
	if err != nil {
		return fmt.Errorf("failed to resolve state dir: %w", err)
	}
	info, err := resolveSession(stateDir, cmd.Args().First())
	if err != nil {
		return err
	}
	runs, err := session.Load(stateDir, info.ID)
	if err != nil {
		return err
	}
	if cmd.Bool("json") {
		return writeJSON(os.Stdout, struct {
			Session session.Info `json:"session"`
			Runs    []runView    `json:"runs"`
		}{info, runViews(runs)})
	}
	printTranscript(os.Stdout, info, runs)

	return nil
}

// runView is the JSON shape of one run in `uah sessions show --json`.
type runView struct {
	RunID    string      `json:"run_id"`
	Status   core.Status `json:"status"`
	Started  time.Time   `json:"started"`
	Messages []string    `json:"messages"`
	Answer   string      `json:"answer"`
}

func runViews(runs []session.LoadedRun) []runView {
	views := make([]runView, 0, len(runs))
	for _, r := range runs {
		v := runView{RunID: r.Record.Result.Request.RunID, Status: r.Record.Result.Status, Started: r.Record.Result.StartedAt, Messages: []string{}}
		for _, e := range r.Events {
			switch m := e.(type) {
			case core.UserMessage:
				v.Messages = append(v.Messages, m.Text)
			case core.AssistantMessage:
				if m.Final {
					v.Answer = m.Text
				}
			}
		}
		views = append(views, v)
	}

	return views
}

func printTranscript(w io.Writer, info session.Info, runs []session.LoadedRun) {
	fmt.Fprintf(w, "session %s · %s/%s · %s\n", info.ID, info.Provider, modelLabel(info.Model), info.Workspace)
	for _, r := range runs {
		res := r.Record.Result
		fmt.Fprintf(w, "\n── run %s · %s · %s\n", res.Request.RunID, res.Status, res.StartedAt.Local().Format("2006-01-02 15:04"))
		for _, e := range r.Events {
			switch m := e.(type) {
			case core.UserMessage:
				fmt.Fprintf(w, "› %s\n", m.Text)
			case core.ToolCalled:
				fmt.Fprintf(w, "  → %s  %s\n", m.Name, m.Label)
			case core.AssistantMessage:
				if m.Final {
					fmt.Fprintf(w, "✓ %s\n", m.Text)
				} else if m.Text != "" {
					fmt.Fprintf(w, "· %s\n", oneLine(m.Text, 200))
				}
			case core.RunnerError:
				fmt.Fprintf(w, "error: %s\n", m.Message)
			}
		}
	}
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("failed to write JSON: %w", err)
	}

	return nil
}

// homeShort writes paths under the home directory with ~.
func homeShort(path string) string {
	if h, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, h) {
		return "~" + strings.TrimPrefix(path, h)
	}

	return path
}

// ago formats a time as a short relative age.
func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case t.IsZero():
		return "-"
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}

	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}
