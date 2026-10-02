package main_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/testing/fakellm"
	"github.com/viktordanov/uah/testing/harnesstest"
)

// TestExecWebSearch: web search is on by default on openai, `uah exec
// --json` reports each search, plain exec prints it as progress, and
// web_search = "disabled" takes the tool away.
func TestExecWebSearch(t *testing.T) {
	e := harnesstest.NewEnv(t)
	reply := fakellm.Reply{Text: "Go 1.27", Searches: []fakellm.Search{{Query: "latest Go release"}}}
	llm := fakellm.New(t, reply, reply, fakellm.Reply{Text: "ok"})
	home := filepath.Join(e.StateDir, "..", "home")
	require.NoError(t, os.MkdirAll(home, 0o700))
	env := []string{
		"UAH_STATE_DIR=" + e.StateDir, "OPENAI_API_KEY=test-key",
		"UAH_LLM_PROVIDER=", "UAH_LLM_MODEL=", "UAH_HOME=" + home,
	}
	args := []string{"--provider", "openai", "-m", "gpt-test", "--base-url", llm.URL, "-C", e.Workspace, "which Go?"}

	res := uahWith(t, env, "", append([]string{"exec", "--json"}, args...)...)
	require.Equal(t, 0, res.code, res.stderr)
	var searches []string
	for line := range strings.Lines(res.stdout) {
		var ev struct {
			Type  string `json:"type"`
			Done  bool   `json:"done"`
			Query string `json:"query"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &ev), line)
		if ev.Type == "web_search" && ev.Done {
			searches = append(searches, ev.Query)
		}
	}
	assert.Equal(t, []string{"latest Go release"}, searches)

	res = uahWith(t, env, "", append([]string{"exec"}, args...)...)
	require.Equal(t, 0, res.code, res.stderr)
	assert.Contains(t, res.stderr, "searched: latest Go release")

	require.NoError(t, os.WriteFile(filepath.Join(home, "config.toml"), []byte(`web_search = "disabled"`+"\n"), 0o600))
	res = uahWith(t, env, "", append([]string{"exec", "--quiet"}, args...)...)
	require.Equal(t, 0, res.code, res.stderr)

	reqs := llm.Requests()
	require.Len(t, reqs, 3)
	offered := func(r fakellm.Request) bool {
		return strings.Contains(strings.Join(rawDefs(r), ","), `"web_search"`)
	}
	assert.True(t, offered(reqs[0]), "on by default")
	assert.True(t, offered(reqs[1]))
	assert.False(t, offered(reqs[2]), "disabled")
}

func rawDefs(r fakellm.Request) []string {
	out := make([]string, 0, len(r.ToolDefs))
	for _, d := range r.ToolDefs {
		out = append(out, string(d))
	}

	return out
}
