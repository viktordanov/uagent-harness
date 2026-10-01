package perf_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/tools/perf/perf"
)

// environment are the variables perf.NewEnv sets; the test restores them.
var environment = []string{
	"HOME", "UAH_HOME", "CODEX_HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "OPENAI_API_KEY", "SHELL",
	"UAH_CONFIG", "UAH_STATE_DIR", "UAH_EXTRA_CONFIG", "UAGENT_CONFIG", "UAGENT_STATE_DIR",
	"UAH_SANDBOX", "UAH_PROVIDER", "UAH_MODEL", "UAH_EFFORT", "UAH_LOG_LEVEL",
}

// bound is a ceiling for one metric of one scenario: about ten times what
// the scenario costs on a laptop (and five times that again under the race
// detector), so only a large regression fails the test.
type bound struct {
	scenario, metric string
	max              float64
}

var bounds = []bound{
	{"load/small", "first_request_ms", 2_000},
	{"load/small", "alloc_mb", 200},
	{"tui/small", "first_frame_ms", 2_000},
	{"tui/small", "view_p95_ms", 50},
	{"turn/small", "cpu_ms", 2_000},
	{"turn/small", "alloc_mb", 500},
	{"turn/small", "records_appended", 300},
	{"fork/small", "child_first_request_ms", 10_000},
	{"spawn/small", "child_first_request_ms", 3_000},
	{"tui-turn/small", "view_p95_ms", 50},
	// The TUI's clock stops when the turn ends: no update while idle.
	{"idle/tui", "updates_per_s", 1},
	{"idle/tui", "cpu_ms_per_s", 200},
	{"agents/small", "peak_goroutines", 1_000},
	{"leak/5-runs", "goroutines_left", 50},
	{"leak/5-runs", "conns_after", 20},
}

// TestPerf_SmallFixtures runs every scenario on the small fixture and
// checks generous ceilings, so CI catches a large regression without
// flaking on a slow machine. go run ./tools/perf measures properly.
func TestPerf_SmallFixtures(t *testing.T) {
	if testing.Short() {
		t.Skip("the performance harness takes about ten seconds")
	}
	for _, k := range environment {
		t.Setenv(k, os.Getenv(k)) // restored after the test
	}
	rep, err := perf.Run(context.Background(), perf.Options{Sizes: []perf.Size{perf.Small}, Scratch: t.TempDir(), Keep: true})
	require.NoError(t, err)
	slack := 1.0
	if raceEnabled {
		slack = 5
	}
	for _, b := range bounds {
		r, ok := rep.Result(b.scenario)
		if !assert.True(t, ok, "%s ran", b.scenario) {
			continue
		}
		got, ok := r.Metrics[b.metric]
		if assert.True(t, ok, "%s measured %s", b.scenario, b.metric) {
			assert.LessOrEqual(t, got, b.max*slack, "%s %s", b.scenario, b.metric)
		}
	}
	if t.Failed() {
		var out testWriter
		rep.Write(&out)
		t.Log("\n" + string(out))
	}
}

type testWriter []byte

func (w *testWriter) Write(b []byte) (int, error) {
	*w = append(*w, b...)

	return len(b), nil
}
