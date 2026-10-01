package embedded_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/internal/engine/embedded"
	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/testing/fakellm"
)

// TestWakeForegroundWaitsForBash: by default a quick command's result wakes
// the model a second after the turn while a slow one still runs; with
// wake-foreground the turn waits for both, and Bash offers background.
func TestWakeForegroundWaitsForBash(t *testing.T) {
	for _, foreground := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "foreground"}[foreground], func(t *testing.T) {
			e := newEnv(t,
				fakellm.Reply{Commands: []string{"echo fast", "sleep 2; echo slow"}},
				fakellm.Reply{Text: "waiting"},
				fakellm.Reply{Text: "done"},
			)
			getenv := e.getenv
			if foreground {
				getenv = func(key string) string {
					if key == "UAH_EXPERIMENTS" {
						return "wake-foreground"
					}

					return e.getenv(key)
				}
			}
			eng := embedded.New(embedded.Config{StateDir: e.StateDir, Provider: "openai", Getenv: getenv})
			s, err := session.Open(context.Background(), eng, session.Options{Settings: e.settings()})
			require.NoError(t, err)
			t.Cleanup(func() { _ = s.Close() })
			ev := &events{t: t, s: s}
			_, err = s.Submit("run both")
			require.NoError(t, err)
			ev.finished()

			reqs := e.llm.Requests()
			require.GreaterOrEqual(t, len(reqs), 2)
			outputs := strings.Join(reqs[1].ToolOutputs, "\n")
			assert.Contains(t, outputs, "fast")
			assert.Equal(t, foreground, strings.Contains(outputs, "slow"), "the second request waited for the slow command: %s", outputs)
			assert.Equal(t, foreground, strings.Contains(reqs[0].Tools["Bash"], `"background"`), "Bash offers background")
		})
	}
}
