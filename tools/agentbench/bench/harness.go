package bench

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// harnessRun is how one harness is started.
type harnessRun struct {
	name string
	args []string
	env  []string
	dir  string
}

// Permission modes.
const (
	// ModeAuto asks a reviewer model about each command that needs
	// approval: uah's permission_mode "auto", Codex's --approve-for-me.
	ModeAuto = "auto"
	// ModeWorkspace refuses what needs approval: the workspace-write
	// sandbox with approvals never asked.
	ModeWorkspace = "workspace"
)

func invocation(cfg Config, env *runEnv, fx *Fixture, t Task, k Key, ws, art string) harnessRun {
	prompt := fx.Expand(t.Prompt)
	runEnv := append(slicesClone(env.base), fx.Env...)
	if fx.Home != "" {
		// A fake home keeps the login where it is.
		runEnv = append(runEnv, "CODEX_HOME="+env.codexHome)
	}
	switch k.Harness {
	case HarnessCodex:
		args := []string{
			"exec", "--json", "--ephemeral", "--ignore-user-config", "--skip-git-repo-check",
			"-m", k.Model, "-c", "model_reasoning_effort=" + k.Effort,
		}
		if cfg.Mode == ModeAuto {
			// It implies the workspace-write sandbox and refuses -s.
			args = append(args, "--approve-for-me")
		} else {
			args = append(args, "-s", "workspace-write", "-c", `approval_policy="never"`)
		}

		return harnessRun{name: cfg.Codex, args: append(args, "-C", ws, prompt), env: runEnv, dir: ws}
	default:
		args := []string{
			"exec", "--json", "--model", k.Model, "--effort", k.Effort,
			"--config", env.uahConfig, "--state-dir", filepath.Join(art, "uah-state"), "--workspace", ws,
		}
		if cfg.Mode != ModeAuto {
			args = append(args, "--sandbox", "workspace-write", "--ask", "never")
		}

		return harnessRun{name: cfg.UAH, args: append(args, prompt), env: append(runEnv, "UAH_HOME="+env.uahHome), dir: ws}
	}
}

func slicesClone(s []string) []string { return append([]string(nil), s...) }

// launched is what a finished harness process left.
type launched struct {
	start  time.Time
	wall   time.Duration
	exit   int
	status string
	stream string // stdout, each line stamped with the time it was read
}

// launch runs the harness with stdin closed, stamping each stdout line
// with the time it arrived, and stops its process group at the limit.
func launch(ctx context.Context, h harnessRun, limit time.Duration, art string) (launched, error) {
	l := launched{stream: filepath.Join(art, "stream.jsonl")}
	stream, err := os.Create(l.stream)
	if err != nil {
		return l, err
	}
	defer stream.Close()
	stderr, err := os.Create(filepath.Join(art, "stderr.txt"))
	if err != nil {
		return l, err
	}
	defer stderr.Close()
	runCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	cmd := exec.CommandContext(runCtx, h.name, h.args...)
	cmd.Dir, cmd.Env, cmd.Stderr = h.dir, h.env, stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		time.AfterFunc(5*time.Second, func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })

		return nil
	}
	cmd.WaitDelay = 15 * time.Second
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return l, err
	}
	l.start = time.Now()
	if err := cmd.Start(); err != nil {
		return l, fmt.Errorf("start %s: %w", h.name, err)
	}
	w := bufio.NewWriter(stream)
	sc := bufio.NewScanner(pipe)
	sc.Buffer(make([]byte, 0, 1<<16), 64<<20)
	for sc.Scan() {
		_, _ = w.WriteString(time.Now().UTC().Format(time.RFC3339Nano) + "\t")
		_, _ = w.Write(sc.Bytes())
		_ = w.WriteByte('\n')
	}
	_ = w.Flush()
	err = cmd.Wait()
	l.wall = time.Since(l.start)
	// The rest of the process group (a background command the agent
	// left) goes with it.
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	l.exit = cmd.ProcessState.ExitCode()
	switch {
	case runCtx.Err() != nil && ctx.Err() == nil:
		l.status = StatusTimeout

		return l, nil
	case err == nil:
		l.status = StatusDone
	default:
		if _, ok := errors.AsType[*exec.ExitError](err); !ok {
			return l, err
		}
		l.status = StatusFailed
	}

	return l, nil
}

// saveDiff stages everything in ws, writes the diff from the task's commit,
// and returns its stat.
func saveDiff(ctx context.Context, ws, path string) string {
	git := func(args ...string) string {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = ws
		out, _ := cmd.Output()

		return string(out)
	}
	git("add", "-A")
	_ = os.WriteFile(path, []byte(git("diff", "--cached")), 0o644)

	return strings.TrimSpace(git("diff", "--cached", "--shortstat"))
}

func writeJSON(path string, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err == nil {
		_ = os.WriteFile(path, b, 0o644)
	}
}
