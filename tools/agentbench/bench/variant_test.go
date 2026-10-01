package bench_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/tools/agentbench/bench"
)

// TestPlanVariant: a variant labels the uah runs only, so their keys differ
// from the control's and Codex's runs stay shared.
func TestPlanVariant(t *testing.T) {
	cfg := bench.Config{Tasks: []bench.Task{{Name: "a"}}, Harnesses: []string{"uah", "codex"}, Repeat: 1, Model: "m", Effort: "high", Variant: "freeform-patch"}
	keys := bench.Plan(cfg)
	require.Len(t, keys, 2)
	assert.Equal(t, "freeform-patch", keys[0].Variant)
	assert.Equal(t, "a/uah+freeform-patch/m-high/1", keys[0].String())
	assert.Empty(t, keys[1].Variant)
	cfg.Variant = ""
	assert.NotEqual(t, keys[0], bench.Plan(cfg)[0], "the control's key")
	assert.Equal(t, keys[1], bench.Plan(cfg)[1], "Codex's key is the same")
}

// TestReportVariant: the report counts a variant as its own harness, keeps
// it out of the uah-against-Codex table, and compares it with the control
// per task.
func TestReportVariant(t *testing.T) {
	run := func(variant string, wallMS, patch int64, passed bool) bench.Result {
		return bench.Result{
			Key:    bench.Key{Task: "md-revise", Harness: "uah", Variant: variant, Model: "m", Effort: "high", Repeat: 1},
			Passed: passed,
			Metrics: bench.Metrics{
				WallMS: wallMS, Requests: 10, Tokens: bench.Tokens{Output: 2 * patch},
				Behavior: bench.Behavior{OutputPatch: patch},
			},
		}
	}
	codex := bench.Result{Key: bench.Key{Task: "md-revise", Harness: "codex", Model: "m", Effort: "high", Repeat: 1}, Metrics: bench.Metrics{WallMS: 100_000}}
	md := bench.Report([]bench.Result{run("", 400_000, 9000, true), run("freeform-patch", 200_000, 6000, true), codex}, bench.Price{})

	assert.Contains(t, md, "| uah+freeform-patch | 1 |", "a row of its own per harness")
	assert.Contains(t, md, "### uah+freeform-patch against uah, per task")
	assert.Contains(t, md, "| md-revise | 1/1 / 1/1 | 400.0 | 200.0 | 0.50 | 10 | 10 | 18000 | 12000 | 9000 | 6000 |")
	assert.Contains(t, md, "| **all runs** | 1/1 / 1/1 | 400.0 | 200.0 | 0.50 |")
	assert.Contains(t, md, "| md-revise | 1/1 | 0/1 | 400.0 | 100.0 | 4.00 |", "uah against Codex is the control's")
	assert.Contains(t, md, "| md-revise | uah+freeform-patch | 1 |", "the runs table names the variant")
}
