package main_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// modelsServer is a loopback ChatGPT backend that lists two models and a
// hidden one, in Codex's shape.
func modelsServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" || r.URL.Query().Get("client_version") == "" || r.Header.Get("ChatGPT-Account-ID") != "acct-test" {
			http.NotFound(w, r)

			return
		}
		_, _ = w.Write([]byte(`{"models":[
			{"slug":"gpt-6-sol","display_name":"GPT-6-Sol","visibility":"list","priority":2,"context_window":272000,
			 "supported_reasoning_levels":[{"effort":"low"},{"effort":"high"}],"default_reasoning_level":"low",
			 "service_tiers":[{"id":"priority"}]},
			{"slug":"gpt-6-luna","display_name":"GPT-6-Luna","visibility":"list","priority":3,"context_window":400000},
			{"slug":"gpt-hidden","visibility":"hide","priority":9}]}`))
	}))
	t.Cleanup(srv.Close)

	return srv
}

func TestModels(t *testing.T) {
	e, env := fakeEnv(t, "simple.jsonl")
	env = append(env, "UNREAL_HARNESS_LLM_BASE_URL="+modelsServer(t).URL)

	res := uahWith(t, env, "", "models", "-C", e.Workspace)
	require.Equal(t, 0, res.code, res.stderr)
	assert.Equal(t, "2 models from openai-codex (live list)\n"+
		"gpt-6-sol · 272k context · effort low (low, high) · fast\n"+
		"gpt-6-luna · 400k context\n", res.stdout)

	res = uahWith(t, env, "", "models", "--json", "--all", "-C", e.Workspace)
	require.Equal(t, 0, res.code, res.stderr)
	var out struct {
		Provider, Origin string
		Models           []struct {
			ID     string `json:"id"`
			Hidden bool   `json:"hidden"`
		}
	}
	require.NoError(t, json.Unmarshal([]byte(res.stdout), &out), res.stdout)
	assert.Equal(t, "cached", out.Origin, "the second call reads the fresh cache")
	require.Len(t, out.Models, 3)
	assert.True(t, out.Models[2].Hidden)

	// Completion reads the cache, without credentials or the network.
	comp := uahWith(t, env, "", "--model", "--generate-shell-completion")
	require.Equal(t, 0, comp.code, comp.stderr)
	assert.Equal(t, []string{"gpt-6-sol", "gpt-6-luna"}, strings.Fields(comp.stdout))
}

func TestModelsCompletionBundled(t *testing.T) {
	_, env := fakeEnv(t, "simple.jsonl")
	res := uahWith(t, env, "", "-m", "--generate-shell-completion")
	require.Equal(t, 0, res.code, res.stderr)
	assert.Contains(t, strings.Fields(res.stdout), "gpt-6-sol", "no cache: the bundled list")
	res = uahWith(t, env, "", "--provider", "ollama", "-m", "--generate-shell-completion")
	assert.Empty(t, strings.TrimSpace(res.stdout), "no list for ollama without a cache")
}
