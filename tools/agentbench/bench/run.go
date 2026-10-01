package bench

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	// Mode is the permission mode of both: ModeAuto or ModeWorkspace.
	Mode string
	Keep bool
	Log  io.Writer
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
	env, err := newEnv(cfg.Work, cfg.Mode)
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
	work      string
	tmp       string
	uahHome   string
	uahConfig string // uah's user configuration: the permission mode only
	codexHome string // the user's, for a run with a fake home
	base      []string
}

// newEnv makes the shared directories and the base environment: the
// user's, without variables that would steer either harness away from its
// defaults, with a temporary directory both sandboxes let commands write
// (and the Go build cache in it), and no network for Go.
func newEnv(work, mode string) (*runEnv, error) {
	e := &runEnv{work: work, tmp: filepath.Join(work, "tmp"), uahHome: filepath.Join(work, "uah-home"), codexHome: os.Getenv("CODEX_HOME")}
	if e.codexHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		e.codexHome = filepath.Join(home, ".codex")
	}
	for _, d := range []string{e.tmp, e.uahHome, filepath.Join(work, "runs")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	e.uahConfig = filepath.Join(work, "uah-"+cmp.Or(mode, ModeWorkspace)+".toml")
	conf := ""
	if mode == ModeAuto {
		conf = "permission_mode = \"auto\"\n"
	}
	if err := os.WriteFile(e.uahConfig, []byte(conf), 0o644); err != nil {
		return nil, err
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
	fx, err := t.StartFixture(ctx, scratch, env.base)
	if err != nil {
		res.Error = err.Error()

		return res
	}
	defer fx.Stop()
	runEnv := append(slicesClone(env.base), fx.Env...)
	ws := filepath.Join(scratch, "ws")
	if err := t.Prepare(ctx, ws, runEnv); err != nil {
		res.Error = err.Error()

		return res
	}
	limit := cmpDur(time.Duration(t.Timeout), cfg.Timeout)
	inv := invocation(cfg, env, fx, t, k, ws, art)
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
	if res.Status == StatusFailed && res.Metrics.Turns == 0 {
		// The harness never took the prompt: a bad flag, a login, a
		// crash. Resume runs it again.
		res.Status = StatusError
		b, _ := os.ReadFile(filepath.Join(art, "stderr.txt"))
		res.Error = strings.TrimSpace(res.Error + "; harness: " + oneLine(string(b), 200))
	}
	res.DiffStat = saveDiff(ctx, ws, filepath.Join(art, "diff.patch"))
	chk, err := t.RunCheck(ctx, ws, filepath.Join(scratch, "check"), runEnv, fx)
	res.Check = chk
	if err != nil {
		res.Error = strings.TrimSpace(res.Error + "; " + err.Error())
		res.Status = StatusError
	}
	_ = os.WriteFile(filepath.Join(art, "check.txt"), []byte(chk.Output), 0o644)
	res.Passed = chk.Passed && res.Status != StatusTimeout

	return res
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
	if res.Harness == HarnessCodex {
		for i := range tl.Requests {
			tl.Requests[i].Effort = res.Effort // set once for the session
		}
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
