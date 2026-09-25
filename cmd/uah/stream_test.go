package main_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/testing/fakellm"
	"github.com/viktordanov/uagent-harness/testing/harnesstest"
)

// TestRunStreamsTheAnswer: `uah run --stream` writes the answer's deltas
// before the final message; plain `uah run` prints the answer once.
func TestRunStreamsTheAnswer(t *testing.T) {
	e := harnesstest.NewEnv(t)
	reply := fakellm.Reply{Deltas: []string{"Hel", "lo"}}
	llm := fakellm.New(t, reply, reply)
	home := filepath.Join(e.StateDir, "..", "home")
	require.NoError(t, os.MkdirAll(home, 0o700))
	env := []string{
		"UAH_ENGINE=embedded", "UAH_STATE_DIR=" + e.StateDir, "OPENAI_API_KEY=test-key",
		"UNREAL_HARNESS_LLM_PROVIDER=", "UNREAL_HARNESS_LLM_MODEL=", "UAH_HOME=" + home,
	}
	args := []string{"--provider", "openai", "-m", "gpt-test", "--base-url", llm.URL, "-C", e.Workspace, "hi"}

	res := uahWith(t, env, "", append([]string{"run", "--stream"}, args...)...)
	require.Equal(t, 0, res.code, res.stderr)
	var streamed strings.Builder
	var order []string
	for line := range strings.Lines(res.stdout) {
		var ev struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &ev), line)
		switch ev.Type {
		case "text_delta":
			streamed.WriteString(ev.Text)
			order = append(order, ev.Type)
		case "assistant_message":
			order = append(order, ev.Type)
		}
	}
	assert.Equal(t, "Hello", streamed.String())
	require.NotEmpty(t, order)
	assert.Equal(t, "text_delta", order[0])
	assert.Equal(t, "assistant_message", order[len(order)-1])

	res = uahWith(t, env, "", append([]string{"run", "--quiet"}, args...)...)
	require.Equal(t, 0, res.code, res.stderr)
	assert.Equal(t, "Hello\n", res.stdout)
}
