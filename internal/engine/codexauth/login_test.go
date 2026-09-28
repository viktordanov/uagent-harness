package codexauth_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/engine/codexauth"
)

// tokenServer is a fake token endpoint: it answers each refresh with
// answer, and counts and keeps the requests.
type tokenServer struct {
	URL    string
	calls  atomic.Int32
	mu     sync.Mutex
	bodies []map[string]string
	types  []string
	answer func(w http.ResponseWriter, refreshToken string)
}

func newTokenServer(t *testing.T, answer func(w http.ResponseWriter, refreshToken string)) *tokenServer {
	t.Helper()
	ts := &tokenServer{answer: answer}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ts.calls.Add(1)
		var body map[string]string
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)
		ts.mu.Lock()
		ts.bodies = append(ts.bodies, body)
		ts.types = append(ts.types, r.Header.Get("Content-Type"))
		ts.mu.Unlock()
		ts.answer(w, body["refresh_token"])
	}))
	t.Cleanup(srv.Close)
	ts.URL = srv.URL + "/oauth/token"

	return ts
}

// issue answers with new tokens for the account.
func issue(access, refresh string) func(http.ResponseWriter, string) {
	return func(w http.ResponseWriter, _ string) {
		_ = json.NewEncoder(w).Encode(map[string]string{"id_token": "id.new.jwt", "access_token": access, "refresh_token": refresh})
	}
}

// refuse answers with a status and a body.
func refuse(status int, body string) func(http.ResponseWriter, string) {
	return func(w http.ResponseWriter, _ string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

// codexHome writes a Codex auth file as Codex does, with fields uah does not
// read, and returns the directory.
func codexHome(t *testing.T, access, refresh string) string {
	t.Helper()
	dir := t.TempDir()
	writeAuth(t, dir, access, refresh)

	return dir
}

func writeAuth(t *testing.T, dir, access, refresh string) {
	t.Helper()
	auth := `{
  "auth_mode": "chatgpt",
  "OPENAI_API_KEY": null,
  "tokens": {
    "id_token": "id.old.jwt",
    "access_token": "` + access + `",
    "refresh_token": "` + refresh + `",
    "account_id": "acct-1"
  },
  "last_refresh": "2026-09-01T10:00:00.123456Z",
  "agent_identity": {"future": [1, 2]}
}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.json"), []byte(auth), 0o600))
}

func env(home, tokenURL string, extra ...string) func(string) string {
	vars := map[string]string{"CODEX_HOME": home, codexauth.TokenURLEnv: tokenURL}
	for i := 0; i+1 < len(extra); i += 2 {
		vars[extra[i]] = extra[i+1]
	}

	return func(k string) string { return vars[k] }
}

func open(t *testing.T, getenv func(string) string) *codexauth.Login {
	t.Helper()
	login, err := codexauth.Open(getenv)
	require.NoError(t, err)

	return login
}

func readAuth(t *testing.T, home string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, "auth.json"))
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(data, &out))

	return out
}

func TestCreds_RefreshesAnExpiringToken(t *testing.T) {
	soon, fresh := token("acct-1", time.Now().Add(time.Minute)), token("acct-1", time.Now().Add(10*24*time.Hour))
	ts := newTokenServer(t, issue(fresh, "rt-2"))
	home := codexHome(t, soon, "rt-1")
	before, err := os.Stat(filepath.Join(home, "auth.json"))
	require.NoError(t, err)

	creds, err := open(t, env(home, ts.URL)).Creds(context.Background())
	require.NoError(t, err)
	assert.Equal(t, codexauth.Creds{AccessToken: fresh, AccountID: "acct-1"}, creds)
	require.EqualValues(t, 1, ts.calls.Load())
	assert.Equal(t, map[string]string{"client_id": "app_EMoamEEZ73f0CkXaXp7hrann", "grant_type": "refresh_token", "refresh_token": "rt-1"},
		ts.bodies[0], "Codex's refresh grant")
	assert.Equal(t, "application/json", ts.types[0])

	// The file is Codex's, with the new tokens and every other field kept.
	path := filepath.Join(home, "auth.json")
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	assert.False(t, os.SameFile(before, info), "the file is replaced by a rename, not rewritten in place")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "{\n  \"auth_mode\": \"chatgpt\",\n  \"OPENAI_API_KEY\": null,\n  \"tokens\": {\n    \"id_token\": \"id.new.jwt\",")
	auth := readAuth(t, home)
	assert.Equal(t, map[string]any{"id_token": "id.new.jwt", "access_token": fresh, "refresh_token": "rt-2", "account_id": "acct-1"}, auth["tokens"])
	assert.Equal(t, map[string]any{"future": []any{1.0, 2.0}}, auth["agent_identity"], "fields uah does not know are kept")
	last, err := time.Parse(time.RFC3339Nano, auth["last_refresh"].(string))
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now(), last, time.Minute)
	entries, err := os.ReadDir(home)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp", "no temporary file is left")
	}

	// A token that does not expire soon is used as it is.
	_, err = open(t, env(home, ts.URL)).Creds(context.Background())
	require.NoError(t, err)
	assert.EqualValues(t, 1, ts.calls.Load())
}

func TestCreds_KeepsAWorkingTokenWhenTheRefreshFails(t *testing.T) {
	soon := token("acct-1", time.Now().Add(time.Minute))
	ts := newTokenServer(t, refuse(http.StatusServiceUnavailable, `{"error":"later"}`))
	creds, err := open(t, env(codexHome(t, soon, "rt-1"), ts.URL)).Creds(context.Background())
	require.NoError(t, err, "Codex uses the token it has when a refresh fails")
	assert.Equal(t, soon, creds.AccessToken)
}

func TestTransport_RenewsAfter401AndRetries(t *testing.T) {
	old, fresh := token("acct-1", time.Now().Add(time.Hour)), token("acct-1", time.Now().Add(10*24*time.Hour))
	ts := newTokenServer(t, issue(fresh, "rt-2"))
	var seen []string
	var bodies []string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen, bodies = append(seen, r.Header.Get("Authorization")), append(bodies, string(body))
		assert.Equal(t, "acct-1", r.Header.Get("Chatgpt-Account-Id"))
		if r.Header.Get("Authorization") != "Bearer "+fresh {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(backend.Close)
	home := codexHome(t, old, "rt-1")
	client := &http.Client{Transport: open(t, env(home, ts.URL)).Transport(http.DefaultTransport)}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, backend.URL, strings.NewReader(`{"model":"m"}`))
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, []string{"Bearer " + old, "Bearer " + fresh}, seen, "the rejected request is sent once more with the new token")
	assert.Equal(t, []string{`{"model":"m"}`, `{"model":"m"}`}, bodies)
	assert.EqualValues(t, 1, ts.calls.Load())

	// A body the transport cannot send again gets the 401.
	seen = nil
	writeAuth(t, home, old, "rt-3")
	req, err = http.NewRequestWithContext(context.Background(), http.MethodPost, backend.URL, &onceReader{s: "x"})
	require.NoError(t, err)
	resp2, err := client.Do(req)
	require.NoError(t, err)
	_ = resp2.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp2.StatusCode)
	assert.Len(t, seen, 1)
}

func TestRenew_OneRefreshForConcurrentCallers(t *testing.T) {
	old, fresh := token("acct-1", time.Now().Add(time.Hour)), token("acct-1", time.Now().Add(10*24*time.Hour))
	release := make(chan struct{})
	ts := newTokenServer(t, func(w http.ResponseWriter, rt string) {
		<-release
		issue(fresh, "rt-2")(w, rt)
	})
	home := codexHome(t, old, "rt-1")
	const n = 16
	var wg sync.WaitGroup
	results := make([]codexauth.Creds, n)
	errs := make([]error, n)
	for i := range n {
		wg.Go(func() {
			// Separate Logins, as the engine, the usage reader, and the
			// model list open their own: they share the file.
			results[i], errs[i] = open(t, env(home, ts.URL)).Renew(context.Background(), codexauth.Creds{AccessToken: old, AccountID: "acct-1"})
		})
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	for i := range n {
		require.NoError(t, errs[i])
		assert.Equal(t, fresh, results[i].AccessToken)
	}
	assert.EqualValues(t, 1, ts.calls.Load(), "one refresh for every caller")
}

func TestRenew_UsesTheTokenAnotherWriterStored(t *testing.T) {
	old, codexs := token("acct-1", time.Now().Add(time.Hour)), token("acct-1", time.Now().Add(9*24*time.Hour))
	ts := newTokenServer(t, issue("unused", "rt-x"))
	home := codexHome(t, old, "rt-1")
	login := open(t, env(home, ts.URL))
	creds, err := login.Creds(context.Background())
	require.NoError(t, err)

	// Codex refreshes the login and rewrites the file in place.
	writeAuth(t, home, codexs, "rt-codex")
	renewed, err := login.Renew(context.Background(), creds)
	require.NoError(t, err)
	assert.Equal(t, codexs, renewed.AccessToken)
	assert.EqualValues(t, 0, ts.calls.Load(), "no refresh: Codex's token is newer")
}

func TestRenew_AWriterThatRefreshesMeanwhileWins(t *testing.T) {
	old, codexs := token("acct-1", time.Now().Add(time.Hour)), token("acct-1", time.Now().Add(9*24*time.Hour))
	home := codexHome(t, old, "rt-1")
	ts := newTokenServer(t, func(w http.ResponseWriter, rt string) {
		// Codex writes its refresh while uah's is in flight.
		writeAuth(t, home, codexs, "rt-codex")
		issue(token("acct-1", time.Now().Add(10*24*time.Hour)), "rt-uah")(w, rt)
	})
	renewed, err := open(t, env(home, ts.URL)).Renew(context.Background(), codexauth.Creds{AccessToken: old, AccountID: "acct-1"})
	require.NoError(t, err)
	assert.Equal(t, codexs, renewed.AccessToken)
	tokens, ok := readAuth(t, home)["tokens"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "rt-codex", tokens["refresh_token"], "Codex's file is not overwritten")
}

func TestRenew_RefusedRefreshSaysToSignInAgain(t *testing.T) {
	for name, answer := range map[string]func(http.ResponseWriter, string){
		"invalid_grant": refuse(http.StatusBadRequest, `{"error":"invalid_grant","error_description":"rt-1 is bad"}`),
		"reused":        refuse(http.StatusBadRequest, `{"error":{"code":"refresh_token_reused","message":"rt-1"}}`),
		"expired":       refuse(http.StatusForbidden, `{"code":"refresh_token_expired"}`),
		"401":           refuse(http.StatusUnauthorized, `rt-1`),
	} {
		t.Run(name, func(t *testing.T) {
			old := token("acct-1", time.Now().Add(-time.Hour))
			ts := newTokenServer(t, answer)
			login := open(t, env(codexHome(t, old, "rt-1"), ts.URL))
			_, err := login.Creds(context.Background())
			require.ErrorIs(t, err, codexauth.ErrLoginExpired)
			assert.Equal(t, "Your ChatGPT login expired; run `codex login`", err.Error())
			_, err = login.Renew(context.Background(), codexauth.Creds{AccessToken: old})
			require.ErrorIs(t, err, codexauth.ErrLoginExpired)
			assert.EqualValues(t, 1, ts.calls.Load(), "a refused refresh token is not sent again")
			status, err := login.Status()
			require.NoError(t, err)
			assert.False(t, status.Refreshable)
		})
	}
}

func TestRenew_OtherFailuresKeepNoSecret(t *testing.T) {
	ts := newTokenServer(t, refuse(http.StatusInternalServerError, `{"error":"boom","token":"rt-1"}`))
	old := token("acct-1", time.Now().Add(-time.Hour))
	_, err := open(t, env(codexHome(t, old, "rt-1"), ts.URL)).Creds(context.Background())
	require.Error(t, err)
	assert.NotErrorIs(t, err, codexauth.ErrLoginExpired)
	assert.Equal(t, "failed to refresh the ChatGPT login: the token endpoint answered 500 Internal Server Error", err.Error())
}

func TestEnvironmentToken_IsNeverRefreshed(t *testing.T) {
	ts := newTokenServer(t, issue("unused", "unused"))
	expired := token("acct-1", time.Now().Add(-time.Minute))
	login := open(t, env(t.TempDir(), ts.URL, "OPENAI_CODEX_ACCESS_TOKEN", expired))
	assert.False(t, login.FromFile())
	_, err := login.Creds(context.Background())
	require.ErrorContains(t, err, "has expired")
	_, err = login.Renew(context.Background(), codexauth.Creds{AccessToken: expired})
	require.ErrorContains(t, err, "OPENAI_CODEX_ACCESS_TOKEN is not refreshed")
	require.NoError(t, codexauth.BeforeRun(context.Background(), codexauth.Provider, env(t.TempDir(), ts.URL, "OPENAI_CODEX_ACCESS_TOKEN", expired)),
		"preflight reports the environment's token")

	valid := token("acct-1", time.Now().Add(time.Hour))
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	t.Cleanup(backend.Close)
	client := &http.Client{Transport: open(t, env(t.TempDir(), ts.URL, "OPENAI_CODEX_ACCESS_TOKEN", valid)).Transport(http.DefaultTransport)}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, backend.URL, http.NoBody)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "the 401 reaches the caller")
	assert.EqualValues(t, 0, ts.calls.Load())
}

func TestBeforeRun(t *testing.T) {
	fresh := token("acct-1", time.Now().Add(10*24*time.Hour))
	ts := newTokenServer(t, issue(fresh, "rt-2"))
	expired := token("acct-1", time.Now().Add(-24*time.Hour))

	home := codexHome(t, expired, "rt-1")
	require.NoError(t, codexauth.BeforeRun(context.Background(), "openai", env(home, ts.URL)), "another provider")
	assert.EqualValues(t, 0, ts.calls.Load())
	require.NoError(t, codexauth.BeforeRun(context.Background(), codexauth.Provider, env(home, ts.URL)))
	assert.EqualValues(t, 1, ts.calls.Load())
	tokens, ok := readAuth(t, home)["tokens"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, fresh, tokens["access_token"], "the runner and preflight read the refreshed file")

	// Within the hour preflight warns about, it refreshes too.
	home = codexHome(t, token("acct-1", time.Now().Add(30*time.Minute)), "rt-1")
	require.NoError(t, codexauth.BeforeRun(context.Background(), codexauth.Provider, env(home, ts.URL)))
	assert.EqualValues(t, 2, ts.calls.Load())

	// Without a refresh token, an expired login blocks the run plainly.
	err := codexauth.BeforeRun(context.Background(), codexauth.Provider, env(codexHome(t, expired, ""), ts.URL))
	require.ErrorIs(t, err, codexauth.ErrLoginExpired)
}

func TestOpen_TokenEndpointMustBeLoopback(t *testing.T) {
	_, err := codexauth.Open(env(t.TempDir(), "https://example.com/oauth/token"))
	require.ErrorContains(t, err, "loopback")
}

func TestStatus(t *testing.T) {
	expires := time.Now().Add(time.Hour).Truncate(time.Second)
	home := codexHome(t, token("acct-1", expires), "rt-1")
	s, err := open(t, env(home, "")).Status()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "auth.json"), s.File)
	assert.True(t, s.Expires.Equal(expires))
	assert.Equal(t, time.Date(2026, 9, 1, 10, 0, 0, 123456000, time.UTC), s.LastRefresh.UTC())
	assert.True(t, s.Refreshable)
}

// onceReader is a body http.NewRequest cannot rewind, so the request's
// GetBody is nil; the transport must not retry it.
type onceReader struct{ s string }

func (r *onceReader) Read(p []byte) (int, error) {
	if r.s == "" {
		return 0, io.EOF
	}
	n := copy(p, r.s)
	r.s = r.s[n:]

	return n, nil
}
