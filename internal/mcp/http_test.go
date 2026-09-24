package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/mcp"
)

func TestStreamableHTTP(t *testing.T) {
	server := sdk.NewServer(&sdk.Implementation{Name: "http", Version: "1"}, nil)
	server.AddTool(&sdk.Tool{Name: "whoami", InputSchema: map[string]any{"type": "object"}},
		func(_ context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: req.Extra.Header.Get("X-Team")}}}, nil
		})
	handler := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)

			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	env := map[string]string{"TOKEN": "secret", "TEAM": "core"}
	m, err := mcp.NewManager(map[string]mcp.ServerConfig{
		"remote": {URL: srv.URL, BearerTokenEnvVar: "TOKEN", EnvHTTPHeaders: map[string]string{"X-Team": "TEAM"}},
	}, mcp.Options{Getenv: func(k string) string { return env[k] }})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })
	tools, err := m.Tools(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"mcp__remote__whoami"}, names(tools))
	r, err := m.Call(context.Background(), "remote", "whoami", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "core", r.Text)
}
