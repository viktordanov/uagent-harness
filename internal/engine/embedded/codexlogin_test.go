package embedded_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"
	uaharness "github.com/viktordanov/uagent/harness"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/engine/codexauth"
	"github.com/viktordanov/uagent-harness/internal/engine/embedded"
	"github.com/viktordanov/uagent-harness/internal/engine/process"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
	"github.com/viktordanov/uagent-harness/testing/harnesstest"
)

// accessToken is a test token (not a real one) for acct-test expiring at
// exp; n makes tokens with the same expiry differ.
func accessToken(exp time.Time, n int) string {
	claims := `{"exp":` + strconv.FormatInt(exp.Unix(), 10) + `,"n":` + strconv.Itoa(n) +
		`,"https://api.openai.com/auth":{"chatgpt_account_id":"acct-test"}}`

	return "x." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".y"
}

// codexLogin is an env whose Codex login has a refresh token, with a fake
// token endpoint that issues fresh.
type codexLogin struct {
	*env
	fresh    string
	tokenURL string
	calls    *atomic.Int32
}

func newCodexLogin(t *testing.T, access string, replies ...fakellm.Reply) *codexLogin {
	t.Helper()
	e := newEnv(t, replies...)
	auth := `{"auth_mode":"chatgpt","tokens":{"id_token":"id","access_token":"` + access +
		`","refresh_token":"rt-1","account_id":"acct-test"},"last_refresh":"2026-09-01T00:00:00Z"}`
	require.NoError(t, os.WriteFile(filepath.Join(e.CodexHome, "auth.json"), []byte(auth), 0o600))
	c := &codexLogin{env: e, fresh: accessToken(time.Now().Add(10*24*time.Hour), 2), calls: &atomic.Int32{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		c.calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": c.fresh, "refresh_token": "rt-2"})
	}))
	t.Cleanup(srv.Close)
	c.tokenURL = srv.URL

	return c
}

func (c *codexLogin) getenv(key string) string {
	if key == codexauth.TokenURLEnv {
		return c.tokenURL
	}

	return c.env.getenv(key)
}

// run sends one message and returns the result and the runner's errors.
func (c *codexLogin) run(t *testing.T, eng engine.Engine) (core.Result, string) {
	t.Helper()
	settings := session.Settings{Provider: codexauth.Provider, Model: "gpt-test", Effort: "high", Workspace: c.Workspace, BaseURL: c.llm.URL}
	s, err := session.Open(context.Background(), eng, session.Options{Settings: settings})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	ev := &events{t: t, s: s}
	_, err = s.Submit("hi")
	require.NoError(t, err)
	result := ev.finished()
	ev.idle()
	var errs string
	for _, e := range ev.all {
		if re, ok := e.(core.RunnerError); ok {
			errs += re.Message + "\n"
		}
	}

	return result, errs
}

// TestEmbedded_CodexRenewsARejectedLogin: the backend rejects the token
// once, as after the login expired while the session was open (GitHub #1);
// the engine refreshes it and sends the request again.
func TestEmbedded_CodexRenewsARejectedLogin(t *testing.T) {
	old := accessToken(time.Now().Add(24*time.Hour), 1)
	c := newCodexLogin(t, old, fakellm.Reply{Fail: http.StatusUnauthorized, FailBody: `{"detail":"token expired"}`}, fakellm.Reply{Text: "hello"})
	eng := embedded.New(embedded.Config{StateDir: c.StateDir, Provider: codexauth.Provider, Getenv: c.getenv})

	result, errs := c.run(t, eng)
	assert.Equal(t, core.StatusOK, result.Status, errs)
	assert.Equal(t, "hello", result.Answer)
	reqs := c.llm.Requests()
	require.Len(t, reqs, 2)
	assert.Equal(t, "Bearer "+old, reqs[0].Authorization)
	assert.Equal(t, "Bearer "+c.fresh, reqs[1].Authorization)
	assert.EqualValues(t, 1, c.calls.Load())
}

// TestEngines_RefreshAnExpiredCodexLoginBeforeTheRun: uagent's preflight
// blocks an expired token, so both engines refresh it first; the runner the
// process engine spawns reads the refreshed file.
func TestEngines_RefreshAnExpiredCodexLoginBeforeTheRun(t *testing.T) {
	expired := accessToken(time.Now().Add(-time.Hour), 1)
	t.Run("embedded", func(t *testing.T) {
		c := newCodexLogin(t, expired, fakellm.Reply{Text: "hello"})
		result, errs := c.run(t, embedded.New(embedded.Config{StateDir: c.StateDir, Provider: codexauth.Provider, Getenv: c.getenv}))
		assert.Equal(t, core.StatusOK, result.Status, errs)
		require.Len(t, c.llm.Requests(), 1)
		assert.Equal(t, "Bearer "+c.fresh, c.llm.Requests()[0].Authorization)
		assert.EqualValues(t, 1, c.calls.Load())
	})
	t.Run("process", func(t *testing.T) {
		if testing.Short() {
			t.Skip("builds unreal-agent-runner")
		}
		c := newCodexLogin(t, expired, fakellm.Reply{Text: "hello"})
		t.Setenv("CODEX_HOME", c.CodexHome) // the runner reads its environment
		proc := process.New(uaharness.Config{RunnerPath: harnesstest.RealRunner(t), StateDir: c.StateDir, Getenv: c.getenv, KillGrace: time.Second})
		result, errs := c.run(t, proc)
		assert.Equal(t, core.StatusOK, result.Status, errs)
		require.Len(t, c.llm.Requests(), 1)
		assert.Equal(t, "Bearer "+c.fresh, c.llm.Requests()[0].Authorization)
	})
}

// TestEmbedded_CodexLoginThatCannotBeRefreshed says to sign in again.
func TestEmbedded_CodexLoginThatCannotBeRefreshed(t *testing.T) {
	c := newCodexLogin(t, accessToken(time.Now().Add(24*time.Hour), 1), fakellm.Reply{Fail: http.StatusUnauthorized})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	t.Cleanup(srv.Close)
	c.tokenURL = srv.URL
	result, errs := c.run(t, embedded.New(embedded.Config{StateDir: c.StateDir, Provider: codexauth.Provider, Getenv: c.getenv}))
	assert.NotEqual(t, core.StatusOK, result.Status)
	assert.Contains(t, errs, "Your ChatGPT login expired; run `codex login`")
	assert.NotContains(t, errs, "rt-1")
	assert.Len(t, c.llm.Requests(), 1, "a refused refresh is not retried")
}
