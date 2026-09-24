package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/mcp"
	"github.com/viktordanov/uagent-harness/testing/oauthserver"
)

func oauthManager(t *testing.T, cfg mcp.ServerConfig, store mcp.CredentialStore) *mcp.Manager {
	t.Helper()
	m, err := mcp.NewManager(map[string]mcp.ServerConfig{"remote": cfg}, mcp.Options{Credentials: store, Getenv: func(string) string { return "" }})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })
	_, err = m.Tools(context.Background())
	require.NoError(t, err)

	return m
}

func login(t *testing.T, cfg mcp.ServerConfig, store mcp.CredentialStore, scopes ...string) string {
	t.Helper()
	var out bytes.Buffer
	err := mcp.Login(context.Background(), "remote", cfg, mcp.LoginOptions{
		Store: store, OpenBrowser: oauthserver.Browser, Out: &out, Scopes: scopes, Timeout: 10 * time.Second,
	})
	require.NoError(t, err, out.String())

	return out.String()
}

// TestOAuth runs the whole story against a real authorization server: a
// server that needs login says so, `uah mcp login` stores tokens (PKCE,
// dynamic registration), calls then work, an expired token is refreshed
// and saved, a revoked login asks for reauthentication, and logout
// forgets it.
func TestOAuth(t *testing.T) {
	srv := oauthserver.New(t)
	path := filepath.Join(t.TempDir(), "mcp-credentials.json")
	store := &mcp.FileStore{Path: path}
	cfg := mcp.ServerConfig{URL: srv.MCPURL()}
	ctx := context.Background()

	st := oauthManager(t, cfg, store).Status()[0]
	assert.Equal(t, mcp.StateNeedsLogin, st.State)
	assert.Equal(t, "The remote MCP server is not logged in. Run `uah mcp login remote`.", st.Error)
	assert.Equal(t, mcp.AuthNotLoggedIn, st.Auth)
	assert.Equal(t, mcp.AuthNotLoggedIn, mcp.AuthStatusOf(ctx, "remote", cfg, store, nil), "discovery finds OAuth")

	srv.SetTTL(time.Second) // within the oauth2 package's 10 s expiry margin, so the next use refreshes
	out := login(t, cfg, store)
	assert.Contains(t, out, "Authorize `remote` by opening this URL in your browser:\n"+srv.URL()+"/authorize?")
	stats := srv.Stats()
	assert.Equal(t, 1, stats.Registrations, "dynamic client registration")
	assert.Equal(t, srv.MCPURL(), stats.Resource)
	assert.ElementsMatch(t, []string{"mcp:read", "mcp:write", "offline_access"}, strings.Fields(stats.Scope), "the advertised scopes and a refresh token")
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	first, err := store.Load("remote", srv.MCPURL()) // the last token of the login
	require.NoError(t, err)
	assert.NotEmpty(t, first.RefreshToken)
	assert.Equal(t, mcp.AuthOAuth, mcp.AuthStatusOf(ctx, "remote", cfg, store, nil))

	srv.SetTTL(time.Hour)
	refreshes := srv.Stats().Refreshes
	m := oauthManager(t, cfg, store)
	st = m.Status()[0]
	require.Equal(t, mcp.StateReady, st.State, st.Error)
	assert.Equal(t, mcp.AuthOAuth, st.Auth)
	r, err := m.Call(ctx, "remote", "whoami", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "authorized", r.Text)
	assert.Equal(t, refreshes+1, srv.Stats().Refreshes, "the expired token was refreshed")
	saved, err := store.Load("remote", srv.MCPURL())
	require.NoError(t, err)
	assert.NotEqual(t, first.AccessToken, saved.AccessToken, "the refreshed token was saved")
	assert.NotEqual(t, first.RefreshToken, saved.RefreshToken, "with the rotated refresh token")

	srv.Revoke()
	st = oauthManager(t, cfg, store).Status()[0]
	assert.Equal(t, mcp.StateNeedsLogin, st.State)
	assert.Contains(t, st.Error, "requires OAuth reauthentication")

	ok, err := mcp.Logout("remote", cfg, store)
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = mcp.Logout("remote", cfg, store)
	require.NoError(t, err)
	assert.False(t, ok)
	_, err = os.Stat(path)
	assert.ErrorIs(t, err, os.ErrNotExist, "the last login removes the file")
}

// TestOAuthLoginOptions sends the configured scopes and oauth_resource.
func TestOAuthLoginOptions(t *testing.T) {
	srv := oauthserver.New(t)
	store := &mcp.FileStore{Path: filepath.Join(t.TempDir(), "creds.json")}
	cfg := mcp.ServerConfig{URL: srv.MCPURL(), Scopes: []string{"mcp:write"}, OAuthResource: "https://resource.example.com"}
	login(t, cfg, store)
	assert.ElementsMatch(t, []string{"mcp:write", "offline_access"}, strings.Fields(srv.Stats().Scope))
	assert.Equal(t, "https://resource.example.com", srv.Stats().Resource)
	login(t, cfg, store, "mcp:read")
	assert.ElementsMatch(t, []string{"mcp:read", "offline_access"}, strings.Fields(srv.Stats().Scope), "--scopes wins over the configuration")

	require.ErrorContains(t, mcp.Login(context.Background(), "s", mcp.ServerConfig{Command: "x"}, mcp.LoginOptions{Store: store}), "only supported for streamable HTTP")
	require.ErrorContains(t, mcp.Login(context.Background(), "s", mcp.ServerConfig{URL: srv.MCPURL(), BearerTokenEnvVar: "T"}, mcp.LoginOptions{Store: store}), "bearer token")
	_, err := mcp.Logout("s", mcp.ServerConfig{Command: "x"}, store)
	require.ErrorContains(t, err, "only supported for streamable_http")
}

// A login that the browser never finishes times out.
func TestOAuthLoginTimesOut(t *testing.T) {
	srv := oauthserver.New(t)
	store := &mcp.FileStore{Path: filepath.Join(t.TempDir(), "creds.json")}
	var out bytes.Buffer
	err := mcp.Login(context.Background(), "remote", mcp.ServerConfig{URL: srv.MCPURL()}, mcp.LoginOptions{
		Store: store, Out: &out, Timeout: 200 * time.Millisecond,
		OpenBrowser: func(string) error { return os.ErrNotExist },
	})
	require.ErrorContains(t, err, "timed out waiting for OAuth callback")
	assert.Contains(t, out.String(), "(Browser launch failed; please copy the URL above manually.)")
}
