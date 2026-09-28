package app_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent-harness/internal/app"
	"github.com/viktordanov/uagent-harness/internal/engine/codexauth"
)

// TestDoctor_CodexLoginUahRefreshes: an expired token with a refresh token
// does not fail the doctor, since each run refreshes it first, and the
// check says when it expired and that uah refreshes it, without a token.
func TestDoctor_CodexLoginUahRefreshes(t *testing.T) {
	e, in := setupEnv(t)
	token := func(d time.Duration) string {
		claims := `{"exp":` + strconv.FormatInt(time.Now().Add(d).Unix(), 10) + `,"https://api.openai.com/auth":{"chatgpt_account_id":"acct-test"}}`

		return "x." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".y"
	}
	expired := token(-3 * time.Hour)
	writeFile(t, filepath.Join(e.CodexHome, "auth.json"), `{"auth_mode":"chatgpt","tokens":{"access_token":"`+expired+
		`","refresh_token":"rt-secret","account_id":"acct-test"},"last_refresh":"`+time.Now().Add(-9*24*time.Hour).UTC().Format(time.RFC3339)+`"}`)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": token(24 * time.Hour), "refresh_token": "rt-2"})
	}))
	t.Cleanup(srv.Close)
	t.Setenv(codexauth.TokenURLEnv, srv.URL) // the usage check reads usage, which refreshes

	checks := app.Doctor(context.Background(), in, doctorOptions(t, filepath.Join(t.TempDir(), "trust.json")))
	got := find(t, checks, "credentials")
	assert.Equal(t, app.CheckOK, got.Status, got.Detail)
	assert.Contains(t, got.Detail, "ChatGPT login in "+filepath.Join(e.CodexHome, "auth.json"))
	assert.Contains(t, got.Detail, "access token expired 3 hours ago")
	assert.Contains(t, got.Detail, "last refreshed 9 days ago")
	assert.Contains(t, got.Detail, "uah refreshes it with the refresh token")
	assert.NotContains(t, got.Detail, "rt-secret")
	assert.NotContains(t, got.Detail, expired)
	assert.True(t, app.Healthy(checks))
	assert.LessOrEqual(t, calls.Load(), int32(1))
}
