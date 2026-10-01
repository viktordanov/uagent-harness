package evalrun

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/viktordanov/unreal-agent/harness/llm"

	"github.com/viktordanov/uah/internal/compaction"
	"github.com/viktordanov/uah/internal/compaction/eval"
	"github.com/viktordanov/uah/internal/sessionfile"
)

// Options configure a run.
type Options struct {
	// Scratch holds the cut sessions while they are captured ("": a
	// temporary directory).
	Scratch    string
	Summaries  *Summaries
	Strategies []Strategy
	// Window is the context window the headroom and the automatic limit
	// use (0: 272,000, Codex's).
	Window int64
	// Progress, when set, gets a line per session (IDs and counts only).
	Progress io.Writer
	// MaxCaseTokens skips the cases whose request is larger (0: none), to
	// bound what a run with a live summary model costs.
	MaxCaseTokens int64
}

// Report is a run's aggregates and what they rest on.
type Report struct {
	Sessions, Cases int
	// Recorded are the cuts at the sessions' own compactions, and
	// Reproduced those whose captured history hashes as the compaction's
	// record does: the capture sends what the session sent.
	Recorded, Reproduced int
	Rows                 []eval.Aggregate
	Sources              map[string]int
	// Skipped counts the cuts that failed to capture.
	Skipped int
}

// Run evaluates the strategies on the session file or directory target.
func Run(ctx context.Context, target string, opts Options) (Report, error) {
	dir, ids, err := Sessions(target)
	if err != nil {
		return Report{}, err
	}
	if opts.Scratch == "" {
		if opts.Scratch, err = os.MkdirTemp("", "uah-compaction-eval-"); err != nil {
			return Report{}, fmt.Errorf("failed to make a scratch directory: %w", err)
		}
		defer os.RemoveAll(opts.Scratch)
	}
	if opts.Window == 0 {
		opts.Window = compaction.DefaultContextWindow
	}
	if opts.Summaries == nil {
		opts.Summaries = &Summaries{}
	}
	var rep Report
	results := map[string][]eval.Result{}
	for _, id := range ids {
		n, err := runSession(ctx, dir, id, opts, &rep, results)
		if err != nil {
			return rep, err
		}
		if n > 0 {
			rep.Sessions++
			if opts.Progress != nil {
				fmt.Fprintf(opts.Progress, "session %.8s: %d cases\n", id, n)
			}
		}
	}
	for _, s := range opts.Strategies {
		rep.Rows = append(rep.Rows, eval.Summarize(s.Name, s.Opaque, results[s.Name]))
	}
	rep.Sources = opts.Summaries.Sources

	return rep, nil
}

func runSession(ctx context.Context, dir, id string, opts Options, rep *Report, results map[string][]eval.Result) (int, error) {
	points, err := Points(dir, id)
	if err != nil {
		return 0, err
	}
	if len(points) == 0 {
		return 0, nil
	}
	if err := opts.Summaries.Record(dir, id); err != nil {
		return 0, err
	}
	later, err := laterCalls(filepath.Join(dir, id+".session.jsonl"))
	if err != nil {
		return 0, err
	}
	n := 0
	for _, p := range points {
		before, err := Capture(ctx, opts.Scratch, dir, id, p)
		if err != nil {
			rep.Skipped++
			if opts.Progress != nil {
				fmt.Fprintf(opts.Progress, "session %.8s: skipped the cut after item %d: %v\n", id, p.Seq, err)
			}

			continue
		}
		if opts.MaxCaseTokens > 0 && compaction.EstimateTokens(before) > opts.MaxCaseTokens {
			continue
		}
		if p.Label == "recorded" {
			rep.Recorded++
			if reproduces(dir, id, before) {
				rep.Reproduced++
			}
		}
		c := eval.Case{Name: fmt.Sprintf("%.8s#%d", id, p.Seq), Before: before, Later: later.after(p.Seq), Focus: p.Focus, Window: opts.Window}
		for _, s := range opts.Strategies {
			after, call, err := s.Apply(ctx, c, opts.Summaries)
			if err != nil {
				return n, fmt.Errorf("strategy %s on %s: %w", s.Name, c.Name, err)
			}
			r := eval.Measure(c, after)
			r.CallInput = call
			results[s.Name] = append(results[s.Name], r)
		}
		n++
		rep.Cases++
	}

	return n, nil
}

// reproduces reports whether a recorded compaction covers exactly the
// captured history's coverable items.
func reproduces(dir, id string, before []llm.Item) bool {
	records, _, err := compaction.OpenLog(dir, id).Records()
	if err != nil {
		return false
	}
	hash, err := compaction.Hash(before[1 : 1+compaction.Coverable(before)])
	if err != nil {
		return false
	}
	for _, r := range records {
		if r.Hash == hash {
			return true
		}
	}

	return false
}

// callsAt are a session's tool calls with the sequence of the response
// that made them.
type callsAt struct {
	seqs  []uint64
	calls []llm.ToolCall
}

func (c callsAt) after(seq uint64) []llm.ToolCall {
	for i, s := range c.seqs {
		if s > seq {
			return c.calls[i:]
		}
	}

	return nil
}

func laterCalls(path string) (callsAt, error) {
	_, page, err := sessionfile.Read(path, 0, 0)
	if err != nil {
		return callsAt{}, fmt.Errorf("failed to read %s: %w", filepath.Base(path), err)
	}
	var out callsAt
	for _, it := range page.Items {
		var r sessionfile.ModelResponse
		if it.Kind != sessionfile.KindModelResponse || it.Decode(&r) != nil {
			continue
		}
		for _, o := range r.Response.Output {
			var c sessionfile.ToolCall
			if o.Type == sessionfile.OutputToolCall && o.Decode(&c) == nil {
				out.seqs = append(out.seqs, it.Sequence)
				out.calls = append(out.calls, llm.ToolCall{CallID: c.CallID, Name: c.Name, Arguments: c.Arguments, Custom: c.Custom})
			}
		}
	}

	return out, nil
}
