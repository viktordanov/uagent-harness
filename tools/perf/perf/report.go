package perf

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// Report is a run's results: what ran where, and one result per scenario.
type Report struct {
	Version int       `json:"version"`
	Date    time.Time `json:"date"`
	Commit  string    `json:"commit,omitempty"`
	Go      string    `json:"go"`
	OS      string    `json:"os"`
	Arch    string    `json:"arch"`
	CPUs    int       `json:"cpus"`
	Count   int       `json:"count"`
	// Sandbox is the sandbox commands ran in: workspace-write, or none
	// where uah has no sandbox (the harness then runs in yolo mode).
	Sandbox string   `json:"sandbox"`
	Results []Result `json:"results"`
}

// Result is one scenario's medians.
type Result struct {
	Name string `json:"name"`
	// Records, SessionMB, and Runs describe the session it ran on.
	Records   int     `json:"records,omitempty"`
	SessionMB float64 `json:"session_mb,omitempty"`
	Runs      int     `json:"runs,omitempty"`
	// Metrics are the medians by name: the Sample's fields and extras.
	Metrics map[string]float64 `json:"metrics"`
}

const reportVersion = 1

func newReport(ctx context.Context, opts Options) *Report {
	return &Report{
		Version: reportVersion, Date: time.Now().UTC().Truncate(time.Second), Commit: commit(ctx),
		Go: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH, CPUs: runtime.NumCPU(), Count: opts.Count,
		Sandbox: "workspace-write",
	}
}

// commit is the working tree's commit, with "+dirty" when it has changes.
func commit(ctx context.Context) string {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	c := strings.TrimSpace(string(out))
	if status, err := exec.CommandContext(ctx, "git", "status", "--porcelain", "--untracked-files=no").Output(); err == nil && len(status) > 0 {
		c += "+dirty"
	}

	return c
}

// flatten is a sample's metrics by name.
func flatten(s Sample) map[string]float64 {
	m := map[string]float64{
		"wall_ms": ms(s.Wall), metricCPU: ms(s.CPU), "child_cpu_ms": ms(s.ChildCPU),
		"alloc_mb": mb(int64(s.AllocBytes)), metricAllocs: float64(s.Allocs), "peak_heap_mb": mb(int64(s.PeakHeap)), // sizes fit
		metricGoroutinesBefore: float64(s.GoroutinesBefore), metricGoroutinesAfter: float64(s.GoroutinesAfter),
		// Each connection still open holds one goroutine of the fake model's
		// server, which is not uah's.
		metricGoroutinesLeft: float64(s.GoroutinesAfter - s.GoroutinesBefore - s.ConnsAfter),
		metricConnsAfter:     float64(s.ConnsAfter), "disk_written_mb": mb(int64(s.DiskWritten)), // sizes fit
		"state_growth_mb": mb(s.StateGrowth), "wakeups": float64(s.Wakeups),
	}
	maps.Copy(m, s.Extra)

	return m
}

// add keeps the medians of samples.
func (rep *Report) add(name string, fx *Fixture, samples []Sample) {
	r := Result{Name: name, Metrics: map[string]float64{}}
	if fx != nil {
		r.Records, r.SessionMB, r.Runs = fx.Records, mb(fx.Bytes), fx.Runs
	}
	all := make([]map[string]float64, 0, len(samples))
	for _, s := range samples {
		all = append(all, flatten(s))
	}
	for k := range all[0] {
		vs := make([]float64, 0, len(all))
		for _, m := range all {
			vs = append(vs, m[k])
		}
		slices.Sort(vs)
		r.Metrics[k] = vs[len(vs)/2]
	}
	rep.Results = append(rep.Results, r)
}

// Result returns the named result.
func (rep *Report) Result(name string) (Result, bool) {
	i := slices.IndexFunc(rep.Results, func(r Result) bool { return r.Name == name })
	if i < 0 {
		return Result{}, false
	}

	return rep.Results[i], true
}

// Save writes the report as JSON.
func (rep *Report) Save(path string) error {
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode the report: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("failed to write the report: %w", err)
	}

	return nil
}

// LoadReport reads a saved report.
func LoadReport(path string) (*Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read the report: %w", err)
	}
	var rep Report
	if err := json.Unmarshal(data, &rep); err != nil {
		return nil, fmt.Errorf("failed to decode %s: %w", path, err)
	}

	return &rep, nil
}

// Metric names used in more than one place.
const (
	metricAllocs           = "allocs"
	metricCPU              = "cpu_ms"
	metricGoroutinesBefore = "goroutines_before"
	metricGoroutinesAfter  = "goroutines_after"
	metricGoroutinesLeft   = "goroutines_left"
	metricConnsAfter       = "conns_after"
)

// columns are the main table's metrics.
var columns = []struct{ key, head string }{
	{"wall_ms", "wall ms"},
	{metricCPU, "cpu ms"},
	{"alloc_mb", "alloc MB"},
	{metricAllocs, "allocs"},
	{"peak_heap_mb", "heap MB"},
	{metricGoroutinesBefore, "gor"},
	{metricGoroutinesLeft, "gor left"},
	{metricConnsAfter, "conns"},
	{"disk_written_mb", "disk MB"},
	{"state_growth_mb", "state MB"},
}

// Write prints the main table, then each scenario's own measurements.
func (rep *Report) Write(w io.Writer) {
	fmt.Fprintf(w, "uah perf  commit %s  %s %s/%s  %d CPUs  count %d  sandbox %s\n\n", rep.Commit, rep.Go, rep.OS, rep.Arch, rep.CPUs, rep.Count, rep.Sandbox)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprint(tw, "scenario\trecords\tsession MB\t")
	for _, c := range columns {
		fmt.Fprintf(tw, "%s\t", c.head)
	}
	fmt.Fprintln(tw)
	for _, r := range rep.Results {
		fmt.Fprintf(tw, "%s\t%s\t%s\t", r.Name, intOrDash(r.Records), floatOrDash(r.SessionMB))
		for _, c := range columns {
			fmt.Fprintf(tw, "%s\t", format(c.key, r.Metrics[c.key]))
		}
		fmt.Fprintln(tw)
	}
	tw.Flush() // printing
	fmt.Fprintln(w)
	for _, r := range rep.Results {
		var extras []string
		for _, k := range slices.Sorted(maps.Keys(r.Metrics)) {
			if !slices.ContainsFunc(columns, func(c struct{ key, head string }) bool { return c.key == k }) {
				extras = append(extras, k+"="+format(k, r.Metrics[k]))
			}
		}
		fmt.Fprintf(w, "%-18s %s\n", r.Name, strings.Join(extras, " "))
	}
}

func format(key string, v float64) string {
	switch {
	case v == float64(int64(v)) && !strings.HasSuffix(key, "_mb"):
		return strconv.FormatInt(int64(v), 10)
	case v >= 100:
		return fmt.Sprintf("%.0f", v)
	case v >= 10:
		return fmt.Sprintf("%.1f", v)
	}

	return fmt.Sprintf("%.2f", v)
}

func intOrDash(n int) string {
	if n == 0 {
		return "-"
	}

	return strconv.Itoa(n)
}

func floatOrDash(v float64) string {
	if v == 0 {
		return "-"
	}

	return format("_mb", v)
}
