package codexauth_test

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/unreallabsai/unreal-agent/harness/llm/clients/openaicodex"

	"github.com/viktordanov/uagent-harness/internal/engine/codexauth"
)

// token is a test access token (not a real one) with the account ID and
// expiry in its claims.
func token(account string, expires time.Time) string {
	claims := `{"exp":` + strconv.FormatInt(expires.Unix(), 10) + `,"https://api.openai.com/auth":{"chatgpt_account_id":"` + account + `"}}`

	return "x." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".y"
}

func authFile(t *testing.T, tok string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"tokens":{"access_token":"`+tok+`"}}`), 0o600))

	return path
}

func TestLoad(t *testing.T) {
	later := time.Now().Add(time.Hour)
	creds, err := codexauth.Load(openaicodex.Config{AuthFile: authFile(t, token("acct-1", later))})
	require.NoError(t, err)
	assert.Equal(t, "acct-1", creds.AccountID, "the account comes from the token's claims")

	creds, err = codexauth.Load(openaicodex.Config{AccessToken: token("acct-2", later)})
	require.NoError(t, err)
	assert.Equal(t, "acct-2", creds.AccountID)

	for name, c := range map[string]struct {
		cfg  openaicodex.Config
		want string
	}{
		"nothing":        {openaicodex.Config{}, "access token must be set"},
		"an API key":     {openaicodex.Config{AccessToken: "sk-test"}, "not an API key"},
		"expired":        {openaicodex.Config{AccessToken: token("acct", time.Now().Add(-time.Hour))}, "has expired"},
		"other account":  {openaicodex.Config{AccessToken: token("acct-1", later), AccountID: "acct-9"}, "does not match"},
		"file and token": {openaicodex.Config{AuthFile: "x", AccessToken: "y"}, "cannot be combined"},
	} {
		_, err := codexauth.Load(c.cfg)
		require.ErrorContains(t, err, c.want, name)
		assert.NotContains(t, err.Error(), "x.", "%s: an error never includes the token", name)
	}
}

func TestHeaderValue(t *testing.T) {
	assert.True(t, codexauth.HeaderValue("abc-123"))
	assert.False(t, codexauth.HeaderValue(""))
	assert.False(t, codexauth.HeaderValue("a b"))
	assert.False(t, codexauth.HeaderValue("é"))
}
