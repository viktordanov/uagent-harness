package bench

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Validation is a task's dry run: its check on the untouched repository
// (which must fail) and on the reference solution (which must pass).
type Validation struct {
	Task      string      `json:"task"`
	Untouched CheckResult `json:"untouched"`
	Solved    CheckResult `json:"solved"`
	Error     string      `json:"error,omitempty"`
}

// OK reports whether the task is valid.
func (v Validation) OK() bool { return v.Error == "" && !v.Untouched.Passed && v.Solved.Passed }

// Validate dry-runs each task, parallel at once, with no model calls. It
// uses the same environment as a run, so it also warms the Go build cache.
func Validate(ctx context.Context, tasks []Task, work string, parallel int) ([]Validation, error) {
	env, err := newEnv(work)
	if err != nil {
		return nil, err
	}
	out := make([]Validation, len(tasks))
	sem := make(chan struct{}, max(1, parallel))
	var wg sync.WaitGroup
	for i, t := range tasks {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = validate(ctx, t, env)
		})
	}
	wg.Wait()

	return out, ctx.Err()
}

func validate(ctx context.Context, t Task, env *runEnv) Validation {
	v := Validation{Task: t.Name}
	scratch, err := os.MkdirTemp(filepath.Join(env.work, "runs"), "dry-"+t.Name+"-")
	if err != nil {
		v.Error = err.Error()

		return v
	}
	defer os.RemoveAll(scratch)
	ws := filepath.Join(scratch, "ws")
	if err := t.Prepare(ctx, ws); err != nil {
		v.Error = err.Error()

		return v
	}
	if v.Untouched, err = t.RunCheck(ctx, ws, filepath.Join(scratch, "check"), env.base); err != nil {
		v.Error = err.Error()

		return v
	}
	if err := t.ApplySolution(ws); err != nil {
		v.Error = "solution: " + err.Error()

		return v
	}
	if v.Solved, err = t.RunCheck(ctx, ws, filepath.Join(scratch, "check"), env.base); err != nil {
		v.Error = err.Error()
	}

	return v
}

// FormatValidation is the dry run's table.
func FormatValidation(vs []Validation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-28s %-6s %-22s %-22s\n", "task", "valid", "untouched (must fail)", "solution (must pass)")
	for _, v := range vs {
		ok := "yes"
		if !v.OK() {
			ok = "NO"
		}
		fmt.Fprintf(&b, "%-28s %-6s %-22s %-22s %s\n", v.Task, ok, outcome(v.Untouched), outcome(v.Solved), v.Error)
	}

	return b.String()
}

func outcome(c CheckResult) string {
	word := "fail"
	if c.Passed {
		word = "pass"
	}

	return fmt.Sprintf("%s %.1fs", word, (time.Duration(c.DurationMS) * time.Millisecond).Seconds())
}
