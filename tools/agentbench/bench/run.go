package bench

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Harness names.
const (
	HarnessUAH   = "uah"
	HarnessCodex = "codex"
)

// Config is one invocation of the benchmark.
type Config struct {
	Tasks     []Task
	Harnesses []string
	Repeat    int
	Model     string
	Effort    string
	Parallel  int
	// Timeout is a run's wall-clock limit unless its task sets one.
	Timeout time.Duration
	// MaxRuns refuses a plan with more runs than this.
	MaxRuns int
	// Work is the scratch root: workspaces, the shared temporary
	// directory and Go build cache, and uah's home.
	Work string
	// Out is the results file (JSON lines); run artifacts go in the
	// directory of the same name without the extension.
	Out   string
	UAH   string // the uah binary
	Codex string // the codex binary
	Price Price
	Keep  bool
	Log   io.Writer
}

// Key names a run; a results file holds each key once.
type Key struct {
	Task    string `json:"task"`
	Harness string `json:"harness"`
	Model   string `json:"model"`
	Effort  string `json:"effort"`
	Repeat  int    `json:"repeat"`
}

func (k Key) String() string {
	return fmt.Sprintf("%s/%s/%s-%s/%d", k.Task, k.Harness, k.Model, k.Effort, k.Repeat)
}

// Run statuses.
const (
	StatusDone    = "done"    // the harness exited 0
	StatusFailed  = "failed"  // the harness exited non-zero
	StatusTimeout = "timeout" // the wall-clock limit stopped it
	StatusError   = "error"   // the benchmark could not run it; resume retries it
)

// Result is one run's line in the results file.
type Result struct {
	Key

	Status   string `json:"status"`
	Passed   bool   `json:"passed"`
	ExitCode int    `json:"exit_code"`
	// StartedAt is when the harness process started: the timeline's zero.
	StartedAt time.Time   `json:"started_at"`
	Check     CheckResult `json:"check"`
	Metrics   Metrics     `json:"metrics"`
	DiffStat  string      `json:"diff_stat,omitempty"`
	Error     string      `json:"error,omitempty"`
	// Artifacts is the run's directory: the stamped event stream, stderr,
	// the timeline, the diff, and uah's state.
	Artifacts string `json:"artifacts"`
}

// Plan lists the runs of cfg in the order they start: repeats outermost,
// and the harnesses' order alternating by repeat so neither always goes
// first.
func Plan(cfg Config) []Key {
	var keys []Key
	for r := 1; r <= cfg.Repeat; r++ {
		for i, t := range cfg.Tasks {
			hs := cfg.Harnesses
			if (r+i)%2 == 0 && len(hs) == 2 {
				hs = []string{hs[1], hs[0]}
			}
			for _, h := range hs {
				keys = append(keys, Key{Task: t.Name, Harness: h, Model: cfg.Model, Effort: cfg.Effort, Repeat: r})
			}
		}
	}

	return keys
}

// LoadResults reads a results file; a missing file has none.
func LoadResults(path string) ([]Result, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Result
	err = eachLine(f, func(_ time.Time, line []byte) error {
		var r Result
		if err := json.Unmarshal(line, &r); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, r)

		return nil
	})

	return out, err
}

// Execute runs every planned run that the results file does not already
// hold (a run that ended in StatusError runs again), appending each result
// as it finishes.
func Execute(ctx context.Context, cfg Config) error {
	done, err := LoadResults(cfg.Out)
	if err != nil {
		return err
	}
	have := map[Key]bool{}
	for _, r := range done {
		if r.Status != StatusError {
			have[r.Key] = true
		}
	}
	var todo []Key
	for _, k := range Plan(cfg) {
		if !have[k] {
			todo = append(todo, k)
		}
	}
	if len(todo) > cfg.MaxRuns {
		return fmt.Errorf("%d runs planned, more than -max-runs %d", len(todo), cfg.MaxRuns)
	}
	fmt.Fprintf(cfg.Log, "%d runs to do (%d already in %s)\n", len(todo), len(Plan(cfg))-len(todo), cfg.Out)
	if len(todo) == 0 {
		return nil
	}
	env, err := newEnv(cfg.Work)
	if err != nil {
		return err
	}
	tasks := map[string]Task{}
	for _, t := range cfg.Tasks {
		tasks[t.Name] = t
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Out), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(cfg.Out, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	var mu sync.Mutex
	jobs := make(chan Key)
	var wg sync.WaitGroup
	for range max(1, cfg.Parallel) {
		wg.Go(func() {
			for k := range jobs {
				res := runOne(ctx, cfg, env, tasks[k.Task], k)
				b, _ := json.Marshal(res)
				mu.Lock()
				_, _ = out.Write(append(b, '\n'))
				fmt.Fprintf(cfg.Log, "%-48s %-7s pass=%-5v wall=%5.0fs model=%5.0fs tools=%5.0fs overlap=%4.0fs calls=%3d tokens=%d/%d/%d %s\n",
					k, res.Status, res.Passed, sec(res.Metrics.WallMS), sec(res.Metrics.ModelMS), sec(res.Metrics.ToolMS), sec(res.Metrics.OverlapMS),
					res.Metrics.ToolCalls, res.Metrics.Tokens.Input, res.Metrics.Tokens.Cached, res.Metrics.Tokens.Output, res.Error)
				mu.Unlock()
			}
		})
	}
	for _, k := range todo {
		select {
		case jobs <- k:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
	}
	close(jobs)
	wg.Wait()

	return ctx.Err()
}

func sec(ms int64) float64 { return float64(ms) / 1000 }

// runEnv is what every run shares.
type runEnv struct {
	work    string
	tmp     string
	uahHome string
	base    []string
}

// newEnv makes the shared directories and the base environment: the
// user's, without variables that would steer either harness away from its
// defaults, with a temporary directory both sandboxes let commands write
// (and the Go build cache in it), and no network for Go.
func newEnv(work string) (*runEnv, error) {
	e := &runEnv{work: work, tmp: filepath.Join(work, "tmp"), uahHome: filepath.Join(work, "uah-home")}
	for _, d := range []string{e.tmp, e.uahHome, filepath.Join(work, "runs")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if dropEnv(name) {
			continue
		}
		e.base = append(e.base, kv)
	}
	e.base = append(e.base,
		"TMPDIR="+e.tmp,
		"GOCACHE="+filepath.Join(e.tmp, "gocache"),
		"GOPROXY=off", "GOTOOLCHAIN=local",
		// A slow suite is slow every time: no test results from the shared cache.
		"GOFLAGS=-count=1",
		"PYTHONDONTWRITEBYTECODE=1",
		"NO_COLOR=1",
	)

	return e, nil
}

func dropEnv(name string) bool {
	for _, p := range []string{"UAH_", "UNREAL_HARNESS_", "WEBTTY_", "OPENAI_", "CLAUDE", "GO", "TMPDIR", "PYTHON"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}

	return name == "NO_COLOR"
}

// runOne runs one key end to end and never returns without a result.
func runOne(ctx context.Context, cfg Config, env *runEnv, t Task, k Key) (res Result) {
	res = Result{Key: k}
	art := filepath.Join(strings.TrimSuffix(cfg.Out, filepath.Ext(cfg.Out)), k.Task, fmt.Sprintf("%s-%s-%s-%d", k.Harness, k.Model, k.Effort, k.Repeat))
	res.Artifacts = art
	defer func() {
		if res.Error != "" && res.Status == "" {
			res.Status = StatusError
		}
	}()
	if err := os.RemoveAll(art); err != nil {
		res.Error = err.Error()

		return res
	}
	if err := os.MkdirAll(art, 0o755); err != nil {
		res.Error = err.Error()

		return res
	}
	scratch, err := os.MkdirTemp(filepath.Join(env.work, "runs"), k.Task+"-"+k.Harness+"-")
	if err != nil {
		res.Error = err.Error()

		return res
	}
	if !cfg.Keep {
		defer os.RemoveAll(scratch)
	}
	ws := filepath.Join(scratch, "ws")
	if err := t.Prepare(ctx, ws); err != nil {
		res.Error = err.Error()

		return res
	}
	limit := cmpDur(time.Duration(t.Timeout), cfg.Timeout)
	inv := invocation(cfg, env, t, k, ws, art)
	out, err := launch(ctx, inv, limit, art)
	res.ExitCode, res.Status = out.exit, out.status
	if err != nil {
		res.Error = err.Error()
	}
	if !out.start.IsZero() {
		res.StartedAt = out.start.UTC()
		res.Metrics.WallMS = out.wall.Milliseconds()
		if perr := measure(&res, cfg.Price); perr != nil {
			res.Error = strings.TrimSpace(res.Error + "; parse: " + perr.Error())
		}
	}
	res.DiffStat = saveDiff(ctx, ws, filepath.Join(art, "diff.patch"))
	chk, err := t.RunCheck(ctx, ws, filepath.Join(scratch, "check"), env.base)
	res.Check = chk
	if err != nil {
		res.Error = strings.TrimSpace(res.Error + "; " + err.Error())
		res.Status = StatusError
	}
	_ = os.WriteFile(filepath.Join(art, "check.txt"), []byte(chk.Output), 0o644)
	res.Passed = chk.Passed && res.Status != StatusTimeout

	return res
}

// harnessRun is how one harness is started.
type harnessRun struct {
	name string
	args []string
	env  []string
	dir  string
}

func invocation(cfg Config, env *runEnv, t Task, k Key, ws, art string) harnessRun {
	switch k.Harness {
	case HarnessCodex:
		return harnessRun{
			name: cfg.Codex,
			args: []string{
				"exec", "--json", "--ephemeral", "--ignore-user-config", "--skip-git-repo-check",
				"-m", k.Model, "-c", "model_reasoning_effort=" + k.Effort, "-c", `approval_policy="never"`,
				"-s", "workspace-write", "-C", ws, t.Prompt,
			},
			env: env.base,
			dir: ws,
		}
	default:
		state := filepath.Join(art, "uah-state")

		return harnessRun{
			name: cfg.UAH,
			args: []string{
				"exec", "--json", "--model", k.Model, "--effort", k.Effort, "--sandbox", "workspace-write", "--ask", "never",
				"--state-dir", state, "--workspace", ws, t.Prompt,
			},
			env: append(slicesClone(env.base), "UAH_HOME="+env.uahHome),
			dir: ws,
		}
	}
}

// measure parses the run's artifacts into its timeline (saved as
// timeline.json) and computes its metrics, keeping the wall time.
func measure(res *Result, price Price) error {
	f, err := os.Open(filepath.Join(res.Artifacts, "stream.jsonl"))
	if err != nil {
		return err
	}
	defer f.Close()
	var tl *Timeline
	if res.Harness == HarnessCodex {
		tl, err = ParseCodex(f, res.StartedAt)
	} else {
		tl, err = ParseUAH(f, res.StartedAt, filepath.Join(res.Artifacts, "uah-state"))
	}
	if err != nil {
		return err
	}
	wall := time.Duration(res.Metrics.WallMS) * time.Millisecond
	tl.End = res.StartedAt.Add(wall)
	res.Metrics = tl.Compute(wall, price)
	writeJSON(filepath.Join(res.Artifacts, "timeline.json"), tl)

	return nil
}

// Remeasure parses every run in the results file again, as after a change
// to a parser or the metrics or the prices, and rewrites the file.
func Remeasure(path string, price Price) error {
	results, err := LoadResults(path)
	if err != nil {
		return err
	}
	var b []byte
	for i := range results {
		if results[i].StartedAt.IsZero() {
			continue
		}
		if err := measure(&results[i], price); err != nil {
			return fmt.Errorf("%s: %w", results[i].Key, err)
		}
		line, err := json.Marshal(results[i])
		if err != nil {
			return err
		}
		b = append(append(b, line...), '\n')
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}

	return os.Rename(tmp, path)
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
