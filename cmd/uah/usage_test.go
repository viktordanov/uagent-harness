package main_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// usagePath is the usage endpoint under a loopback base URL without
// /backend-api.
const usagePath = "/api/codex/usage"

// serveUsage answers with the Pro login's fixture: one weekly window, sent
// as the primary window.
func serveUsage(w http.ResponseWriter, r *http.Request) {
	body, err := os.ReadFile(filepath.Join("..", "..", "internal", "usage", "testdata", "pro_weekly_only.json"))
	if err != nil || r.Header.Get("ChatGPT-Account-ID") != "acct-test" || r.Header.Get("User-Agent") != "unreal-agent" {
		http.NotFound(w, r)

		return
	}
	_, _ = w.Write(body)
}

func TestUsage(t *testing.T) {
	e, env := fakeEnv(t, "simple.jsonl")
	srv := httptest.NewServer(http.HandlerFunc(serveUsage))
	t.Cleanup(srv.Close)
	env = append(env, "UNREAL_HARNESS_LLM_BASE_URL="+srv.URL, "TZ=UTC")

	res := uahWith(t, env, "", "usage", "-C", e.Workspace)
	require.Equal(t, 0, res.code, res.stderr)
	assert.Equal(t, "pro plan (openai-codex)\n"+
		"weekly  [███████████████░░░░░]  22% used · 78% left · resets 12:44 on 26 Sep\n", res.stdout)

	res = uahWith(t, env, "", "usage", "--json", "-C", e.Workspace)
	require.Equal(t, 0, res.code, res.stderr)
	var out struct {
		Provider string `json:"provider"`
		Plan     string `json:"plan"`
		Reached  bool   `json:"limit_reached"`
		Windows  []struct {
			Name        string  `json:"name"`
			Minutes     int64   `json:"minutes"`
			UsedPercent float64 `json:"used_percent"`
			LeftPercent float64 `json:"left_percent"`
			ResetsAt    string  `json:"resets_at"`
		} `json:"windows"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.stdout), &out), res.stdout)
	assert.Equal(t, "openai-codex", out.Provider)
	assert.Equal(t, "pro", out.Plan)
	assert.False(t, out.Reached)
	require.Len(t, out.Windows, 1)
	assert.Equal(t, "weekly", out.Windows[0].Name)
	assert.Equal(t, int64(10080), out.Windows[0].Minutes)
	assert.InDelta(t, 22, out.Windows[0].UsedPercent, 0)
	assert.InDelta(t, 78, out.Windows[0].LeftPercent, 0)
	assert.Equal(t, "2026-09-26T12:44:39Z", out.Windows[0].ResetsAt)
}

func TestUsage_OtherProviders(t *testing.T) {
	e, env := fakeEnv(t, "simple.jsonl")
	res := uahWith(t, env, "", "usage", "--provider", "ollama", "-m", "llama3", "-C", e.Workspace)
	assert.Equal(t, 1, res.code)
	assert.Equal(t, "uah: usage is not available for ollama\n", res.stderr)
	assert.Empty(t, res.stdout)
}
